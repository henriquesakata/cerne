package cli

import (
	"encoding/json"
	"slices"
	"testing"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/store"
)

// F-0334 crit. 7: `cerne pending list --json` and app.ListPendings are the
// same selection in the same order, for every state (plus all) crossed with
// every filter — the ui route calls the same function.
func TestListPendingsMatchesCLI(t *testing.T) {
	projDir, _, repoDir := setupProject(t, "core", "web")
	newOriginFeature(t, projDir, "Origem core", "--module", "core")
	newOriginFeature(t, projDir, "Origem web", "--module", "web")
	pendingNew(t, projDir, "Cobrir links quebrados", "--from", "F-0001", "--component", "lint")
	pendingNew(t, projDir, "Documentar rota nova", "--from", "F-0002")
	pendingNew(t, projDir, "Ajustar cache do browser", "--from", "F-0002", "--component", "lint")
	pendingNew(t, projDir, "Remover flag antiga", "--from", "F-0001")
	pendingNew(t, projDir, "Migrar índice de busca", "--from", "F-0001")
	for _, args := range [][]string{
		{"pending", "resolve", "P-0003"},
		{"pending", "drop", "P-0004", "--reason", "fora de escopo"},
		{"pending", "promote", "P-0005", "--as", "feature", "--module", "core"},
	} {
		if out, err := execCerne(t, append(args, "--dir", projDir)...); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}
	extras := []struct {
		args []string
		f    app.PendingFilter
	}{
		{nil, app.PendingFilter{}},
		{[]string{"--from", "f-0001"}, app.PendingFilter{From: "f-0001"}},
		{[]string{"--component", "Lint"}, app.PendingFilter{Component: "Lint"}},
		{[]string{"--module", "web"}, app.PendingFilter{Module: "web"}},
	}
	ids := func(ps []store.Pending) []string {
		out := []string{}
		for _, p := range ps {
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
			var doc struct {
				Pendings []store.Pending `json:"pendings"`
			}
			if err := json.Unmarshal([]byte(out), &doc); err != nil {
				t.Fatalf("%v: %v\n%s", args, err, out)
			}
			f := ex.f
			f.State = s
			got, err := app.ListPendings(repoDir, f)
			if err != nil {
				t.Fatalf("ListPendings(%+v): %v", f, err)
			}
			if cli, ui := ids(doc.Pendings), ids(got); !slices.Equal(cli, ui) {
				t.Fatalf("%v: cli %v, ListPendings %v", args, cli, ui)
			}
			if s == "all" && ex.args == nil && len(doc.Pendings) != 5 {
				t.Fatalf("--state all lists %d pendings, want 5", len(doc.Pendings))
			}
		}
	}
}
