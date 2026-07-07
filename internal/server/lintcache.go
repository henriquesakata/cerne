package server

import (
	"sync"
	"time"

	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/lint"
	"gitlab.com/rockt/cerne/internal/telemetry"
)

// The full lint pass re-scans the backend (3-5s on the hub) and the UI
// refetches it after every SSE change event — so it is cached per backend
// HEAD (F-0153): any mutation moves HEAD and invalidates naturally. The TTL
// exists because the stale-ready/stale-work rules depend on the clock, not
// only on content; 60s of staleness lag is irrelevant for day/hour windows.
const lintCacheTTL = 60 * time.Second

// lintRunner and lintNow are package vars so tests can count runs and move
// the clock.
var (
	lintRunner = lint.RunConfig
	lintNow    = time.Now
)

type lintCacheEntry struct {
	head     string
	expires  time.Time
	findings []lint.Finding
}

var (
	lintCacheMu sync.Mutex
	lintCache   = map[string]lintCacheEntry{}
)

// cachedLintFindings returns the lint findings for repoDir, from cache when
// the backend HEAD is unchanged and the TTL has not passed. A backend without
// a resolvable HEAD (git-less fixture) just runs every time. Phases land on
// op (nil-safe): a hit records `lint_cache` (~0ms), a real pass records
// `lint` wrapping the run. The bool reports a hit (the caller only feeds the
// per-project histogram on real passes).
func cachedLintFindings(op *telemetry.Op, repoDir string, staleReadyDays, staleWorkHours int, ignore []string) ([]lint.Finding, bool, error) {
	head, err := gitback.HeadSHA(repoDir)
	if err != nil {
		head = ""
	}
	if head != "" {
		lintCacheMu.Lock()
		e, ok := lintCache[repoDir]
		lintCacheMu.Unlock()
		if ok && e.head == head && lintNow().Before(e.expires) {
			op.Phase("lint_cache")()
			return e.findings, true, nil
		}
	}
	stop := op.Phase("lint")
	findings, err := lintRunner(repoDir, staleReadyDays, staleWorkHours, ignore...)
	stop()
	if err != nil {
		return nil, false, err
	}
	if head != "" {
		lintCacheMu.Lock()
		lintCache[repoDir] = lintCacheEntry{head: head, expires: lintNow().Add(lintCacheTTL), findings: findings}
		lintCacheMu.Unlock()
	}
	return findings, false, nil
}

// resetLintCache empties the cache (tests).
func resetLintCache() {
	lintCacheMu.Lock()
	lintCache = map[string]lintCacheEntry{}
	lintCacheMu.Unlock()
}
