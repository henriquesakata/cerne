package cli

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/compile"
)

// backdateLock ages the lock file by d, standing in for a holder that has not
// shown a sign of life for that long.
func backdateLock(t *testing.T, path string, d time.Duration) {
	t.Helper()
	when := time.Now().Add(-d)
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatalf("backdate lock: %v", err)
	}
}

// TestCompileLockCoalescesAndStealsStale pins the lock contract behind the
// single-flight compile (F-0286): the second caller never waits, a LIVE holder
// is never robbed however long its run takes (it touches the lock between
// pages), and a DEAD holder frees the wiki once the window elapses.
func TestCompileLockCoalescesAndStealsStale(t *testing.T) {
	repoDir := filepath.Join(t.TempDir(), "repo")
	path := compileLockPath(repoDir)

	held, ok := tryCompileLock(repoDir)
	if !ok {
		t.Fatalf("first caller failed to take a free lock")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("lock file not created: %v", err)
	}

	// Contended: the second compile must stand down at once, not queue.
	if _, ok := tryCompileLock(repoDir); ok {
		t.Fatalf("second caller took a held lock")
	}

	// A run longer than the window is normal — one page is a whole LLM call —
	// so the holder proves it is alive by touching the lock between pages.
	backdateLock(t, path, compile.LockStaleAfter+time.Minute)
	held.touch()
	if _, ok := tryCompileLock(repoDir); ok {
		t.Fatalf("a touched lock was stolen from a live holder")
	}

	// A holder that died mid-synthesis must not wedge the wiki forever.
	backdateLock(t, path, compile.LockStaleAfter+time.Minute)
	stolen, ok := tryCompileLock(repoDir)
	if !ok {
		t.Fatalf("stale lock was not stolen")
	}

	stolen.release()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("release left the lock behind, stat err = %v", err)
	}
}
