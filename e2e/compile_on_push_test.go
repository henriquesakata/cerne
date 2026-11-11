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

	"github.com/henriquesakata/cerne/internal/gitback"
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

// TestCompileOnPushGoldenPath pins the async contract: the push hands the wiki
// off to a detached `cerne compile` and returns without ever calling the LLM
// itself, and that deferred compile really does have work waiting for it.
func TestCompileOnPushGoldenPath(t *testing.T) {
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"content":[{"type":"text","text":"# Wiki sintetizada\n\nPágina do projeto. Visão geral do módulo sintetizada a partir das features vigentes; cada seção respeita o estado declarado da fonte e nada aqui é inferido além do que as specs afirmam."}]}`)
	}))
	defer srv.Close()
	proj := compileOnPushProject(t, srv.URL)

	// Push a real spec → the wiki project page turns dirty → the hook spawns
	// the compile instead of running it (crit. 1: the push never blocks on an LLM).
	writeReal(t, featureDir(t, proj), "spec.md", "Motor")
	out := mustCerne(t, "push", "--dir", proj)
	if !strings.Contains(out, "Wiki recompiling in background") {
		t.Fatalf("push output missing the background notice:\n%s", out)
	}
	if calls.Load() != 0 {
		t.Fatalf("push called the LLM inline: %d call(s)", calls.Load())
	}

	// The work the push deferred is real: running the compile compiles the page.
	out = mustCerne(t, "compile", "--dir", proj)
	if !strings.Contains(out, "Compiled 1 page(s)") {
		t.Fatalf("deferred compile did nothing:\n%s", out)
	}
	if calls.Load() == 0 {
		t.Fatalf("provider was never called")
	}

	// A second compile with no source change costs zero LLM calls — compile.Dirty
	// sees clean provenance stamps (crit. 2).
	before := calls.Load()
	out = mustCerne(t, "compile", "--dir", proj)
	if !strings.Contains(out, "Nothing to recompile.") {
		t.Fatalf("second compile should be a no-op:\n%s", out)
	}
	if calls.Load() != before {
		t.Fatalf("provider called on a no-op compile: %d → %d", before, calls.Load())
	}

	// --dry-run never triggers the hook (crit. 7).
	writeReal(t, featureDir(t, proj), "plan.md", "Plano")
	out, _ = cerne(t, "push", "--dry-run", "--dir", proj)
	if strings.Contains(out, "Wiki recompiling in background") {
		t.Fatalf("dry-run triggered the compile hook:\n%s", out)
	}
}

// TestCompileOnPushConcurrentIsCoalesced pins the anti-queue rule: a compile
// started while another one holds the lock returns immediately instead of
// stacking a second LLM run over the same pages.
func TestCompileOnPushConcurrentIsCoalesced(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"content":[{"type":"text","text":"# Wiki\n\nVisão geral do módulo sintetizada a partir das features vigentes; cada seção respeita o estado declarado da fonte e nada aqui é inferido além do que as specs afirmam. As partes se relacionam pela emissão do boleto e pela conciliação do pagamento."}]}`)
	}))
	defer srv.Close()
	proj := compileOnPushProject(t, srv.URL)
	writeReal(t, featureDir(t, proj), "spec.md", "Motor")
	mustCerne(t, "push", "--dir", proj)

	// Stand in for a live holder: the lock file another compile process left.
	repos, err := filepath.Glob(filepath.Join(os.Getenv("CERNE_CACHE_DIR"), "*", "repo"))
	if err != nil || len(repos) != 1 {
		t.Fatalf("locating the backend clone: %v %v", repos, err)
	}
	lock := repos[0] + ".compile.lock"
	if err := os.WriteFile(lock, []byte("1\n"), 0o644); err != nil {
		t.Fatalf("write lock: %v", err)
	}
	out := mustCerne(t, "compile", "--dir", proj)
	if !strings.Contains(out, "already running") {
		t.Fatalf("contended compile should stand down, got:\n%s", out)
	}

	// Once the holder is gone, the same dirty page compiles normally.
	if err := os.Remove(lock); err != nil {
		t.Fatalf("remove lock: %v", err)
	}
	if out = mustCerne(t, "compile", "--dir", proj); !strings.Contains(out, "Compiled 1 page(s)") {
		t.Fatalf("compile after release did nothing:\n%s", out)
	}
}

func TestCompileOnPushDelegationWarns(t *testing.T) {
	proj := compileOnPushProject(t, "") // no [llm] → delegation default

	writeReal(t, featureDir(t, proj), "spec.md", "Motor")
	out, err := cerne(t, "push", "--dir", proj)
	if err != nil {
		t.Fatalf("push must succeed in delegation: %v\n%s", err, out)
	}
	if !strings.Contains(out, "compile_on_push requires a callable [llm]") {
		t.Fatalf("missing delegation warning:\n%s", out)
	}
	// No .pending side effects.
	if _, err := os.Stat(filepath.Join(proj, ".docs", "wiki", ".pending")); !os.IsNotExist(err) {
		t.Fatalf(".pending must not be created by the hook, stat err = %v", err)
	}
}
