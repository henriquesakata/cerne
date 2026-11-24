package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestNoticeAppendAndTake (F-0504 crit. 5): the notices of a session come out
// once, in order, ahead of nothing else, and the file is gone afterwards.
func TestNoticeAppendAndTake(t *testing.T) {
	cacheDir := t.TempDir()
	now := time.Date(2026, 11, 24, 21, 0, 0, 0, time.UTC)
	for _, line := range []string{"F-0001: candidate → approved by local", "F-0002: implemented → done by local"} {
		if err := appendNotice(cacheDir, "sess-a", notice{Entity: line[:6], Line: line, At: now}); err != nil {
			t.Fatalf("appendNotice: %v", err)
		}
	}
	got := TakeNotices(cacheDir, "sess-a", now.Add(time.Minute))
	want := "cerne: card state changed since this session last looked:\n" +
		"- F-0001: candidate → approved by local\n" +
		"- F-0002: implemented → done by local\n"
	if got != want {
		t.Fatalf("TakeNotices =\n%q\nwant\n%q", got, want)
	}
	if again := TakeNotices(cacheDir, "sess-a", now.Add(time.Minute)); again != "" {
		t.Fatalf("second TakeNotices = %q, want empty (shown once)", again)
	}
	if left, _ := filepath.Glob(filepath.Join(cacheDir, "notices", "*")); len(left) != 0 {
		t.Fatalf("notices left behind: %v", left)
	}
	if other := TakeNotices(cacheDir, "sess-b", now); other != "" {
		t.Fatalf("TakeNotices of another session = %q, want empty", other)
	}
}

// TestNoticeTakeDropsOld (F-0504 crit. 5): a notice older than the channel TTL
// is deleted without being shown.
func TestNoticeTakeDropsOld(t *testing.T) {
	cacheDir := t.TempDir()
	now := time.Date(2026, 11, 24, 21, 0, 0, 0, time.UTC)
	if err := appendNotice(cacheDir, "s", notice{Entity: "F-0001", Line: "F-0001: old", At: now.Add(-25 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := appendNotice(cacheDir, "s", notice{Entity: "F-0002", Line: "F-0002: fresh", At: now.Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	got := TakeNotices(cacheDir, "s", now)
	if strings.Contains(got, "old") || !strings.Contains(got, "F-0002: fresh") {
		t.Fatalf("TakeNotices = %q, want only the fresh notice", got)
	}
	if err := appendNotice(cacheDir, "s", notice{Entity: "F-0001", Line: "F-0001: old", At: now.Add(-25 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if got := TakeNotices(cacheDir, "s", now); got != "" {
		t.Fatalf("TakeNotices with only an old notice = %q, want empty", got)
	}
	if _, err := os.Stat(filepath.Join(cacheDir, "notices", "s.ndjson")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old notice file still there: %v", err)
	}
}

// TestNoticeTakeConcurrent (F-0504 crit. 5): two contact points at once — a
// PreToolUse and a PostToolUse — show each notice once in total.
func TestNoticeTakeConcurrent(t *testing.T) {
	cacheDir := t.TempDir()
	now := time.Now().UTC()
	for range 20 {
		if err := appendNotice(cacheDir, "s", notice{Entity: "F-0001", Line: "F-0001: x", At: now}); err != nil {
			t.Fatal(err)
		}
	}
	var mu sync.Mutex
	var all strings.Builder
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got := TakeNotices(cacheDir, "s", now)
			mu.Lock()
			all.WriteString(got)
			mu.Unlock()
		}()
	}
	wg.Wait()
	if n := strings.Count(all.String(), "- F-0001: x\n"); n != 20 {
		t.Fatalf("notices shown %d times in total, want 20", n)
	}
}

// TestNoticeUnsafeSession (F-0504 crit. 5): a session id that could escape the
// notices dir is refused on write and reads as nothing.
func TestNoticeUnsafeSession(t *testing.T) {
	cacheDir := t.TempDir()
	for _, sess := range []string{"", "../x", "a/b", "s.agent"} {
		if err := appendNotice(cacheDir, sess, notice{Entity: "F-0001", Line: "x", At: time.Now()}); !errors.Is(err, ErrUnsafeSessionID) {
			t.Fatalf("appendNotice(%q) = %v, want ErrUnsafeSessionID", sess, err)
		}
		if got := TakeNotices(cacheDir, sess, time.Now()); got != "" {
			t.Fatalf("TakeNotices(%q) = %q, want empty", sess, got)
		}
	}
}
