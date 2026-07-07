package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/lint"
)

// countingLintRunner swaps the lint runner for a counter; restore deferred.
func countingLintRunner(t *testing.T) *int {
	t.Helper()
	calls := 0
	prev := lintRunner
	lintRunner = func(repoDir string, staleReadyDays, staleWorkHours int, ignore ...string) ([]lint.Finding, error) {
		calls++
		return nil, nil
	}
	t.Cleanup(func() { lintRunner = prev; resetLintCache() })
	resetLintCache()
	return &calls
}

func getLint(t *testing.T, h http.Handler) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/lint", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("lint status = %d", rec.Code)
	}
}

// TestLintCacheHitOnSameHead pins criterion 4 (F-0153): a repeat with the
// same backend HEAD inside the TTL answers from cache.
func TestLintCacheHitOnSameHead(t *testing.T) {
	calls := countingLintRunner(t)
	repoDir, projDir, _ := newGitBackend(t)
	h := New(repoDir, projDir, "demo")
	getLint(t, h)
	getLint(t, h)
	if *calls != 1 {
		t.Fatalf("lint ran %d times, want 1 (second call cached)", *calls)
	}
}

// TestLintCacheMissOnNewHead: a backend mutation (new HEAD) recomputes.
func TestLintCacheMissOnNewHead(t *testing.T) {
	calls := countingLintRunner(t)
	repoDir, projDir, _ := newGitBackend(t)
	h := New(repoDir, projDir, "demo")
	getLint(t, h)
	writeDoc(t, repoDir, "F-0001", "spec.md", realDoc("F-0001", "Mudou."))
	commitBackend(t, repoDir)
	getLint(t, h)
	if *calls != 2 {
		t.Fatalf("lint ran %d times, want 2 (new HEAD recomputes)", *calls)
	}
}

// TestLintCacheExpiresByTTL: staleness rules depend on the clock, so the
// cache re-runs after the TTL even with an unchanged HEAD.
func TestLintCacheExpiresByTTL(t *testing.T) {
	calls := countingLintRunner(t)
	base := time.Now()
	prevNow := lintNow
	lintNow = func() time.Time { return base }
	t.Cleanup(func() { lintNow = prevNow })

	repoDir, projDir, _ := newGitBackend(t)
	h := New(repoDir, projDir, "demo")
	getLint(t, h)
	lintNow = func() time.Time { return base.Add(lintCacheTTL + time.Second) }
	getLint(t, h)
	if *calls != 2 {
		t.Fatalf("lint ran %d times, want 2 (TTL expired)", *calls)
	}
}

// TestLintNoCacheWithoutGitHead: a git-less backend (no HEAD to key on)
// simply runs every time — no cache, no error.
func TestLintNoCacheWithoutGitHead(t *testing.T) {
	calls := countingLintRunner(t)
	repoDir, projDir := newBackend(t)
	h := New(repoDir, projDir, "demo")
	getLint(t, h)
	getLint(t, h)
	if *calls != 2 {
		t.Fatalf("lint ran %d times, want 2 (no HEAD, no cache)", *calls)
	}
}
