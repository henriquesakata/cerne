package server

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/store"
)

type syncBody struct {
	Ideas    []string
	Trackers []struct {
		Name     string
		Imported int
		Errors   []string
	}
}

func decodeSync(t *testing.T, body []byte) syncBody {
	t.Helper()
	var out syncBody
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("body = %s (%v)", body, err)
	}
	return out
}

// reasonOf is the 409's reason, which the screen reads instead of the message.
func reasonOf(t *testing.T, body []byte) string {
	t.Helper()
	var b struct{ Reason string }
	if err := json.Unmarshal(body, &b); err != nil {
		t.Fatalf("body = %s (%v)", body, err)
	}
	return b.Reason
}

// F-0487 crit. 2
func TestMirrorSyncRoute(t *testing.T) {
	h, repoDir, _, fakes := mirrorFixture(t, true)
	fakes[0].Open("Crash on save", "It crashes.", "")
	jsonHdr := map[string]string{"Content-Type": "application/json"}

	rec := do(h, "POST", "/api/mirror/sync", jsonHdr)
	if rec.Code != 200 {
		t.Fatalf("sync = %d: %s", rec.Code, rec.Body)
	}
	out := decodeSync(t, rec.Body.Bytes())
	if len(out.Ideas) != 1 || len(out.Trackers) != 2 || out.Trackers[0].Name != "a" || out.Trackers[0].Imported != 1 {
		t.Fatalf("first sync = %+v", out)
	}
	if i, err := store.LoadIdea(repoDir, out.Ideas[0]); err != nil || i.Mirror == nil || i.Mirror.Tracker != "a" {
		t.Fatalf("imported idea = %+v (%v)", i, err)
	}
	// Idempotent by the issue URL: a second sync imports nothing.
	if again := decodeSync(t, do(h, "POST", "/api/mirror/sync", jsonHdr).Body.Bytes()); len(again.Ideas) != 0 {
		t.Fatalf("second sync imported %v", again.Ideas)
	}
	// Importing ideas is no human gate: an agent may run it, as on the CLI.
	t.Setenv("CERNE_ACTOR", "agent")
	if rec := do(h, "POST", "/api/mirror/sync", jsonHdr); rec.Code != 200 {
		t.Fatalf("agent sync = %d: %s", rec.Code, rec.Body)
	}
	// It spends the forge credential: another origin, or a body that is not JSON, is refused.
	if rec := do(h, "POST", "/api/mirror/sync", map[string]string{"Origin": "http://evil.test"}); rec.Code != 403 {
		t.Errorf("cross-origin = %d, want 403", rec.Code)
	}
	if rec := do(h, "POST", "/api/mirror/sync", map[string]string{"Content-Type": "text/plain"}); rec.Code != 403 {
		t.Errorf("text/plain = %d, want 403", rec.Code)
	}
}

// F-0487 crit. 2
func TestMirrorSyncRouteTrackerFails(t *testing.T) {
	h, _, _, fakes := mirrorFixture(t, true)
	fakes[1].Err = errors.New("boom")
	rec := do(h, "POST", "/api/mirror/sync", nil)
	if rec.Code != 200 {
		t.Fatalf("sync = %d: %s", rec.Code, rec.Body)
	}
	out := decodeSync(t, rec.Body.Bytes())
	if b := out.Trackers[1]; b.Name != "b" || len(b.Errors) == 0 || !strings.Contains(b.Errors[0], "boom") {
		t.Fatalf("tracker b = %+v", b)
	}
	if a := out.Trackers[0]; len(a.Errors) != 0 {
		t.Fatalf("tracker a = %+v, want no error", a)
	}
}

// F-0487 crit. 2
func TestMirrorSyncRouteOff(t *testing.T) {
	h, _, _, fakes := mirrorFixture(t, false)
	rec := do(h, "POST", "/api/mirror/sync", nil)
	if rec.Code != 409 || !strings.Contains(rec.Body.String(), "mirror is off") || reasonOf(t, rec.Body.Bytes()) != "off" {
		t.Fatalf("sync off = %d: %s", rec.Code, rec.Body)
	}
	if n := len(fakes[0].Calls) + len(fakes[1].Calls); n != 0 {
		t.Fatalf("sync off called the trackers %d time(s)", n)
	}
}

// F-0487 crit. 3
func TestMirrorSyncRouteBusy(t *testing.T) {
	h, repoDir, _, fakes := mirrorFixture(t, true)
	mu := mirrorSyncLock(repoDir)
	mu.Lock() // a sync in flight
	rec := do(h, "POST", "/api/mirror/sync", nil)
	mu.Unlock()
	if rec.Code != 409 || !strings.Contains(rec.Body.String(), "already running") || reasonOf(t, rec.Body.Bytes()) != "running" {
		t.Fatalf("busy sync = %d: %s", rec.Code, rec.Body)
	}
	if n := len(fakes[0].Calls) + len(fakes[1].Calls); n != 0 {
		t.Fatalf("busy sync called the trackers %d time(s)", n)
	}
	if rec := do(h, "POST", "/api/mirror/sync", nil); rec.Code != 200 {
		t.Fatalf("sync after the other ended = %d: %s", rec.Code, rec.Body)
	}
}

// runLoop runs mirrorSyncLoop for d and returns how many rounds synced; it
// waits for the loop to stop, so the fakes can be read without a race.
func runLoop(t *testing.T, repoDir, projDir string, every, d time.Duration) int {
	return runLoopFull(t, repoDir, projDir, every, every, d)
}

func runLoopFull(t *testing.T, repoDir, projDir string, every, full, d time.Duration) int {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	rounds := 0
	done := make(chan struct{})
	go func() {
		mirrorSyncLoop(ctx, repoDir, projDir, every, full, func() { rounds++ })
		close(done)
	}()
	<-done
	return rounds
}

// The loop does a full sync at once, then only imports between full syncs: a
// new outside issue arrives on the next quick round, and no quick round
// touches a mirrored card's issue.
// F-0487 crit. 1
func TestMirrorSyncLoop(t *testing.T) {
	_, repoDir, projDir, fakes := mirrorFixture(t, true)
	fakes[0].Open("Crash on save", "", "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rounds := make(chan int, 8)
	n := 0
	done := make(chan struct{})
	go func() {
		mirrorSyncLoop(ctx, repoDir, projDir, 50*time.Millisecond, time.Hour, func() {
			n++
			if n == 1 { // between rounds, on the loop's goroutine: no race with the fake
				fakes[1].Open("Export the board as CSV", "", "")
			}
			rounds <- n
		})
		close(done)
	}()
	for want := 1; want <= 2; want++ {
		select {
		case <-rounds:
		case <-time.After(20 * time.Second):
			t.Fatalf("round %d never synced", want)
		}
	}
	cancel()
	<-done
	ideas, err := store.LoadAllIdeas(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	from := map[string]int{}
	for _, i := range ideas {
		if i.Mirror != nil {
			from[i.Mirror.Tracker]++
		}
	}
	if from["a"] != 1 || from["b"] != 2 { // b already had I-0001 in the fixture
		t.Fatalf("imported by tracker = %v, want a:1 (full round) and b:2 (quick round)", from)
	}
	updates := 0
	for _, f := range fakes {
		for _, c := range f.Calls {
			if c.Op == "Update" {
				updates++
			}
		}
	}
	if updates != 2 {
		t.Fatalf("card issues updated %d time(s), want 2: only the full round touches the two mirrored cards", updates)
	}
}

// F-0487 crit. 1
func TestMirrorSyncLoopOff(t *testing.T) {
	_, repoDir, projDir, fakes := mirrorFixture(t, false)
	if rounds := runLoop(t, repoDir, projDir, 20*time.Millisecond, 200*time.Millisecond); rounds != 0 {
		t.Fatalf("loop with the mirror off synced %d round(s)", rounds)
	}
	if n := len(fakes[0].Calls) + len(fakes[1].Calls); n != 0 {
		t.Fatalf("loop with the mirror off called the trackers %d time(s)", n)
	}
}

// F-0487 crit. 3
func TestMirrorSyncLoopSkipsWhileBusy(t *testing.T) {
	_, repoDir, projDir, fakes := mirrorFixture(t, true)
	mu := mirrorSyncLock(repoDir)
	mu.Lock()
	rounds := runLoop(t, repoDir, projDir, 20*time.Millisecond, 200*time.Millisecond)
	mu.Unlock()
	if rounds != 0 || len(fakes[0].Calls)+len(fakes[1].Calls) != 0 {
		t.Fatalf("loop ran %d round(s) while a sync was in flight", rounds)
	}
}
