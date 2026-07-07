package app

import (
	"path/filepath"
	"sync"

	"gitlab.com/rockt/cerne/internal/telemetry"
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
			if err := autocommitLock(projDir); err != nil {
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
