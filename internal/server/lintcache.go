package server

import (
	"sync"
	"time"

	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/lint"
	"github.com/henriquesakata/cerne/internal/telemetry"
)

// The full lint pass re-scans the backend (measured 4.9s average on the hub)
// and the UI refetches it after every SSE change event. The cache is keyed by
// backend HEAD (F-0153) — but every transition moves HEAD, so in a burst of
// approvals a plain cache never hits (96 misses vs 3 hits measured, W-0014).
// F-0155 makes it stale-while-revalidate: a divergent HEAD (or expired TTL)
// answers IMMEDIATELY from the cached findings flagged stale, and one
// coalesced background pass refreshes the cache, announcing via the SSE
// change event so the UI refetches. Lint data tolerates lag by nature — its
// staleness rules run on day/hour windows.
const lintCacheTTL = 60 * time.Second

// lintRunner and lintNow are package vars so tests can count runs and move
// the clock. lintBroadcast announces a finished revalidation (pid); the
// `cerne ui` server injects the SSE hub's broadcast, everywhere else no-op.
var (
	lintRunner    = lint.RunConfig
	lintNow       = time.Now
	lintBroadcast = func(pid string) {}
)

type lintCacheEntry struct {
	head     string
	expires  time.Time
	findings []lint.Finding
}

var (
	lintCacheMu sync.Mutex
	lintCache   = map[string]lintCacheEntry{}
	lintReval   = map[string]*lintRevalState{}
)

// lintRevalState is the per-project single-flight of the background pass:
// one running, at most one pending — a burst of invalidations never piles up
// passes (same shape as the autocommit queue, F-0153).
type lintRevalState struct {
	running bool
	again   bool
}

// lintParams carries the config knobs of one pass.
type lintParams struct {
	staleReadyDays, staleWorkHours, staleDecisionDays int
	ignore                         []string
}

// lintFor returns findings for repoDir with stale-while-revalidate semantics
// (F-0155): fresh cache → instant hit; divergent HEAD or expired TTL with a
// cache present → instant STALE answer + coalesced background revalidation;
// no cache (cold start) or no resolvable HEAD (git-less fixture) → the pass
// runs synchronously as before. Phases land on op (nil-safe): `lint_cache`
// fresh hit, `lint_stale` stale hit, `lint` a synchronous real pass.
func lintFor(op *telemetry.Op, projectID, repoDir string, p lintParams) ([]lint.Finding, bool, error) {
	head, err := gitback.HeadSHA(repoDir)
	if err != nil {
		head = ""
	}
	if head != "" {
		lintCacheMu.Lock()
		e, ok := lintCache[repoDir]
		lintCacheMu.Unlock()
		if ok {
			if e.head == head && lintNow().Before(e.expires) {
				op.Phase("lint_cache")()
				return e.findings, false, nil
			}
			// Serve the stale findings now; freshness arrives by SSE.
			op.Phase("lint_stale")()
			scheduleLintRevalidation(projectID, repoDir, p)
			return e.findings, true, nil
		}
	}
	stop := op.Phase("lint")
	start := time.Now()
	findings, err := lintRunner(repoDir, p.staleReadyDays, p.staleWorkHours, p.staleDecisionDays, p.ignore...)
	stop()
	if err != nil {
		return nil, false, err
	}
	telemetry.Default.ObserveLint(projectID, time.Since(start))
	if head != "" {
		storeLintEntry(repoDir, head, findings)
	}
	return findings, false, nil
}

// scheduleLintRevalidation runs one coalesced background pass per project.
func scheduleLintRevalidation(projectID, repoDir string, p lintParams) {
	lintCacheMu.Lock()
	st, ok := lintReval[repoDir]
	if !ok {
		st = &lintRevalState{}
		lintReval[repoDir] = st
	}
	if st.running {
		st.again = true
		lintCacheMu.Unlock()
		return
	}
	st.running = true
	lintCacheMu.Unlock()

	go func() {
		defer func() {
			lintCacheMu.Lock()
			st.running = false
			lintCacheMu.Unlock()
		}()
		for {
			op := telemetry.StartOp("lint_bg")
			op.Project = projectID
			// The HEAD the pass will observe; if it moves mid-run, the next
			// request sees divergence and schedules another round.
			head, herr := gitback.HeadSHA(repoDir)
			start := time.Now()
			stop := op.Phase("lint")
			findings, err := lintRunner(repoDir, p.staleReadyDays, p.staleWorkHours, p.staleDecisionDays, p.ignore...)
			stop()
			if err != nil || herr != nil {
				op.End("error")
			} else {
				telemetry.Default.ObserveLint(projectID, time.Since(start))
				storeLintEntry(repoDir, head, findings)
				op.End("ok")
				lintBroadcast(projectID)
			}
			lintCacheMu.Lock()
			if st.again {
				st.again = false
				lintCacheMu.Unlock()
				continue
			}
			lintCacheMu.Unlock()
			return
		}
	}()
}

func storeLintEntry(repoDir, head string, findings []lint.Finding) {
	lintCacheMu.Lock()
	lintCache[repoDir] = lintCacheEntry{head: head, expires: lintNow().Add(lintCacheTTL), findings: findings}
	lintCacheMu.Unlock()
}

// resetLintCache empties the cache and the revalidation states (tests).
func resetLintCache() {
	lintCacheMu.Lock()
	lintCache = map[string]lintCacheEntry{}
	lintReval = map[string]*lintRevalState{}
	lintCacheMu.Unlock()
}
