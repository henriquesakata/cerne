package gitback

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ErrLockTimeout is the sentinel for WithLock failing to acquire the lock
// before lockAcquireTimeout: another live process holds it and it is not stale.
var ErrLockTimeout = errors.New("could not acquire backend lock")

// Lock tuning. Operations under the lock are short (seconds), so a lockfile
// older than lockStaleAfter is treated as a crashed holder and stolen — a crash
// never wedges the project. lockAcquireTimeout is kept above lockStaleAfter so a
// single WithLock call always recovers a crashed lock within one acquire.
// Tests lower these via the unexported vars.
var (
	lockStaleAfter     = 2 * time.Minute
	lockAcquireTimeout = 150 * time.Second
	lockPollInterval   = 25 * time.Millisecond
)

// LockPath is the advisory lock guarding the shared backend clone at repoDir.
// It sits next to the clone (never inside the worktree, so it is never swept
// into a commit): repoDir is <cache>/<id>/repo, so the lock is
// <cache>/<id>/repo.lock.
func LockPath(repoDir string) string {
	return repoDir + ".lock"
}

// WithLock runs fn while holding an exclusive advisory lock at lockPath, so
// concurrent cerne processes serialize their mutations of the shared backend
// clone (which every worktree of a project resolves to). The lock is a lockfile
// created O_EXCL; a holder that died without releasing (lockfile older than
// lockStaleAfter) is stolen. lockPath must live OUTSIDE the git worktree so it
// is never swept into a commit.
func WithLock(lockPath string, fn func() error) error {
	return WithLockWait(lockPath, lockAcquireTimeout, fn)
}

// WithLockWait is WithLock that gives up with ErrLockTimeout after wait. The
// lock is a lockfile, not an OS lock: it is NOT released when the process dies
// (only stolen once older than lockStaleAfter), so callers must not abandon fn.
func WithLockWait(lockPath string, wait time.Duration, fn func() error) error {
	release, err := acquireLock(lockPath, wait)
	if err != nil {
		return err
	}
	defer release()
	return fn()
}

// held is the lockfiles this process image holds, guarded by heldMu together
// with their create and remove. A lockfile naming this PID that is not in held
// was left by the image before an exec: the cerne ui restart (F-0478) execs
// with a pull in flight and no deferred release runs, so the new image must
// not wait lockStaleAfter for its own predecessor (B-0067).
var (
	heldMu sync.Mutex
	held   = map[string]bool{}
)

// acquireLock blocks until it creates lockPath exclusively (or steals a stale
// one), returning a release func that removes it. ErrLockTimeout when a live
// holder keeps it past lockAcquireTimeout.
func acquireLock(lockPath string, wait time.Duration) (func(), error) {
	deadline := time.Now().Add(wait)
	for {
		heldMu.Lock()
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			held[lockPath] = true
			_, _ = fmt.Fprintf(f, "%d\n%d\n", os.Getpid(), time.Now().UnixNano())
			_ = f.Close()
		}
		heldMu.Unlock()
		if err == nil {
			return func() {
				heldMu.Lock()
				defer heldMu.Unlock()
				_ = os.Remove(lockPath)
				delete(held, lockPath)
			}, nil
		}
		if !isContendedCreateErr(err) {
			return nil, err
		}
		if stealOrphanedLock(lockPath) || stealStaleLock(lockPath) {
			continue // stale or vanished: retry the create immediately
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("%w: %s", ErrLockTimeout, lockPath)
		}
		time.Sleep(lockPollInterval)
	}
}

// stealOrphanedLock removes lockPath when it names this PID and this image
// does not hold it (see held). Returns true when the caller should retry the
// create, as stealStaleLock.
func stealOrphanedLock(lockPath string) bool {
	heldMu.Lock()
	defer heldMu.Unlock()
	if held[lockPath] {
		return false
	}
	b, err := os.ReadFile(lockPath)
	if err != nil {
		return errors.Is(err, os.ErrNotExist)
	}
	if pid, _, _ := strings.Cut(string(b), "\n"); pid != strconv.Itoa(os.Getpid()) {
		return false
	}
	_ = os.Remove(lockPath)
	return true
}

// stealStaleLock removes lockPath when it is older than lockStaleAfter (a
// crashed holder). Returns true when the caller should retry the create: the
// lock was removed, or it vanished on its own between the failed create and now.
func stealStaleLock(lockPath string) bool {
	info, err := os.Stat(lockPath)
	if err != nil {
		return errors.Is(err, os.ErrNotExist)
	}
	if time.Since(info.ModTime()) < lockStaleAfter {
		return false
	}
	_ = os.Remove(lockPath)
	return true
}
