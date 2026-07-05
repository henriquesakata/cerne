package e2e_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"gitlab.com/rockt/cerne/internal/gitback"
)

// compileOnPushProject boots a cerne project wired to a fake Anthropic server
// counting Complete calls, with compile_on_push enabled, and one feature born.
func compileOnPushProject(t *testing.T, srvURL string) (proj string) {
	t.Helper()
	t.Setenv("CERNE_CACHE_DIR", t.TempDir())
	origin := filepath.Join(t.TempDir(), "origin.git")
	if err := gitback.InitBare(origin); err != nil {
		t.Fatalf("InitBare: %v", err)
	}
	proj = t.TempDir()
	// No --module: a moduleless project synthesizes the single "project"
	// fallback page — the wiki shape of this very repo.
	mustCerne(t, "init", "--name", "Demo", "--backend", origin, "--dir", proj)
	extra := "\ncompile_on_push = true\n"
	if srvURL != "" {
		t.Setenv("CERNE_TEST_KEY", "test-key")
		extra += fmt.Sprintf("\n[llm]\nmode = \"byo-key\"\nbase_url = %q\nkey_env = \"CERNE_TEST_KEY\"\n", srvURL)
	}
	appendToFile(t, filepath.Join(proj, ".cernerc"), extra)
	mustCerne(t, "feature", "new", "Motor", "--context", "ctx de teste", "--dir", proj)
	mustCerne(t, "pull", "--dir", proj)
	return proj
}

func TestCompileOnPushGoldenPath(t *testing.T) {
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"content":[{"type":"text","text":"# Wiki sintetizada\n\nPágina do projeto."}]}`)
	}))
	defer srv.Close()
	proj := compileOnPushProject(t, srv.URL)

	// Push a real spec → the wiki project page turns dirty → the hook compiles it.
	writeReal(t, featureDir(t, proj), "spec.md", "Motor")
	out := mustCerne(t, "push", "--dir", proj)
	if !strings.Contains(out, "Wiki recompilada:") {
		t.Fatalf("push output missing wiki recompile notice:\n%s", out)
	}
	if calls.Load() == 0 {
		t.Fatalf("provider was never called")
	}
	// A second push with no source change sends nothing and costs zero LLM
	// calls — compile.Dirty sees clean provenance stamps (crit. 2).
	before := calls.Load()
	out = mustCerne(t, "push", "--dir", proj)
	if !strings.Contains(out, "Nada para enviar.") {
		t.Fatalf("second push should have nothing to send:\n%s", out)
	}
	if calls.Load() != before {
		t.Fatalf("provider called on a no-op push: %d → %d", before, calls.Load())
	}

	// --dry-run never triggers the hook (crit. 7).
	writeReal(t, featureDir(t, proj), "plan.md", "Plano")
	before = calls.Load()
	_, _ = cerne(t, "push", "--dry-run", "--dir", proj)
	if calls.Load() != before {
		t.Fatalf("provider called on dry-run")
	}
}

func TestCompileOnPushFailureDoesNotFailPush(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	proj := compileOnPushProject(t, srv.URL)

	writeReal(t, featureDir(t, proj), "spec.md", "Motor")
	out, err := cerne(t, "push", "--dir", proj)
	if err != nil {
		t.Fatalf("push must succeed even when wiki compile fails: %v\n%s", err, out)
	}
	if !strings.Contains(out, "recompilação da wiki pós-push falhou") {
		t.Fatalf("missing non-fatal warning:\n%s", out)
	}
}

func TestCompileOnPushDelegationWarns(t *testing.T) {
	proj := compileOnPushProject(t, "") // no [llm] → delegation default

	writeReal(t, featureDir(t, proj), "spec.md", "Motor")
	out, err := cerne(t, "push", "--dir", proj)
	if err != nil {
		t.Fatalf("push must succeed in delegation: %v\n%s", err, out)
	}
	if !strings.Contains(out, "compile_on_push exige [llm] chamável") {
		t.Fatalf("missing delegation warning:\n%s", out)
	}
	// No .pending side effects.
	if _, err := os.Stat(filepath.Join(proj, ".docs", "wiki", ".pending")); !os.IsNotExist(err) {
		t.Fatalf(".pending must not be created by the hook, stat err = %v", err)
	}
}
