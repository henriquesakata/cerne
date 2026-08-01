package gitback

import (
	"os"
	"path/filepath"
	"sync"
	"time"
)

// The sync mark is the F-0156 elision predicate: the moment this machine last
// talked to a repoDir's remote successfully (fetch, pull or push).
//
// F-0156 kept it in-process on purpose — one process never trusting another's
// sync — and recorded the consequence: every CLI command is a fresh process
// with an empty map, so the CLI could never elide. F-0200 gives the mark an
// on-disk lastro for exactly that case: the map answers first, and a process
// that has none falls back to the stamp a previous command left behind. The
// risk that ruling out persistence avoided (planning a WRITE against a stale
// tracking head) is still covered where it matters — reconcile's retry refetches
// on a non-fast-forward — while the read refresh (SafePull) gets to skip the
// handshake entirely.
var (
	syncMu  sync.Mutex
	syncAt  = map[string]time.Time{}
	syncNow = time.Now // injectable clock (tests)
)

// syncStampPath is the on-disk mark for repoDir. It lives inside .git/ — not in
// the worktree — because a tracked-tree file would show up in WorktreeChanges
// and permanently trip SafePull's own dirty guard.
func syncStampPath(repoDir string) string {
	return filepath.Join(repoDir, ".git", "cerne-synced")
}

// markSynced stamps a successful remote conversation for repoDir, in memory and
// on disk. The stamp is best-effort: a bare repo or an unwritable .git must
// never fail the caller's git operation, it only costs the next process an
// honest fetch. Its mtime comes from syncNow so the injected clock governs the
// window arithmetic on both sides of the mark.
func markSynced(repoDir string) {
	now := syncNow()
	syncMu.Lock()
	syncAt[repoDir] = now
	syncMu.Unlock()

	path := syncStampPath(repoDir)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	_ = f.Close()
	_ = os.Chtimes(path, now, now)
}

// SyncedWithin reports whether this machine synced repoDir's remote within d:
// this process's own mark first, then the stamp a previous process left.
func SyncedWithin(repoDir string, d time.Duration) bool {
	syncMu.Lock()
	t, ok := syncAt[repoDir]
	syncMu.Unlock()
	if !ok {
		fi, err := os.Stat(syncStampPath(repoDir))
		if err != nil {
			return false
		}
		t = fi.ModTime()
	}
	return syncNow().Sub(t) < d
}

// resetSyncMarks clears all marks (tests): "nothing ever synced", so the stamps
// this process wrote go too — otherwise a reset meant to simulate a cold start
// would silently inherit the fixture's own push mark.
func resetSyncMarks() {
	syncMu.Lock()
	for dir := range syncAt {
		_ = os.Remove(syncStampPath(dir))
	}
	syncAt = map[string]time.Time{}
	syncMu.Unlock()
}

// forgetSyncMemory drops the in-process map but leaves the stamps on disk — how
// tests simulate a fresh process (an incoming CLI one-shot) that must still see
// what the previous one synced.
func forgetSyncMemory() {
	syncMu.Lock()
	syncAt = map[string]time.Time{}
	syncMu.Unlock()
}

// ResetSyncMarks is test support for consumer packages whose fixtures push
// (and therefore mark) before exercising the elision paths. Not for
// production use — a process has no reason to forget that it synced.
func ResetSyncMarks() { resetSyncMarks() }
