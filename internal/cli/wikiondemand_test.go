package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/lint"
	"gitlab.com/rockt/cerne/internal/spectmpl"
	"gitlab.com/rockt/cerne/internal/wiki"
)

// recordSpawns replaces the detached compile spawn with a counter for the
// test's lifetime: under go test the real spawn is skipped, so the decision to
// spawn is what a test can observe.
func recordSpawns(t *testing.T) *int {
	t.Helper()
	n := 0
	orig := startBackgroundCompile
	startBackgroundCompile = func(projCtx) error { n++; return nil }
	t.Cleanup(func() { startBackgroundCompile = orig })
	return &n
}

// compileOnPushCallable opts projDir into compile_on_push with a callable
// [llm] mode; no provider is ever reached (the spawn is recorded, not run).
func compileOnPushCallable(t *testing.T, projDir string) {
	t.Helper()
	rc := filepath.Join(projDir, ".cernerc")
	data, err := os.ReadFile(rc)
	if err != nil {
		t.Fatal(err)
	}
	// A top-level key goes first: appended after a table it would land inside it.
	mustWrite(t, rc, "compile_on_push = true\n"+string(data)+"\n[llm]\nmode = \"byo-key\"\nbase_url = \"http://127.0.0.1:1\"\nkey_env = \"CERNE_TEST_KEY\"\n")
}

// TestWikiOnDemand is F-0316's named trio over one fixture in two states: a
// compile_on_push project with a callable provider whose only lint finding is
// a wiki debt (a page citing F-0999, which does not exist), first with
// wiki.enabled on, then off, then on again.
func TestWikiOnDemand(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	spawns := recordSpawns(t)
	compileOnPushCallable(t, projDir)

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

	if out := cerneOK(t, "push"); !strings.Contains(out, "Wiki recompiling in background") || *spawns != 1 {
		t.Fatalf("control: push with wiki on must spawn the compile (spawns = %d):\n%s", *spawns, out)
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
		if *spawns != 1 {
			t.Fatalf("push with wiki off spawned the compile: spawns = %d, want still 1", *spawns)
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
		if out := cerneOK(t, "push"); !strings.Contains(out, "Wiki recompiling in background") || *spawns != 2 {
			t.Fatalf("push with wiki back on must spawn the compile (spawns = %d):\n%s", *spawns, out)
		}
	})
}

// TestWikiEnabledBrokenSourceWarns pins that a broken wiki.enabled source —
// a malformed host settings file, an invalid project value — still falls
// through as F-0314 resolves it, but push and lint say so on stderr, naming
// the scope, the file and the value used, instead of flipping the wiki back
// on in silence.
func TestWikiEnabledBrokenSourceWarns(t *testing.T) {
	cases := []struct {
		name, project, host string
		warn                func(projDir, hostDir string) string
	}{
		{"malformed host file", "", "[wiki]\nenabled = false\n[graph\n", func(_, hostDir string) string {
			return "warning: wiki.enabled unreadable in host scope (" + filepath.Join(hostDir, "settings.toml") + "); using default true"
		}},
		{"invalid project value", "\n[wiki]\nenabled = \"no\"\n", "", func(projDir, _ string) string {
			return "warning: wiki.enabled invalid in project scope (" + filepath.Join(projDir, ".cernerc") + "); using default true"
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			projDir, _, _ := setupProject(t)
			recordSpawns(t)
			compileOnPushCallable(t, projDir)
			hostDir := t.TempDir()
			t.Setenv("CERNE_CONFIG_DIR", hostDir)
			if tc.host != "" {
				mustWrite(t, filepath.Join(hostDir, "settings.toml"), tc.host)
			}
			if tc.project != "" {
				rc := filepath.Join(projDir, ".cernerc")
				data, err := os.ReadFile(rc)
				if err != nil {
					t.Fatal(err)
				}
				mustWrite(t, rc, string(data)+tc.project)
			}
			newOriginFeature(t, projDir, "Motor", "--facet", "code")
			if out, err := execCerne(t, "pull", "--dir", projDir); err != nil {
				t.Fatalf("pull: %v\n%s", err, out)
			}
			specs, _ := filepath.Glob(filepath.Join(projDir, ".docs", "current", "features", "F-0001-*", "spec.md"))
			if len(specs) != 1 {
				t.Fatalf("F-0001 spec in .docs: %v", specs)
			}
			data, err := os.ReadFile(specs[0])
			if err != nil {
				t.Fatal(err)
			}
			mustWrite(t, specs[0], string(data)+"\nMais contexto.\n")
			want := tc.warn(projDir, hostDir)

			out, err := execCerne(t, "push", "--dir", projDir)
			if err != nil || !strings.Contains(out, want) {
				t.Fatalf("push: %v, want the warning %q:\n%s", err, want, out)
			}
			out, _ = execCerne(t, "lint", "--dir", projDir)
			if !strings.Contains(out, want) {
				t.Fatalf("lint, want the warning %q:\n%s", want, out)
			}
		})
	}
}

// TestLintFixTasksOmitsAdvisory pins C6 of F-0316 for --fix-tasks, the list
// pasted into an agent as work: advisory findings — a stale pending (F-0322),
// wiki debt with the wiki off — stay in the report but are no task.
func TestLintFixTasksOmitsAdvisory(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	newOriginFeature(t, projDir, "Origem", "--facet", "code")
	pendingNew(t, projDir, "Débito velho", "--from", "F-0001")
	ageInBackend(t, repoDir, "P-0001", true)
	page := filepath.Join(repoDir, "content", "wiki", "project.md")
	if err := os.MkdirAll(filepath.Dir(page), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, page, wiki.RenderProvenance(wiki.Provenance{Page: "project", Sources: []string{"F-0999"}, CompiledFrom: "seed", InputHash: "seed"})+"\n# Projeto\n")
	commitBackend(t, repoDir)
	if out, err := execCerne(t, "settings", "set", "wiki.enabled", "false", "--scope", "project", "--dir", projDir); err != nil {
		t.Fatalf("settings set: %v\n%s", err, out)
	}

	out, _ := execCerne(t, "lint", "--fix-tasks", "--dir", projDir)
	for _, shown := range []string{"STALE-PENDING (1) (warning)", "WIKI-SOURCE-UNKNOWN (1) (warning: wiki.enabled is off)"} {
		if !strings.Contains(out, shown) {
			t.Fatalf("report lacks %q:\n%s", shown, out)
		}
	}
	_, tasks, ok := strings.Cut(out, "## Maintenance tasks")
	if !ok || !strings.Contains(tasks, "W-0001") {
		t.Fatalf("fix-tasks lacks the blocking stale work:\n%s", out)
	}
	for _, advisory := range []string{"P-0001", "content/wiki/project.md"} {
		if strings.Contains(tasks, advisory) {
			t.Fatalf("fix-tasks lists the advisory %s as work:\n%s", advisory, tasks)
		}
	}
}
