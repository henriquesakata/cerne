package server

import (
	"os"
	"sync"
	"time"

	"gitlab.com/rockt/cerne/internal/gitback"
)

// defaultPullTTL bounds how often a served project's clone is refreshed from
// its backend. Five minutes keeps a freshly pushed note reaching a downstream
// harness well inside the one-hour budget (hub F-0517 criterion 2) while a
// busy service pays at most one fetch per project per interval.
const defaultPullTTL = 5 * time.Minute

// pullFunc is the refresh primitive — a package variable so tests can observe
// and fail it without touching git.
var pullFunc = gitback.SafePull

// nowFunc is time.Now, swappable in tests to advance past the TTL.
var nowFunc = time.Now

// repoPull is the per-clone refresh state. Its mutex is also the single-flight
// gate: concurrent readers of the same project serialize here, and all but the
// first find lastPull inside the TTL and return without fetching.
type repoPull struct {
	mu       sync.Mutex
	lastPull time.Time
}

var (
	pullMu    sync.Mutex
	pullState = map[string]*repoPull{}
)

// pullTTL reads CERNE_SERVE_PULL_TTL (a Go duration, e.g. "30s", "10m").
// Unset falls back to defaultPullTTL; "0" (or any non-positive/unparseable
// value) disables the refresh, which is the no-redeploy kill switch when a
// backend is unreachable and the fetch cost is not worth paying.
func pullTTL() time.Duration {
	raw, ok := os.LookupEnv("CERNE_SERVE_PULL_TTL")
	if !ok {
		return defaultPullTTL
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0
	}
	return d
}

// refreshProject brings repoDir up to date with its backend, at most once per
// TTL per clone. It exists because `cerne serve` clones a project exactly once
// (ensureProjectClone, on register) and never advanced it again: the served
// catalog stayed pinned at the SHA of registration day (hub F-0517).
//
// The refresh is synchronous on purpose — the read that follows a push must
// see the push, not the one after it. It is also best-effort: a failed fetch
// (offline, auth, dirty clone) still stamps lastPull, so a broken backend is
// retried once per TTL instead of on every request, and the handler keeps
// serving the clone it already has.
func refreshProject(repoDir string) {
	ttl := pullTTL()
	if ttl <= 0 {
		return
	}
	pullMu.Lock()
	st, ok := pullState[repoDir]
	if !ok {
		st = &repoPull{}
		pullState[repoDir] = st
	}
	pullMu.Unlock()

	st.mu.Lock()
	defer st.mu.Unlock()
	if !st.lastPull.IsZero() && nowFunc().Sub(st.lastPull) < ttl {
		return
	}
	_ = pullFunc(repoDir)
	st.lastPull = nowFunc()
}
