package gitback

import (
	"sync"
	"time"
)

// The sync mark is the F-0156 elision predicate: the moment this PROCESS last
// talked to a repoDir's remote successfully (fetch, pull or push). In-process
// by design — one process never trusts another's sync; a fresh CLI one-shot
// has no marks and therefore never elides.
var (
	syncMu  sync.Mutex
	syncAt  = map[string]time.Time{}
	syncNow = time.Now // injectable clock (tests)
)

// markSynced stamps a successful remote conversation for repoDir.
func markSynced(repoDir string) {
	syncMu.Lock()
	syncAt[repoDir] = syncNow()
	syncMu.Unlock()
}

// SyncedWithin reports whether this process synced repoDir's remote within d.
func SyncedWithin(repoDir string, d time.Duration) bool {
	syncMu.Lock()
	t, ok := syncAt[repoDir]
	syncMu.Unlock()
	return ok && syncNow().Sub(t) < d
}

// resetSyncMarks clears all marks (tests).
func resetSyncMarks() {
	syncMu.Lock()
	syncAt = map[string]time.Time{}
	syncMu.Unlock()
}
