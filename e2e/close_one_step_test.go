package e2e_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/cost"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/project"
)

// TestCloseFeatureInOneCommand (F-0402 crit. 8): in a throwaway project (own
// .cernerc, bare backend, cache dir and ledger, a transcript for the bound
// session) one `feature complete` makes the transition, stamps the cost and
// ends with the Next step block; the backend, the lock and origin agree.
func TestCloseFeatureInOneCommand(t *testing.T) {
	t.Setenv("CERNE_CACHE_DIR", t.TempDir())
	claude := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", claude)
	origin := filepath.Join(t.TempDir(), "origin.git")
	if err := gitback.InitBare(origin); err != nil {
		t.Fatal(err)
	}
	proj := t.TempDir()
	mustCerne(t, "init", "--docs-language", "pt-BR", "--name", "Demo", "--backend", origin, "--module", "core", "--dir", proj)

	mustCerne(t, "feature", "new", "Fechar em um passo", "--context", "ctx de teste", "--facet", "code", "--module", "core", "--dir", proj)
	mustCerne(t, "pull", "--dir", proj)
	writeReal(t, featureDir(t, proj), "spec.md", "Fechar em um passo")
	mustCerne(t, "push", "--dir", proj)
	mustCerne(t, "feature", "approve", "F-0001", "--dir", proj)
	mustCerne(t, "pull", "--dir", proj)
	writeReal(t, featureDir(t, proj), "plan.md", "Plano")
	mustCerne(t, "push", "--dir", proj)
	mustCerne(t, "feature", "start", "F-0001", "--dir", proj)
	mustCerne(t, "pull", "--dir", proj)
	writeReal(t, featureDir(t, proj), "result.md", "Resultado")
	mustCerne(t, "push", "--dir", proj)

	// A test ledger: one session bound to F-0001, with one assistant message.
	cfg, _, err := project.LoadConfig(proj)
	if err != nil {
		t.Fatal(err)
	}
	cache, err := project.CacheDir(cfg.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	bound := time.Now().UTC().Add(-time.Hour)
	if err := cost.AppendBind(cache, cost.Bind{Session: "e2e-sess", Entity: "F-0001", Harness: cost.HarnessClaudeCode, TS: bound}); err != nil {
		t.Fatal(err)
	}
	line := `{"type":"assistant","timestamp":"` + time.Now().UTC().Add(-30*time.Minute).Format(time.RFC3339) +
		`","requestId":"req-1","message":{"id":"m1","model":"claude-opus-5-5","usage":{"input_tokens":10,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"output_tokens":20}}}` + "\n"
	tp := filepath.Join(claude, "projects", "-tmp-p", "e2e-sess.jsonl")
	if err := os.MkdirAll(filepath.Dir(tp), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tp, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}

	out := mustCerne(t, "feature", "complete", "F-0001", "--dir", proj)
	for _, want := range []string{
		"F-0001: in_development → implemented\n",
		"cost stamped: ",
		"Next step:\n  - human accept: cerne feature accept F-0001 (or drag the card to Done)\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("complete output lacks %q:\n%s", want, out)
		}
	}
	if strings.Count(out, "Next step") != 1 {
		t.Errorf("want one Next step block:\n%s", out)
	}
	if got := mustCerne(t, "show", "F-0001", "--doc", "result", "--dir", proj); !strings.Contains(got, "<!-- cerne:cost v1 {") {
		t.Errorf("result carries no cost anchor:\n%s", got)
	}
	assertLockParity(t, proj, origin, "after complete")
}
