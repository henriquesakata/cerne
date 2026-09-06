package cli

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// dirtySourceServer boots a fake provider that appends to the feature's spec —
// the source the page is compiled from — on the first dirtyUntil calls, standing
// in for a push that lands WHILE the synthesis is running. It reports the call
// count and any write error (the handler runs off the test goroutine, so it must
// not call t.Fatalf).
func dirtySourceServer(t *testing.T, specPath string, dirtyUntil int64) (*httptest.Server, *atomic.Int64, *atomic.Value) {
	t.Helper()
	var calls atomic.Int64
	var writeErr atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n <= dirtyUntil {
			f, err := os.OpenFile(specPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
			if err == nil {
				_, err = fmt.Fprintf(f, "\n\nEdição %d chegada durante a síntese.\n", n)
				_ = f.Close()
			}
			if err != nil {
				writeErr.Store(err)
			}
		}
		// Long enough to clear compile.ValidatePageBody's floor (F-0287): a
		// 19-character body is refused as "not a page", never stamped.
		_, _ = fmt.Fprintf(w, `{"content":[{"type":"text","text":"# Finance\n\nrodada %d\n\nO módulo finance emite boletos a partir dos pedidos aprovados e reconcilia o retorno do banco. A página é sintetizada das specs vigentes do módulo, e reflete o estado corrente de cada uma delas."}]}`, n)
	}))
	return srv, &calls, &writeErr
}

// absorbProject boots a project with one feature in the "finance" module and
// returns the project dir plus the path of that feature's spec (the page's
// source) and of the compiled page. The caller wires byo-key once its fake
// provider is up.
func absorbProject(t *testing.T) (projDir, specPath, pagePath string) {
	t.Helper()
	projDir, _, repoDir := setupProject(t, "finance")
	if out, err := execCerne(t, "feature", "new", "Boletos", "--context", "ctx de teste", "--module", "finance", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v\n%s", err, out)
	}
	t.Setenv("TEST_BYO_KEY", "sk-byo")
	return projDir,
		filepath.Join(repoDir, "content", "features", "F-0001", "spec.md"),
		filepath.Join(repoDir, "content", "wiki", "modules", "finance.md")
}

// TestCompileAbsorbsSourceChangedMidRun pins why coalescing is safe (F-0286):
// the compile holding the lock does not synthesize a stale snapshot and stop —
// it re-reads the sources and runs another round, so a push that landed while
// the LLM was answering is folded in instead of being dropped on the floor.
func TestCompileAbsorbsSourceChangedMidRun(t *testing.T) {
	projDir, specPath, pagePath := absorbProject(t)
	srv, calls, writeErr := dirtySourceServer(t, specPath, 1) // only the first round dirties
	defer srv.Close()
	enableByoKey(t, projDir, srv.URL, "TEST_BYO_KEY")

	out, err := execCerne(t, "compile", "--dir", projDir)
	if err != nil {
		t.Fatalf("compile: %v\n%s", err, out)
	}
	if e := writeErr.Load(); e != nil {
		t.Fatalf("dirtying the source: %v", e)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("provider called %d time(s), want 2 (one round + the absorbing round)", got)
	}

	// The summary counts DISTINCT pages, not synthesis calls: recompiling the
	// same page in the absorbing round is the loop working, not two pages.
	if !strings.Contains(out, "Compiled 1 page(s)") {
		t.Fatalf("summary counted rounds instead of pages:\n%s", out)
	}
	if n := strings.Count(out, "content/wiki/modules/finance.md"); n != 1 {
		t.Fatalf("page listed %d time(s) in the summary, want 1:\n%s", n, out)
	}

	// The page on disk is the SECOND round's — the one that saw the late edit.
	page, err := os.ReadFile(pagePath)
	if err != nil {
		t.Fatalf("read compiled page: %v", err)
	}
	if !strings.Contains(string(page), "rodada 2") {
		t.Fatalf("page kept the stale first round:\n%s", page)
	}

	// And the loop converged: nothing is dirty any more.
	if out, err = execCerne(t, "compile", "--dir", projDir); err != nil {
		t.Fatalf("second compile: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Nothing to recompile.") {
		t.Fatalf("loop did not converge, second compile says:\n%s", out)
	}
}

// TestCompileAbsorbLoopIsBounded pins the other half of the deal: a project
// under continuous pushes must not pin one compile process forever. Sources
// that go dirty on every round stop the holder at maxCompileRounds, without
// erroring — the leftovers are picked up by the next push's compile.
func TestCompileAbsorbLoopIsBounded(t *testing.T) {
	projDir, specPath, _ := absorbProject(t)
	srv, calls, writeErr := dirtySourceServer(t, specPath, 1000) // never converges
	defer srv.Close()
	enableByoKey(t, projDir, srv.URL, "TEST_BYO_KEY")

	out, err := execCerne(t, "compile", "--dir", projDir)
	if err != nil {
		t.Fatalf("compile: %v\n%s", err, out)
	}
	if e := writeErr.Load(); e != nil {
		t.Fatalf("dirtying the source: %v", e)
	}
	if got := calls.Load(); got != int64(maxCompileRounds) {
		t.Fatalf("provider called %d time(s), want maxCompileRounds=%d", got, maxCompileRounds)
	}
}
