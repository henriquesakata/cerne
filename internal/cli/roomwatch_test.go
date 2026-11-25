package cli

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/room"
)

func TestCheckpointRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cursor.json")

	if _, ok, err := loadCheckpoint(path); err != nil || ok {
		t.Fatalf("loadCheckpoint on missing file: ok=%v err=%v, want ok=false err=nil", ok, err)
	}

	want := roomCheckpoint{RoomID: "R1", SessionHash: "deadbeef", LastSeq: 7, UpdatedAt: time.Now().UTC()}
	if err := saveCheckpoint(path, want); err != nil {
		t.Fatalf("saveCheckpoint: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("checkpoint perm = %o, want 0600", perm)
	}

	got, ok, err := loadCheckpoint(path)
	if err != nil || !ok {
		t.Fatalf("loadCheckpoint after save: ok=%v err=%v, want ok=true err=nil", ok, err)
	}
	if got.LastSeq != want.LastSeq || got.RoomID != want.RoomID || got.SessionHash != want.SessionHash {
		t.Fatalf("loadCheckpoint = %+v, want %+v", got, want)
	}
}

func TestCheckpointRenameFailureKeepsOldValue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cursor.json")

	if err := saveCheckpoint(path, roomCheckpoint{RoomID: "R1", LastSeq: 1}); err != nil {
		t.Fatalf("initial saveCheckpoint: %v", err)
	}

	old := roomCheckpointRename
	roomCheckpointRename = func(oldpath, newpath string) error {
		return errors.New("simulated crash before rename")
	}
	defer func() { roomCheckpointRename = old }()

	err := saveCheckpoint(path, roomCheckpoint{RoomID: "R1", LastSeq: 2})
	if err == nil {
		t.Fatal("saveCheckpoint with failing rename: want error, got nil")
	}

	roomCheckpointRename = old
	got, ok, err := loadCheckpoint(path)
	if err != nil || !ok {
		t.Fatalf("loadCheckpoint after failed rename: ok=%v err=%v", ok, err)
	}
	if got.LastSeq != 1 {
		t.Fatalf("checkpoint LastSeq = %d after failed rename, want 1 (unchanged — no gap, safe to repeat)", got.LastSeq)
	}
}

func TestLockCheckpointRefusesSecondHolder(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cursor.json")

	release1, err := lockCheckpoint(path)
	if err != nil {
		t.Fatalf("first lockCheckpoint: %v", err)
	}
	defer release1()

	_, err = lockCheckpoint(path)
	if err == nil {
		t.Fatal("second lockCheckpoint on same path: want error, got nil")
	}

	release1()
	release2, err := lockCheckpoint(path)
	if err != nil {
		t.Fatalf("lockCheckpoint after release: %v", err)
	}
	release2()
}

// TestRoomLockHelperProcess is not a test: lockHolder re-executes the test
// binary into it to take the checkpoint lock in another process, report it
// taken, and hold it until the parent kills it (F-0472).
func TestRoomLockHelperProcess(t *testing.T) {
	path := os.Getenv("CERNE_ROOM_LOCK_HELPER")
	if path == "" {
		t.Skip("helper process for the F-0472 lock tests")
	}
	release, err := lockCheckpoint(path)
	if err != nil {
		fmt.Println("error:", err)
		os.Exit(1)
	}
	fmt.Println("locked")
	time.Sleep(time.Minute)
	release()
	os.Exit(0)
}

// lockHolder starts a child process that holds the checkpoint lock at path and
// returns once the child reports the lock taken.
func lockHolder(t *testing.T, path string) *exec.Cmd {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the holder is stopped with SIGKILL, which Windows lacks")
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestRoomLockHelperProcess$")
	cmd.Env = append(os.Environ(), "CERNE_ROOM_LOCK_HELPER="+path)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if strings.TrimSpace(line) != "locked" {
		t.Fatalf("lock holder said %q (read err %v), want \"locked\"", line, err)
	}
	return cmd
}

// F-0472 criterion 1: a holder killed with SIGKILL, which runs no cleanup,
// leaves the lock free for the next watch.
func TestLockCheckpointFreedWhenHolderKilled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cursor.json")
	holder := lockHolder(t, path)
	if err := holder.Process.Kill(); err != nil { // SIGKILL
		t.Fatal(err)
	}
	_ = holder.Wait()

	release, err := lockCheckpoint(path)
	if err != nil {
		t.Fatalf("lockCheckpoint after the holder was killed: %v", err)
	}
	release()
}

// F-0472 criterion 2: while another live process holds the lock, a second
// taker is refused at once.
func TestLockCheckpointRefusedWhileOtherProcessHolds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cursor.json")
	lockHolder(t, path)

	if _, err := lockCheckpoint(path); !errors.Is(err, errCheckpointLocked) {
		t.Fatalf("lockCheckpoint while another process holds it: err = %v, want errCheckpointLocked", err)
	}
}

func TestCheckpointPathHashesSessionNotRaw(t *testing.T) {
	t.Setenv("CERNE_CACHE_DIR", t.TempDir())
	p1, err := checkpointPath("proj-x", "R1", "raw-session-token-should-not-appear")
	if err != nil {
		t.Fatalf("checkpointPath: %v", err)
	}
	if strings.Contains(p1, "raw-session-token-should-not-appear") {
		t.Fatalf("checkpoint path %q leaks the raw session token", p1)
	}
	p2, err := checkpointPath("proj-x", "R1", "raw-session-token-should-not-appear")
	if err != nil {
		t.Fatalf("checkpointPath: %v", err)
	}
	if p1 != p2 {
		t.Fatalf("checkpointPath not deterministic: %q != %q", p1, p2)
	}
}

// settleArrival is one batch (or one read error) the fake room delivers at an
// offset on the fake clock.
type settleArrival struct {
	at     time.Duration
	events []roomWatchEvent
	err    error
}

// settleReader is a fake room on a fake clock, so the --settle tests never
// wait in real time: a read returns the next arrival and moves the clock to
// it; when that arrival lands after the read's deadline, or none is left, it
// moves the clock to the deadline and returns context.DeadlineExceeded, as
// the real client does when the window runs out.
type settleReader struct {
	start, now time.Time
	arrivals   []settleArrival
	calls      int
	deadlines  []time.Time
}

func (r *settleReader) readAsSession(ctx context.Context, id string, since int, session string) ([]roomWatchEvent, error) {
	r.calls++
	deadline, hasDeadline := ctx.Deadline()
	if hasDeadline {
		r.deadlines = append(r.deadlines, deadline)
	}
	if len(r.arrivals) == 0 || (hasDeadline && r.start.Add(r.arrivals[0].at).After(deadline)) {
		if !hasDeadline {
			return nil, errors.New("fake room: a read with no deadline and nothing left to deliver would block forever")
		}
		r.now = deadline
		return nil, context.DeadlineExceeded
	}
	a := r.arrivals[0]
	r.arrivals = r.arrivals[1:]
	r.now = r.start.Add(a.at)
	return a.events, a.err
}

func settleEvent(seq int, typ, nick, body string, own bool) roomWatchEvent {
	return roomWatchEvent{Message: room.Message{Seq: seq, Type: typ, Nick: nick, Body: body}, Own: &own}
}

func settleAt(at time.Duration, events ...roomWatchEvent) settleArrival {
	return settleArrival{at: at, events: events}
}

// runSettle runs one `room watch --once` loop from seq 3 against arrivals on
// a fake clock anchored an hour ahead, so no deadline it computes ever
// expires in real time. It returns the reader, the output and the
// checkpoint's last seq.
func runSettle(t *testing.T, settle time.Duration, arrivals ...settleArrival) (*settleReader, string, int) {
	t.Helper()
	start := time.Now().Add(time.Hour)
	r := &settleReader{start: start, now: start, arrivals: arrivals}
	old := roomWatchNow
	roomWatchNow = func() time.Time { return r.now }
	t.Cleanup(func() { roomWatchNow = old })

	cpPath := filepath.Join(t.TempDir(), "cursor.json")
	var out bytes.Buffer
	if err := roomWatchLoop(context.Background(), &out, r, "R1", "sess", cpPath, 3, true, settle); err != nil {
		t.Fatalf("roomWatchLoop: %v (output %q)", err, out.String())
	}
	cp, ok, err := loadCheckpoint(cpPath)
	if err != nil || !ok {
		t.Fatalf("loadCheckpoint: ok=%v err=%v", ok, err)
	}
	return r, out.String(), cp.LastSeq
}

// E-0006 crit. 5: a burst of 3 events inside the window comes out in one
// --once run, checkpointed at the last, and the run ends after --settle of
// silence counted from the last event, not the first.
func TestRoomWatchSettleCoalescesBurst(t *testing.T) {
	r, out, last := runSettle(t, 1500*time.Millisecond,
		settleAt(0, settleEvent(4, "message", "bob", "um", false)),
		settleAt(400*time.Millisecond, settleEvent(5, "message", "cy", "dois", false)),
		settleAt(900*time.Millisecond, settleEvent(6, "message", "dee", "tres", false)),
	)
	for _, body := range []string{"um", "dois", "tres"} {
		if !strings.Contains(out, body) {
			t.Fatalf("output = %q, want %q from the same burst", out, body)
		}
	}
	if last != 6 {
		t.Fatalf("checkpoint = %d, want 6 (the burst's last event)", last)
	}
	if want := r.start.Add(2400 * time.Millisecond); !r.now.Equal(want) {
		t.Fatalf("watch ended at +%v, want +2.4s (--settle of silence after the last event)", r.now.Sub(r.start))
	}
}

// E-0006 crit. 5: --settle 0 keeps the old --once, ending on the first batch.
func TestRoomWatchSettleZeroExitsOnFirstBatch(t *testing.T) {
	r, out, last := runSettle(t, 0,
		settleAt(0, settleEvent(4, "message", "bob", "um", false)),
		settleAt(100*time.Millisecond, settleEvent(5, "message", "cy", "dois", false)),
	)
	if !strings.Contains(out, "um") || strings.Contains(out, "dois") {
		t.Fatalf("output = %q, want only the first batch", out)
	}
	if last != 4 || r.calls != 1 {
		t.Fatalf("checkpoint = %d after %d read(s), want 4 after 1", last, r.calls)
	}
}

// E-0006 crit. 5: a room that never goes quiet ends the window 5s after the
// first relevant event, whatever --settle says.
func TestRoomWatchSettleCapsAtFiveSeconds(t *testing.T) {
	var arrivals []settleArrival
	for i := 0; i <= 8; i++ { // seq 4..12, one every 900ms, up to +7.2s
		arrivals = append(arrivals, settleAt(time.Duration(i)*900*time.Millisecond, settleEvent(4+i, "message", "bob", fmt.Sprintf("m%d", 4+i), false)))
	}
	r, out, last := runSettle(t, 1500*time.Millisecond, arrivals...)
	limit := r.start.Add(roomWatchSettleCap)
	for _, d := range r.deadlines {
		if d.After(limit) {
			t.Fatalf("a window read had deadline +%v, past the 5s cap", d.Sub(r.start))
		}
	}
	if !strings.Contains(out, "m9") || strings.Contains(out, "m10") {
		t.Fatalf("output = %q, want events up to +4.5s (m9) and none after the cap (m10 at +5.4s)", out)
	}
	if last != 9 || !r.now.Equal(limit) {
		t.Fatalf("checkpoint = %d, ended at +%v; want 9 at +5s", last, r.now.Sub(r.start))
	}
}

// E-0006 crit. 5: your own echo inside the window is not printed but still
// advances the checkpoint.
func TestRoomWatchSettleFiltersOwnEchoButCheckpoints(t *testing.T) {
	_, out, last := runSettle(t, 1500*time.Millisecond,
		settleAt(0, settleEvent(4, "message", "bob", "um", false)),
		settleAt(500*time.Millisecond, settleEvent(5, "message", "cy", "dois", false)),
		settleAt(1000*time.Millisecond, settleEvent(6, "message", "ana", "meu eco", true)),
	)
	if !strings.Contains(out, "um") || !strings.Contains(out, "dois") {
		t.Fatalf("output = %q, want both relevant events", out)
	}
	if strings.Contains(out, "meu eco") {
		t.Fatalf("output = %q, must not print the session's own echo", out)
	}
	if last != 6 {
		t.Fatalf("checkpoint = %d, want 6 (the echo advances it)", last)
	}
}

// E-0006 crit. 5: a dropped connection inside the window ends the run like
// silence does (exit 0, no backoff), so the 5s cap holds; the next --once
// resumes from the checkpoint.
func TestRoomWatchSettleTransientErrorEndsWindow(t *testing.T) {
	var slept []time.Duration
	old := roomWatchSleep
	roomWatchSleep = func(ctx context.Context, d time.Duration) { slept = append(slept, d) }
	t.Cleanup(func() { roomWatchSleep = old })

	_, out, last := runSettle(t, 1500*time.Millisecond,
		settleAt(0, settleEvent(4, "message", "bob", "um", false)),
		settleArrival{at: 500 * time.Millisecond, err: fmt.Errorf("%w: connection reset", errRoomTransient)},
		settleAt(600*time.Millisecond, settleEvent(5, "message", "cy", "dois", false)),
	)
	if !strings.Contains(out, "um") || strings.Contains(out, "dois") {
		t.Fatalf("output = %q, want the first event only", out)
	}
	if last != 4 || len(slept) != 0 {
		t.Fatalf("checkpoint = %d, slept %v; want 4 and no backoff inside the window", last, slept)
	}
}

// E-0006 crit. 6: a close in the first batch ends the watch at once, with
// no settle window.
func TestRoomWatchSettleCloseInFirstBatchEndsAtOnce(t *testing.T) {
	r, out, last := runSettle(t, 1500*time.Millisecond,
		settleAt(0, settleEvent(4, "message", "bob", "um", false), settleEvent(5, "close", "", "", false)),
	)
	if !strings.Contains(out, "close") {
		t.Fatalf("output = %q, want the close event printed", out)
	}
	if last != 5 || r.calls != 1 || !r.now.Equal(r.start) {
		t.Fatalf("checkpoint = %d after %d read(s) at +%v, want 5 after 1 read at +0s", last, r.calls, r.now.Sub(r.start))
	}
}

// E-0006 crit. 6: a close inside the window ends the watch at once, without
// waiting out the rest of --settle.
func TestRoomWatchSettleCloseMidWindowEndsAtOnce(t *testing.T) {
	r, out, last := runSettle(t, 1500*time.Millisecond,
		settleAt(0, settleEvent(4, "message", "bob", "um", false)),
		settleAt(500*time.Millisecond, settleEvent(5, "close", "", "", false)),
		settleAt(1000*time.Millisecond, settleEvent(6, "message", "cy", "depois", false)),
	)
	if !strings.Contains(out, "close") || strings.Contains(out, "depois") {
		t.Fatalf("output = %q, want the close printed and nothing after it", out)
	}
	if last != 5 || r.calls != 2 || !r.now.Equal(r.start.Add(500*time.Millisecond)) {
		t.Fatalf("checkpoint = %d after %d read(s) at +%v, want 5 after 2 reads at +0.5s", last, r.calls, r.now.Sub(r.start))
	}
}
