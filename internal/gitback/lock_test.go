package gitback

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// WithLock provides mutual exclusion across goroutines. The lock lives in the
// filesystem (invisible to the race detector), so we prove serialization with
// an atomic "inside" flag — any overlap of two holders fails the CAS — rather
// than a shared plain counter the detector would (correctly) flag.
func TestWithLockSerializes(t *testing.T) {
	defer swapLockTiming(t, time.Minute, time.Minute, time.Millisecond)()
	lockPath := filepath.Join(t.TempDir(), "x.lock")

	const N = 25
	var inside, ran int32
	var wg sync.WaitGroup
	errs := make(chan error, N)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- WithLock(lockPath, func() error {
				if !atomic.CompareAndSwapInt32(&inside, 0, 1) {
					return errors.New("two holders ran concurrently")
				}
				time.Sleep(time.Millisecond) // widen the overlap window
				atomic.AddInt32(&ran, 1)
				atomic.StoreInt32(&inside, 0)
				return nil
			})
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("WithLock: %v", err)
		}
	}
	if got := atomic.LoadInt32(&ran); got != N {
		t.Fatalf("ran = %d, want %d (lock did not serialize)", got, N)
	}
}

// A crashed holder leaves a stale lockfile; the next WithLock steals it instead
// of wedging the project forever.
func TestWithLockStealsStale(t *testing.T) {
	defer swapLockTiming(t, 10*time.Millisecond, time.Second, time.Millisecond)()
	lockPath := filepath.Join(t.TempDir(), "x.lock")
	if err := os.WriteFile(lockPath, []byte("999999\n0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond) // age past lockStaleAfter

	ran := false
	if err := WithLock(lockPath, func() error { ran = true; return nil }); err != nil {
		t.Fatalf("WithLock should steal stale lock: %v", err)
	}
	if !ran {
		t.Fatal("fn did not run after stealing stale lock")
	}
}

// B-0067: a lockfile naming this very PID that no goroutine of this process
// holds was left by the image before an exec (the cerne ui restart runs no
// deferred release). It is taken at once, not after lockStaleAfter.
func TestWithLockTakesOwnPIDOrphanAtOnce(t *testing.T) {
	defer swapLockTiming(t, time.Hour, 200*time.Millisecond, time.Millisecond)()
	lockPath := filepath.Join(t.TempDir(), "repo.lock")
	if err := os.WriteFile(lockPath, []byte(strconv.Itoa(os.Getpid())+"\n0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ran := false
	if err := WithLock(lockPath, func() error { ran = true; return nil }); err != nil {
		t.Fatalf("WithLock waited on the lockfile its own pre-exec image left: %v", err)
	}
	if !ran {
		t.Fatal("fn did not run")
	}
	if _, err := os.Stat(lockPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("lockfile not released after fn: %v", err)
	}
}

// B-0067 critic 1: two containers sharing a cache volume both run as PID 1. A
// lockfile with this PID written on another host is a live peer, never taken.
func TestWithLockKeepsSamePIDFromAnotherHost(t *testing.T) {
	defer swapLockTiming(t, time.Hour, 100*time.Millisecond, time.Millisecond)()
	lockPath := filepath.Join(t.TempDir(), "repo.lock")
	body := strconv.Itoa(os.Getpid()) + "\n0\nnot-" + lockHost() + "\n"
	if err := os.WriteFile(lockPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	err := WithLock(lockPath, func() error { t.Error("ran under a peer's live lock"); return nil })
	if !errors.Is(err, ErrLockTimeout) {
		t.Fatalf("err = %v, want ErrLockTimeout", err)
	}
}

// A live holder that keeps the lock past the acquire timeout yields ErrLockTimeout.
func TestWithLockTimeout(t *testing.T) {
	defer swapLockTiming(t, time.Hour, 40*time.Millisecond, 5*time.Millisecond)()
	lockPath := filepath.Join(t.TempDir(), "x.lock")

	acquired := make(chan struct{})
	release := make(chan struct{})
	go func() {
		_ = WithLock(lockPath, func() error {
			close(acquired)
			<-release
			return nil
		})
	}()
	<-acquired
	err := WithLock(lockPath, func() error { return nil })
	close(release)
	if !errors.Is(err, ErrLockTimeout) {
		t.Fatalf("err = %v, want ErrLockTimeout", err)
	}
}

// swapLockTiming sets the package lock-timing vars for a test and returns a
// restore func.
func swapLockTiming(t *testing.T, stale, acquire, poll time.Duration) func() {
	t.Helper()
	s0, a0, p0 := lockStaleAfter, lockAcquireTimeout, lockPollInterval
	lockStaleAfter, lockAcquireTimeout, lockPollInterval = stale, acquire, poll
	return func() { lockStaleAfter, lockAcquireTimeout, lockPollInterval = s0, a0, p0 }
}

// B-0017: WithLockWait gives up after its own wait, not the package default.
func TestWithLockWaitTimesOut(t *testing.T) {
	path := filepath.Join(t.TempDir(), "repo.lock")
	err := WithLock(path, func() error {
		start := time.Now()
		werr := WithLockWait(path, 50*time.Millisecond, func() error { t.Error("ran under a held lock"); return nil })
		if !errors.Is(werr, ErrLockTimeout) || time.Since(start) > 5*time.Second {
			t.Errorf("want ErrLockTimeout quickly, got %v after %v", werr, time.Since(start))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
