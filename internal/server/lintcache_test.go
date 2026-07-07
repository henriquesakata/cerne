package server

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/lint"
)

var errTest = errors.New("boom")

// countingLintRunner swaps the lint runner for an atomic counter (the SWR
// revalidator runs it from a goroutine); gate, when non-nil, blocks each run
// until released. Restore + cache reset deferred.
func countingLintRunner(t *testing.T, gate chan struct{}) *atomic.Int32 {
	t.Helper()
	var calls atomic.Int32
	prev := lintRunner
	lintRunner = func(repoDir string, staleReadyDays, staleWorkHours int, ignore ...string) ([]lint.Finding, error) {
		calls.Add(1)
		if gate != nil {
			<-gate
		}
		return nil, nil
	}
	t.Cleanup(func() { lintRunner = prev; resetLintCache() })
	resetLintCache()
	return &calls
}

// waitFor polls cond until true or the deadline; fails the test otherwise.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for !cond() {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for %s", what)
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func getLint(t *testing.T, h http.Handler) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/lint", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("lint status = %d", rec.Code)
	}
	return rec
}

// TestLintCacheHitOnSameHead pins criterion 4 (F-0153): a repeat with the
// same backend HEAD inside the TTL answers from cache, no rerun.
func TestLintCacheHitOnSameHead(t *testing.T) {
	calls := countingLintRunner(t, nil)
	repoDir, projDir, _ := newGitBackend(t)
	h := New(repoDir, projDir, "demo")
	getLint(t, h)
	getLint(t, h)
	if got := calls.Load(); got != 1 {
		t.Fatalf("lint ran %d times, want 1 (second call cached)", got)
	}
}

// TestLintStaleServedOnNewHead pins criteria 1+2 (F-0155): a new HEAD does
// NOT block the request — the old findings come back immediately and the
// recompute happens in background.
func TestLintStaleServedOnNewHead(t *testing.T) {
	calls := countingLintRunner(t, nil)
	repoDir, projDir, _ := newGitBackend(t)
	h := New(repoDir, projDir, "demo")
	getLint(t, h) // cold start, sync
	writeDoc(t, repoDir, "F-0001", "spec.md", realDoc("F-0001", "Mudou."))
	commitBackend(t, repoDir)

	rec := getLint(t, h) // must be served from stale cache, instantly
	if calls.Load() != 1 && calls.Load() != 2 {
		t.Fatalf("unexpected run count %d right after stale hit", calls.Load())
	}
	if body := rec.Body.String(); !contains(body, `"stale": true`) {
		t.Fatalf("stale response not flagged:\n%s", body)
	}
	// background revalidation lands exactly one more run
	waitFor(t, "background revalidation", func() bool { return calls.Load() == 2 })
	waitFor(t, "fresh cache", func() bool {
		return contains(getLint(t, h).Body.String(), `"stale": false`)
	})
	if got := calls.Load(); got != 2 {
		t.Fatalf("lint ran %d times, want 2 (fresh served from cache, no loop)", got)
	}
}

// TestLintRevalidationCoalesces pins criterion 2: K stale hits while a
// recompute is in flight coalesce to at most one follow-up run.
func TestLintRevalidationCoalesces(t *testing.T) {
	gate := make(chan struct{})
	calls := countingLintRunner(t, gate)
	repoDir, projDir, _ := newGitBackend(t)
	h := New(repoDir, projDir, "demo")

	go func() { gate <- struct{}{} }() // release the cold-start sync run
	getLint(t, h)

	writeDoc(t, repoDir, "F-0001", "spec.md", realDoc("F-0001", "Mudou."))
	commitBackend(t, repoDir)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 5; i++ {
			getLint(t, h) // all stale hits; revalidator blocked on the gate
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("stale hits blocked behind the revalidator")
	}
	close(gate) // release background run(s)
	waitFor(t, "runs to settle", func() bool { n := calls.Load(); return n >= 2 && n <= 3 })
	time.Sleep(50 * time.Millisecond) // no burst of queued runs after settling
	if got := calls.Load(); got > 3 {
		t.Fatalf("lint ran %d times for a 5-hit burst, want ≤3 (coalesced)", got)
	}
}

// TestLintBroadcastOnFresh pins criterion 3: a finished revalidation
// announces the project on the injected broadcaster.
func TestLintBroadcastOnFresh(t *testing.T) {
	calls := countingLintRunner(t, nil)
	var announced atomic.Value
	prev := lintBroadcast
	lintBroadcast = func(pid string) { announced.Store(pid) }
	t.Cleanup(func() { lintBroadcast = prev })

	repoDir, projDir, _ := newGitBackend(t)
	h := New(repoDir, projDir, "demo")
	getLint(t, h)
	writeDoc(t, repoDir, "F-0001", "spec.md", realDoc("F-0001", "Mudou."))
	commitBackend(t, repoDir)
	getLint(t, h) // stale → schedules revalidation

	waitFor(t, "broadcast", func() bool { return announced.Load() != nil })
	if pid := announced.Load().(string); pid != "demo" {
		t.Fatalf("broadcast pid = %q, want demo", pid)
	}
	_ = calls
}

// TestLintRevalidatorFailureRetriggers: a failed background run must not wedge
// the single-flight — the next stale hit schedules again.
func TestLintRevalidatorFailureRetriggers(t *testing.T) {
	resetLintCache()
	var calls atomic.Int32
	prev := lintRunner
	lintRunner = func(repoDir string, a, b int, ignore ...string) ([]lint.Finding, error) {
		n := calls.Add(1)
		if n == 2 { // the first background run fails
			return nil, errTest
		}
		return nil, nil
	}
	t.Cleanup(func() { lintRunner = prev; resetLintCache() })

	repoDir, projDir, _ := newGitBackend(t)
	h := New(repoDir, projDir, "demo")
	getLint(t, h)
	writeDoc(t, repoDir, "F-0001", "spec.md", realDoc("F-0001", "Mudou."))
	commitBackend(t, repoDir)
	getLint(t, h) // stale → bg run #2 fails
	waitFor(t, "failed bg run", func() bool { return calls.Load() == 2 })
	getLint(t, h) // still stale → must schedule again
	waitFor(t, "retriggered bg run", func() bool { return calls.Load() == 3 })
}

// TestLintNoCacheWithoutGitHead: a git-less backend (no HEAD to key on)
// simply runs every time — synchronous, no cache, no error.
func TestLintNoCacheWithoutGitHead(t *testing.T) {
	calls := countingLintRunner(t, nil)
	repoDir, projDir := newBackend(t)
	h := New(repoDir, projDir, "demo")
	getLint(t, h)
	getLint(t, h)
	if got := calls.Load(); got != 2 {
		t.Fatalf("lint ran %d times, want 2 (no HEAD, no cache)", got)
	}
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }

// TestLintTTLExpiryGoesStale: same HEAD but expired TTL also takes the stale
// path (staleness rules depend on the clock, not only on content).
func TestLintTTLExpiryGoesStale(t *testing.T) {
	calls := countingLintRunner(t, nil)
	base := time.Now()
	prevNow := lintNow
	lintNow = func() time.Time { return base }
	t.Cleanup(func() { lintNow = prevNow })

	repoDir, projDir, _ := newGitBackend(t)
	h := New(repoDir, projDir, "demo")
	getLint(t, h)
	lintNow = func() time.Time { return base.Add(lintCacheTTL + time.Second) }
	rec := getLint(t, h)
	if !contains(rec.Body.String(), `"stale": true`) {
		t.Fatalf("expired TTL must serve stale:\n%s", rec.Body.String())
	}
	waitFor(t, "revalidation after TTL", func() bool { return calls.Load() == 2 })
}
