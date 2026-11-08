package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/server"
	"gitlab.com/rockt/cerne/internal/store"
)

// F-0334 crit. 7: `cerne pending list --json` and the ui's GET /pendings are
// the same selection in the same order, for every state (plus all) crossed
// with every filter. Both sides run for real — the CLI command and the HTTP
// handler — so a drift in either one's filtering or ordering fails here.
func TestListPendingsMatchesCLI(t *testing.T) {
	projDir, _, repoDir := setupProject(t, "core", "web")
	newOriginFeature(t, projDir, "Origem core", "--module", "core")
	newOriginFeature(t, projDir, "Origem web", "--module", "web")
	pendingNew(t, projDir, "Cobrir links quebrados", "--from", "F-0001", "--component", "lint")
	pendingNew(t, projDir, "Documentar rota nova", "--from", "F-0002")
	pendingNew(t, projDir, "Ajustar cache do browser", "--from", "F-0002", "--component", "lint")
	pendingNew(t, projDir, "Remover flag antiga", "--from", "F-0001")
	pendingNew(t, projDir, "Migrar índice de busca", "--from", "F-0001")
	pendingNew(t, projDir, "Revisar textos do menu", "--from", "F-0002", "--component", "ui")
	for _, args := range [][]string{
		{"pending", "resolve", "P-0003"},
		{"pending", "drop", "P-0004", "--reason", "fora de escopo"},
		{"pending", "promote", "P-0005", "--as", "feature", "--module", "core"},
	} {
		if out, err := execCerne(t, append(args, "--dir", projDir)...); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}
	h := server.New(repoDir, projDir, "")
	extras := []struct {
		args  []string
		query url.Values
	}{
		{nil, url.Values{}},
		{[]string{"--from", "f-0001"}, url.Values{"from": {"f-0001"}}},
		{[]string{"--component", "Lint"}, url.Values{"component": {"Lint"}}},
		{[]string{"--from", "F-0002", "--component", "lint"}, url.Values{"from": {"F-0002"}, "component": {"lint"}}},
		{[]string{"--module", "web"}, url.Values{"module": {"web"}}},
	}
	ids := func(doc []byte) []string {
		t.Helper()
		var v struct {
			Pendings []store.Pending `json:"pendings"`
		}
		if err := json.Unmarshal(doc, &v); err != nil {
			t.Fatalf("invalid JSON %q: %v", doc, err)
		}
		out := []string{}
		for _, p := range v.Pendings {
			out = append(out, p.ID)
		}
		return out
	}
	for _, s := range []string{"open", "resolved", "dropped", "promoted", "all"} {
		for _, ex := range extras {
			args := append([]string{"pending", "list", "--json", "--state", s}, ex.args...)
			out, err := execCerne(t, append(args, "--dir", projDir)...)
			if err != nil {
				t.Fatalf("%v: %v\n%s", args, err, out)
			}
			q := url.Values{"state": {s}}
			for k, v := range ex.query {
				q[k] = v
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/pendings?"+q.Encode(), nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("GET /api/pendings?%s = %d: %s", q.Encode(), rec.Code, rec.Body.String())
			}
			if cli, api := ids([]byte(out)), ids(rec.Body.Bytes()); !slices.Equal(cli, api) {
				t.Fatalf("%v: cli %v, GET /pendings?%s %v", args, cli, q.Encode(), api)
			}
			if s == "all" && ex.args == nil && len(ids([]byte(out))) != 6 {
				t.Fatalf("--state all lists %v, want all 6", ids([]byte(out)))
			}
		}
	}
}

// F-0334 (critic C2, C3): --state is checked before the project is resolved,
// with the message it always had (the list now ends in all), and an explicit
// empty --state is refused instead of read as open.
func TestPendingListStateValidation(t *testing.T) {
	projDir, _, _ := setupProject(t)
	for _, tc := range []struct{ state, dir string }{
		{"bogus", projDir},
		{"", projDir},
		{"bogus", t.TempDir()}, // not a project: the state error still comes first
	} {
		want := fmt.Sprintf("--state %q: want one of open, resolved, dropped, promoted, all", tc.state)
		out, err := execCerne(t, "pending", "list", "--state", tc.state, "--dir", tc.dir)
		if !errors.Is(err, app.ErrPendingStateInvalid) || err.Error() != want {
			t.Fatalf("--state %q in %s: err = %v, want %q (ErrPendingStateInvalid)\n%s", tc.state, tc.dir, err, want, out)
		}
	}
}

// F-0334 (critic C4): with --state all the text lines carry each pending's
// state, before the title; a single-state list keeps its lines unchanged, and
// an empty all list says "No pendings.".
func TestPendingListAllShowsState(t *testing.T) {
	projDir, _, _ := setupProject(t)
	newOriginFeature(t, projDir, "Origem demo")
	pendingNew(t, projDir, "Cobrir links quebrados", "--from", "F-0001")
	pendingNew(t, projDir, "Documentar rota nova", "--from", "F-0001")
	if out, err := execCerne(t, "pending", "resolve", "P-0002", "--dir", projDir); err != nil {
		t.Fatalf("resolve: %v\n%s", err, out)
	}
	list := func(args ...string) string {
		t.Helper()
		out, err := execCerne(t, append([]string{"pending", "list", "--dir", projDir}, args...)...)
		if err != nil {
			t.Fatalf("pending list %v: %v\n%s", args, err, out)
		}
		return out
	}
	all := list("--state", "all")
	for _, want := range []string{"P-0001  0d  F-0001  open  Cobrir links quebrados\n", "P-0002  0d  F-0001  resolved  Documentar rota nova\n"} {
		if !strings.Contains(all, want) {
			t.Fatalf("--state all lacks %q:\n%s", want, all)
		}
	}
	if open := list(); !strings.Contains(open, "P-0001  0d  F-0001  Cobrir links quebrados\n") {
		t.Fatalf("the open list changed its lines:\n%s", open)
	}
	if empty := list("--state", "all", "--from", "F-0099"); empty != "No pendings.\n" {
		t.Fatalf("empty all list = %q, want \"No pendings.\\n\"", empty)
	}
}
