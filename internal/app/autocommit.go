package app

import (
	"path/filepath"
	"sync"

	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/telemetry"
)

// The docs.lock pin (autocommitLock) is best-effort by design, so it never
// belongs on the mutation's latency path (F-0153): a burst of transitions
// enqueues here and coalesces — the pin lands once, after the dust settles.
// The lock file itself is always written synchronously (updateSnapshot);
// only the convenience commit is deferred. A process dying with a pin
// pending loses nothing: the next advance pins again.

// autocommitHookBeforeCommit, when non-nil, runs before each deferred pin
// commit — tests gate it to prove mutations don't wait and bursts coalesce.
var autocommitHookBeforeCommit func()

var (
	autocommitMu      sync.Mutex
	autocommitWG      sync.WaitGroup
	autocommitPending = map[string]*autocommitState{}
)

type autocommitState struct {
	running bool
	again   bool
}

// enqueueAutocommit schedules the docs.lock pin for projDir. One worker per
// project: a request arriving while a pin is in flight marks one (and only
// one) follow-up round, so N rapid mutations produce at most two commits —
// and usually one, since the last-written lock is what the first round pins.
func enqueueAutocommit(projDir string) {
	autocommitMu.Lock()
	st, ok := autocommitPending[projDir]
	if !ok {
		st = &autocommitState{}
		autocommitPending[projDir] = st
	}
	if st.running {
		st.again = true
		autocommitMu.Unlock()
		return
	}
	st.running = true
	autocommitMu.Unlock()

	autocommitWG.Add(1)
	go func() {
		defer autocommitWG.Done()
		for {
			if autocommitHookBeforeCommit != nil {
				autocommitHookBeforeCommit()
			}
			op := telemetry.StartOp("autocommit")
			op.Project = filepath.Base(projDir)
			err := autocommitLock(projDir)
			if outcome := pushPin(projDir); outcome != "" {
				op.Phase("pin-push=" + outcome)()
			}
			if err != nil {
				op.End("error")
			} else {
				op.End("ok")
			}
			autocommitMu.Lock()
			if st.again {
				st.again = false
				autocommitMu.Unlock()
				continue
			}
			st.running = false
			autocommitMu.Unlock()
			return
		}
	}()
}

// FlushAutocommits blocks until every pending docs.lock pin has landed. The
// CLI calls it before exiting so one-shot commands keep their synchronous
// semantics; the long-running servers never call it.
func FlushAutocommits() {
	autocommitWG.Wait()
}

// pushPin publishes the docs.lock pin to the project's origin (F-0180),
// closing the divergence window that made every pull on the pin branch a
// merge: with the pin already on origin, other machines fast-forward. Opt-in
// via push_pin, and gated hard on origin/<pin_branch>..HEAD carrying nothing
// but pin commits — the user's own unpublished work is never pushed by cerne.
// Best-effort like autocommitLock: failures become a telemetry outcome, never
// an error, and an unpublished pin self-heals on the next round. Returns ""
// when push_pin is off (so pre-F-0180 telemetry records stay identical),
// otherwise "ok", "skipped" or "error".
func pushPin(projDir string) string {
	cfg, _, err := project.LoadConfig(projDir)
	if err != nil || !cfg.PushPin || cfg.PinBranch == "" {
		return ""
	}
	branch, err := gitback.CurrentBranch(projDir)
	if err != nil || branch != cfg.PinBranch {
		return "skipped"
	}
	// Refresh the tracking ref so the pin-only guard judges against the
	// remote's current truth, not a stale one.
	if err := gitback.Fetch(projDir); err != nil {
		return "error"
	}
	onlyPins, err := gitback.AheadTouchesOnly(projDir, cfg.PinBranch, "docs.lock")
	if err != nil {
		return "error"
	}
	if !onlyPins {
		return "skipped"
	}
	if err := gitback.PushBranch(projDir, cfg.PinBranch); err != nil {
		return "error"
	}
	return "ok"
}
