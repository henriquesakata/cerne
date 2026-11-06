package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"gitlab.com/rockt/cerne/internal/lint"
	"gitlab.com/rockt/cerne/internal/spectmpl"
	"gitlab.com/rockt/cerne/internal/wiki"
)

// TestWikiOnDemand is F-0316's named trio over one fixture in two states: a
// compile_on_push project with a callable provider whose only lint finding is
// a wiki debt (a page citing F-0999, which does not exist), first with
// wiki.enabled on, then off, then on again.
func TestWikiOnDemand(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"content":[{"type":"text","text":"# Wiki\n\nSíntese."}]}`)
	}))
	defer srv.Close()
	t.Setenv("CERNE_TEST_KEY", "test-key")
	rc := filepath.Join(projDir, ".cernerc")
	data, err := os.ReadFile(rc)
	if err != nil {
		t.Fatal(err)
	}
	// A top-level key goes first: appended after a table it would land inside it.
	mustWrite(t, rc, "compile_on_push = true\n"+string(data)+
		fmt.Sprintf("\n[llm]\nmode = \"byo-key\"\nbase_url = %q\nkey_env = \"CERNE_TEST_KEY\"\n", srv.URL))

	cerneOK := func(t *testing.T, args ...string) string {
		t.Helper()
		out, err := execCerne(t, append(args, "--dir", projDir)...)
		if err != nil {
			t.Fatalf("cerne %v: %v\n%s", args, err, out)
		}
		return out
	}
	cerneOK(t, "feature", "new", "Motor", "--context", "ctx de teste", "--facet", "code")
	cerneOK(t, "pull")

	// The wiki debt: a compiled page whose provenance cites an unknown source.
	page := filepath.Join(repoDir, "content", "wiki", "project.md")
	if err := os.MkdirAll(filepath.Dir(page), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, page, wiki.RenderProvenance(wiki.Provenance{Page: "project", Sources: []string{"F-0001", "F-0999"}, CompiledFrom: "seed", InputHash: "seed"})+"\n# Projeto\n\nSíntese.\n")
	commitBackend(t, repoDir)
	cerneOK(t, "pull")

	// A complete spec, so no other rule joins the wiki debt.
	specs, _ := filepath.Glob(filepath.Join(projDir, ".docs", "current", "features", "F-0001-*", "spec.md"))
	if len(specs) != 1 {
		t.Fatalf("F-0001 spec in .docs: %v", specs)
	}
	spec := specs[0]
	var b strings.Builder
	b.WriteString("---\ncerne_id: F-0001\n---\n\n# Motor\n\n")
	for _, s := range spectmpl.Sections {
		fmt.Fprintf(&b, "## %s\n\nConteúdo real de %s para a fixture do motor.\n\n", s, s)
		if s == "Verificação" {
			b.WriteString("Barra: fixture de teste — referência nomeada\n\n")
		}
	}
	mustWrite(t, spec, b.String())
	edit := func(t *testing.T) {
		t.Helper()
		data, err := os.ReadFile(spec)
		if err != nil {
			t.Fatal(err)
		}
		mustWrite(t, spec, string(data)+"\nMais uma linha.\n")
	}

	if out := cerneOK(t, "push"); !strings.Contains(out, "Wiki recompiling in background") {
		t.Fatalf("control: push with wiki on must start the compile:\n%s", out)
	}
	out, err := execCerne(t, "lint", "--dir", projDir)
	if err == nil || err.Error() != "1 problem(s) found" || !strings.Contains(out, "WIKI-SOURCE-UNKNOWN (1)\n") || !strings.Contains(out, "Total: 1 problem(s).") {
		t.Fatalf("sanity: the fixture's only finding must be the blocking wiki debt: %v\n%s", err, out)
	}
	cerneOK(t, "settings", "set", "wiki.enabled", "false", "--scope", "project")

	debts := func(t *testing.T) []string {
		t.Helper()
		var q struct {
			Debts []struct{ Rule string } `json:"debts"`
		}
		if err := json.Unmarshal([]byte(cerneOK(t, "next", "--json")), &q); err != nil {
			t.Fatal(err)
		}
		var rules []string
		for _, d := range q.Debts {
			rules = append(rules, d.Rule)
		}
		return rules
	}
	hasWikiDebt := func(rules []string) bool {
		for _, r := range rules {
			if r == lint.RuleWikiSourceUnknown {
				return true
			}
		}
		return false
	}

	t.Run("push com wiki off não dispara síntese", func(t *testing.T) {
		edit(t)
		out := cerneOK(t, "push")
		if !strings.Contains(out, "Sent: 1 document(s)") || strings.Contains(out, "Wiki recompiling in background") || strings.Contains(out, "compile_on_push") {
			t.Fatalf("push with wiki off must send without starting the compile:\n%s", out)
		}
		if n := calls.Load(); n != 0 {
			t.Fatalf("provider called %d time(s)", n)
		}
	})

	t.Run("débito de wiki off é informativo, fora do cerne next e sem falha de gate", func(t *testing.T) {
		out := cerneOK(t, "lint")
		if !strings.Contains(out, "WIKI-SOURCE-UNKNOWN (1) (warning: wiki.enabled is off)") || !strings.Contains(out, "all advisory — lint passes") {
			t.Fatalf("lint with wiki off must keep the debt as a labeled advisory:\n%s", out)
		}
		if rules := debts(t); hasWikiDebt(rules) {
			t.Fatalf("next --json debts with wiki off = %v, want no %s", rules, lint.RuleWikiSourceUnknown)
		}
		if out := cerneOK(t, "next"); strings.Contains(out, "WIKI-SOURCE-UNKNOWN") {
			t.Fatalf("next with wiki off lists the wiki debt:\n%s", out)
		}
		edit(t)
		cerneOK(t, "push", "--dry-run")
	})

	t.Run("religar a chave restaura a pendência normal sem reconciliação", func(t *testing.T) {
		cerneOK(t, "settings", "set", "wiki.enabled", "true", "--scope", "project")
		out, err := execCerne(t, "lint", "--dir", projDir)
		if err == nil || err.Error() != "1 problem(s) found" || strings.Contains(out, "wiki.enabled is off") {
			t.Fatalf("lint with wiki back on: err = %v, want '1 problem(s) found' unlabeled\n%s", err, out)
		}
		if rules := debts(t); !hasWikiDebt(rules) {
			t.Fatalf("next --json debts with wiki back on = %v, want %s", rules, lint.RuleWikiSourceUnknown)
		}
		if out := cerneOK(t, "push"); !strings.Contains(out, "Wiki recompiling in background") {
			t.Fatalf("push with wiki back on must start the compile:\n%s", out)
		}
	})
}
