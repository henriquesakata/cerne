package server

import (
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// withFakePuller swaps the refresh primitive and the clock for the test, and
// clears the per-clone state so tests don't leak into each other.
func withFakePuller(t *testing.T, pull func(string) error) *atomic.Int32 {
	t.Helper()
	var calls atomic.Int32
	origPull, origNow := pullFunc, nowFunc
	pullFunc = func(dir string) error {
		calls.Add(1)
		if pull == nil {
			return nil
		}
		return pull(dir)
	}
	pullMu.Lock()
	pullState = map[string]*repoPull{}
	pullMu.Unlock()
	t.Cleanup(func() {
		pullFunc, nowFunc = origPull, origNow
		pullMu.Lock()
		pullState = map[string]*repoPull{}
		pullMu.Unlock()
	})
	return &calls
}

// TestRefreshProjectThrottles is the core of hub F-0517: a served project's
// clone advances with its backend, but no more than once per TTL.
func TestRefreshProjectThrottles(t *testing.T) {
	calls := withFakePuller(t, nil)
	base := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	now := base
	nowFunc = func() time.Time { return now }

	refreshProject("/repo") // cold: must fetch
	if got := calls.Load(); got != 1 {
		t.Fatalf("first read: pulls = %d, want 1", got)
	}

	now = base.Add(defaultPullTTL - time.Second)
	refreshProject("/repo") // inside the TTL: must not fetch
	if got := calls.Load(); got != 1 {
		t.Fatalf("inside TTL: pulls = %d, want 1", got)
	}

	now = base.Add(defaultPullTTL + time.Second)
	refreshProject("/repo") // past the TTL: must fetch again
	if got := calls.Load(); got != 2 {
		t.Fatalf("past TTL: pulls = %d, want 2", got)
	}
}

// TestRefreshProjectIsPerClone keeps one project's refresh from starving
// another's: the TTL is tracked per repoDir, not globally.
func TestRefreshProjectIsPerClone(t *testing.T) {
	calls := withFakePuller(t, nil)
	nowFunc = func() time.Time { return time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC) }

	refreshProject("/repo-a")
	refreshProject("/repo-b")
	if got := calls.Load(); got != 2 {
		t.Fatalf("pulls = %d, want 2 (one per clone)", got)
	}
}

// TestRefreshProjectSingleFlight: concurrent reads of the same project cost
// one fetch, not one per request.
func TestRefreshProjectSingleFlight(t *testing.T) {
	release := make(chan struct{})
	calls := withFakePuller(t, func(string) error {
		<-release // hold the first fetch open while the others pile up
		return nil
	})
	nowFunc = func() time.Time { return time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC) }

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			refreshProject("/repo")
		}()
	}
	time.Sleep(20 * time.Millisecond) // let the goroutines reach the gate
	close(release)
	wg.Wait()

	if got := calls.Load(); got != 1 {
		t.Fatalf("concurrent reads: pulls = %d, want 1", got)
	}
}

// TestRefreshProjectSurvivesPullError: an unreachable backend must not fail
// the read, and must not be retried on every request either.
func TestRefreshProjectSurvivesPullError(t *testing.T) {
	calls := withFakePuller(t, func(string) error { return errors.New("origin unreachable") })
	base := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	now := base
	nowFunc = func() time.Time { return now }

	refreshProject("/repo")
	now = base.Add(time.Minute)
	refreshProject("/repo")

	if got := calls.Load(); got != 1 {
		t.Fatalf("failing backend: pulls = %d, want 1 (retry only after the TTL)", got)
	}
}

// TestRefreshProjectDisabled: CERNE_SERVE_PULL_TTL=0 is the kill switch — no
// fetch at all, no redeploy needed.
func TestRefreshProjectDisabled(t *testing.T) {
	calls := withFakePuller(t, nil)
	t.Setenv("CERNE_SERVE_PULL_TTL", "0")

	refreshProject("/repo")

	if got := calls.Load(); got != 0 {
		t.Fatalf("disabled: pulls = %d, want 0", got)
	}
}

// TestPullTTLFromEnv covers the parsing contract around the kill switch.
func TestPullTTLFromEnv(t *testing.T) {
	cases := []struct {
		name string
		set  bool
		raw  string
		want time.Duration
	}{
		{name: "unset default", set: false, want: defaultPullTTL},
		{name: "explicit duration", set: true, raw: "30s", want: 30 * time.Second},
		{name: "zero disables", set: true, raw: "0", want: 0},
		{name: "garbage disables", set: true, raw: "later", want: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.set {
				t.Setenv("CERNE_SERVE_PULL_TTL", tc.raw)
			} else {
				// t.Setenv first so the cleanup restores the original value.
				t.Setenv("CERNE_SERVE_PULL_TTL", "")
				if err := os.Unsetenv("CERNE_SERVE_PULL_TTL"); err != nil {
					t.Fatalf("Unsetenv: %v", err)
				}
			}
			if got := pullTTL(); got != tc.want {
				t.Fatalf("pullTTL() = %v, want %v", got, tc.want)
			}
		})
	}
}
