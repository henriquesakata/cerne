package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/room"
)

func TestRoomWatchRequiresSession(t *testing.T) {
	dir, _ := newRoomTestServer(t, "proj-watch-nosession") // --dir: never the repo's .cernerc
	_, err := runRoom(t, "watch", "R1", "--dir", dir)
	if err == nil {
		t.Fatal("watch without session: want error, got nil")
	}
	if !strings.Contains(err.Error(), "--session") || !strings.Contains(err.Error(), "CERNE_ROOM_SESSION") {
		t.Fatalf("error = %q, want both --session and CERNE_ROOM_SESSION mentioned", err.Error())
	}
}

func TestResolveWatchStartPrecedence(t *testing.T) {
	dir, port := newRoomTestServer(t, "proj-watch-precedence")
	dirFlag := []string{"--dir", dir, "--port", strconv.Itoa(port)}
	runRoom(t, append([]string{"open", "topic"}, dirFlag...)...)
	sess := joinRoomAs(t, dirFlag, "R1", "ana") // R1 now at seq 2 (open, join)

	c, err := newRoomClient(dir, port)
	if err != nil {
		t.Fatalf("newRoomClient: %v", err)
	}
	cpPath := filepath.Join(t.TempDir(), "cursor.json")

	// No --since, no checkpoint: falls back to resume().LastSeq (the tail).
	start, err := resolveWatchStart(context.Background(), c, "R1", sess, cpPath, 0, false)
	if err != nil {
		t.Fatalf("resolveWatchStart (tail): %v", err)
	}
	if start != 2 {
		t.Fatalf("start = %d, want 2 (resume's last_seq after open+join)", start)
	}

	// A saved checkpoint wins over resume's last_seq, even if that's stale.
	if err := saveCheckpoint(cpPath, roomCheckpoint{RoomID: "R1", LastSeq: 1}); err != nil {
		t.Fatalf("saveCheckpoint: %v", err)
	}
	start, err = resolveWatchStart(context.Background(), c, "R1", sess, cpPath, 0, false)
	if err != nil {
		t.Fatalf("resolveWatchStart (checkpoint): %v", err)
	}
	if start != 1 {
		t.Fatalf("start = %d, want 1 (the checkpoint, ignoring resume's tail)", start)
	}

	// --since overrides both, consciously.
	start, err = resolveWatchStart(context.Background(), c, "R1", sess, cpPath, 99, true)
	if err != nil {
		t.Fatalf("resolveWatchStart (--since): %v", err)
	}
	if start != 99 {
		t.Fatalf("start = %d, want 99 (--since wins over checkpoint and resume)", start)
	}
}

func TestResolveWatchStartInvalidSessionIsDefinitiveError(t *testing.T) {
	dir, port := newRoomTestServer(t, "proj-watch-badsession")
	dirFlag := []string{"--dir", dir, "--port", strconv.Itoa(port)}
	runRoom(t, append([]string{"open", "topic"}, dirFlag...)...)

	c, err := newRoomClient(dir, port)
	if err != nil {
		t.Fatalf("newRoomClient: %v", err)
	}
	cpPath := filepath.Join(t.TempDir(), "cursor.json")

	_, err = resolveWatchStart(context.Background(), c, "R1", "not-a-real-session", cpPath, 0, false)
	if err == nil {
		t.Fatal("resolveWatchStart with bogus session: want error, got nil")
	}
}

func TestResolveWatchStartUnknownRoomIsDefinitiveError(t *testing.T) {
	dir, port := newRoomTestServer(t, "proj-watch-noroom")
	c, err := newRoomClient(dir, port)
	if err != nil {
		t.Fatalf("newRoomClient: %v", err)
	}
	cpPath := filepath.Join(t.TempDir(), "cursor.json")

	_, err = resolveWatchStart(context.Background(), c, "R404", "whatever", cpPath, 0, false)
	if err == nil {
		t.Fatal("resolveWatchStart on unknown room: want error, got nil")
	}
}

func TestRoomWatchLoopFiltersOwnEcho(t *testing.T) {
	dir, port := newRoomTestServer(t, "proj-watch-echo")
	dirFlag := []string{"--dir", dir, "--port", strconv.Itoa(port)}
	runRoom(t, append([]string{"open", "topic"}, dirFlag...)...)
	sessA := joinRoomAs(t, dirFlag, "R1", "ana")
	sessB := joinRoomAs(t, dirFlag, "R1", "bob")

	c, err := newRoomClient(dir, port)
	if err != nil {
		t.Fatalf("newRoomClient: %v", err)
	}
	cpPath := filepath.Join(t.TempDir(), "cursor.json")

	done := make(chan struct{})
	var out bytes.Buffer
	var loopErr error
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		loopErr = roomWatchLoop(ctx, &out, c, "R1", sessA, cpPath, 2, false, 0)
		close(done)
	}()

	time.Sleep(200 * time.Millisecond)
	if _, err := c.say(context.Background(), "R1", sessA, "eco de ana"); err != nil {
		t.Fatalf("say (ana): %v", err)
	}
	if _, err := c.say(context.Background(), "R1", sessB, "fala do bob"); err != nil {
		t.Fatalf("say (bob): %v", err)
	}
	<-done
	if loopErr != nil {
		t.Fatalf("roomWatchLoop: %v", loopErr)
	}
	if strings.Contains(out.String(), "eco de ana") {
		t.Fatalf("watch output = %q, must filter ana's own echo", out.String())
	}
	if !strings.Contains(out.String(), "fala do bob") {
		t.Fatalf("watch output = %q, want bob's message visible", out.String())
	}

	cp, ok, err := loadCheckpoint(cpPath)
	if err != nil || !ok {
		t.Fatalf("loadCheckpoint: ok=%v err=%v", ok, err)
	}
	if cp.LastSeq < 4 {
		t.Fatalf("checkpoint LastSeq = %d, want it advanced past both messages (echo included)", cp.LastSeq)
	}
}

func TestRoomWatchLoopOnceWaitsPastEcho(t *testing.T) {
	dir, port := newRoomTestServer(t, "proj-watch-once-echo")
	dirFlag := []string{"--dir", dir, "--port", strconv.Itoa(port)}
	runRoom(t, append([]string{"open", "topic"}, dirFlag...)...)
	sessA := joinRoomAs(t, dirFlag, "R1", "ana")
	sessB := joinRoomAs(t, dirFlag, "R1", "bob")

	c, err := newRoomClient(dir, port)
	if err != nil {
		t.Fatalf("newRoomClient: %v", err)
	}
	cpPath := filepath.Join(t.TempDir(), "cursor.json")

	done := make(chan struct{})
	var out bytes.Buffer
	var loopErr error
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		// start = 3: past open(1)+ana join(2)+bob join(3), the tail before
		// this watch begins — otherwise bob's own pre-existing join would
		// already count as a first "relevant" event for --once.
		loopErr = roomWatchLoop(ctx, &out, c, "R1", sessA, cpPath, 3, true, 0)
		close(done)
	}()

	time.Sleep(200 * time.Millisecond)
	if _, err := c.say(context.Background(), "R1", sessA, "so eco"); err != nil {
		t.Fatalf("say (ana): %v", err)
	}
	// give --once a chance to (wrongly) return on the echo-only batch
	time.Sleep(200 * time.Millisecond)
	select {
	case <-done:
		t.Fatal("roomWatchLoop with --once returned after an echo-only batch, want it to keep waiting")
	default:
	}

	if _, err := c.say(context.Background(), "R1", sessB, "agora sim"); err != nil {
		t.Fatalf("say (bob): %v", err)
	}
	<-done
	if loopErr != nil {
		t.Fatalf("roomWatchLoop: %v", loopErr)
	}
	if !strings.Contains(out.String(), "agora sim") {
		t.Fatalf("watch output = %q, want bob's message", out.String())
	}
}

func TestRoomWatchLoopClosePrintsCheckpointsAndExits(t *testing.T) {
	dir, port := newRoomTestServer(t, "proj-watch-close")
	dirFlag := []string{"--dir", dir, "--port", strconv.Itoa(port)}
	_, facilitator := openRoomCLI(t, dirFlag, "topic")
	sess := joinRoomAs(t, dirFlag, "R1", "ana")

	c, err := newRoomClient(dir, port)
	if err != nil {
		t.Fatalf("newRoomClient: %v", err)
	}
	cpPath := filepath.Join(t.TempDir(), "cursor.json")

	done := make(chan struct{})
	var out bytes.Buffer
	var loopErr error
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		loopErr = roomWatchLoop(ctx, &out, c, "R1", sess, cpPath, 2, false, 0)
		close(done)
	}()

	time.Sleep(200 * time.Millisecond)
	if _, err := c.close(context.Background(), "R1", facilitator, room.Minutes{Conclusion: "ok", Rationale: "ok"}, false); err != nil {
		t.Fatalf("close: %v", err)
	}
	<-done
	if loopErr != nil {
		t.Fatalf("roomWatchLoop on close: %v", loopErr)
	}
	if !strings.Contains(out.String(), "close") {
		t.Fatalf("watch output = %q, want the close event printed", out.String())
	}
	cp, ok, err := loadCheckpoint(cpPath)
	if err != nil || !ok {
		t.Fatalf("loadCheckpoint after close: ok=%v err=%v", ok, err)
	}
	if cp.LastSeq != 3 {
		t.Fatalf("checkpoint LastSeq = %d, want 3 (the close event's seq)", cp.LastSeq)
	}
}

func TestRoomWatchTwoProcessesSameCheckpointSecondFails(t *testing.T) {
	dir, port := newRoomTestServer(t, "proj-watch-lock")
	dirFlag := []string{"--dir", dir, "--port", strconv.Itoa(port)}
	runRoom(t, append([]string{"open", "topic"}, dirFlag...)...)
	sess := joinRoomAs(t, dirFlag, "R1", "ana")

	cpPath := filepath.Join(t.TempDir(), "shared-cursor.json")
	release, err := lockCheckpoint(cpPath)
	if err != nil {
		t.Fatalf("lockCheckpoint: %v", err)
	}
	defer release()

	_, err = runRoom(t, append([]string{"watch", "R1", "--session", sess, "--once", "--cursor-file", cpPath}, dirFlag...)...)
	if err == nil {
		t.Fatal("watch on a locked checkpoint: want error, got nil")
	}
	if !strings.Contains(err.Error(), "already running") {
		t.Fatalf("error = %q, want a clear already-running message", err.Error())
	}
}

func TestRoomWatchLoopPrintsSameFormatAsRead(t *testing.T) {
	dir, port := newRoomTestServer(t, "proj-watch-format")
	dirFlag := []string{"--dir", dir, "--port", strconv.Itoa(port)}
	runRoom(t, append([]string{"open", "topic"}, dirFlag...)...)
	sessA := joinRoomAs(t, dirFlag, "R1", "ana")
	sessB := joinRoomAs(t, dirFlag, "R1", "bob")

	c, err := newRoomClient(dir, port)
	if err != nil {
		t.Fatalf("newRoomClient: %v", err)
	}
	cpPath := filepath.Join(t.TempDir(), "cursor.json")

	done := make(chan struct{})
	var out bytes.Buffer
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		// start = 3: past open(1)+ana join(2)+bob join(3) — see the same
		// note in TestRoomWatchLoopOnceWaitsPastEcho.
		_ = roomWatchLoop(ctx, &out, c, "R1", sessA, cpPath, 3, true, 0)
		close(done)
	}()
	time.Sleep(200 * time.Millisecond)
	if _, err := c.say(context.Background(), "R1", sessB, "oi"); err != nil {
		t.Fatalf("say: %v", err)
	}
	<-done
	if !strings.Contains(out.String(), fmt.Sprintf("[%s] oi", "bob")) {
		t.Fatalf("watch output = %q, want the same '[nick] body' rendering as read", out.String())
	}
}

// runRoomTrapped runs a `cerne room` command under startTrapped (the signal
// wiring is what is under test), writing its output to out.
func runRoomTrapped(t *testing.T, out *syncBuf, args ...string) chan error {
	t.Helper()
	cmd := newRoomCmd()
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs(args)
	return startTrapped(t, cmd.Execute)
}

// waitOutput polls out until it contains want.
func waitOutput(t *testing.T, out *syncBuf, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(out.String(), want) {
		if time.Now().After(deadline) {
			t.Fatalf("output = %q, want it to contain %q", out.String(), want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// assertLockFree checks that no process holds the checkpoint lock: a fresh
// lockCheckpoint on the same path succeeds. The .lock file may stay on disk;
// the OS lock on it, not the file, marks it held (F-0472).
func assertLockFree(t *testing.T, cpPath string) {
	t.Helper()
	release, err := lockCheckpoint(cpPath)
	if err != nil {
		t.Fatalf("checkpoint lock still held after the signal: %v", err)
	}
	release()
}

// F-0472 criterion 3: a .lock file an earlier version left on disk, with no
// live process holding it, does not block the watch.
func TestRoomWatchIgnoresLeftoverLockFile(t *testing.T) {
	dir, port := newRoomTestServer(t, "proj-watch-leftover")
	dirFlag := []string{"--dir", dir, "--port", strconv.Itoa(port)}
	runRoom(t, append([]string{"open", "topic"}, dirFlag...)...)
	sessA := joinRoomAs(t, dirFlag, "R1", "ana")
	sessB := joinRoomAs(t, dirFlag, "R1", "bob") // R1 now at seq 3 (open, 2 joins)
	c, err := newRoomClient(dir, port)
	if err != nil {
		t.Fatalf("newRoomClient: %v", err)
	}
	cpPath := filepath.Join(t.TempDir(), "cursor.json")
	if err := os.WriteFile(cpPath+".lock", nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := c.say(context.Background(), "R1", sessB, "depois da queda"); err != nil {
		t.Fatalf("say: %v", err)
	}

	out, err := runRoom(t, append([]string{"watch", "R1", "--session", sessA, "--once", "--since", "3", "--cursor-file", cpPath}, dirFlag...)...)
	if err != nil {
		t.Fatalf("watch with a leftover .lock file: %v", err)
	}
	if !strings.Contains(out, "depois da queda") {
		t.Fatalf("watch output = %q, want bob's line", out)
	}
}

// F-0467 criterion 1: a room watch stopped with SIGTERM mid-loop exits clean
// and releases the checkpoint lock, so the same session can watch again.
func TestRoomWatchSIGTERMReleasesLock(t *testing.T) {
	dir, port := newRoomTestServer(t, "proj-watch-sigterm")
	dirFlag := []string{"--dir", dir, "--port", strconv.Itoa(port)}
	runRoom(t, append([]string{"open", "topic"}, dirFlag...)...)
	sessA := joinRoomAs(t, dirFlag, "R1", "ana")
	sessB := joinRoomAs(t, dirFlag, "R1", "bob") // R1 now at seq 3 (open, 2 joins)
	c, err := newRoomClient(dir, port)
	if err != nil {
		t.Fatalf("newRoomClient: %v", err)
	}
	cpPath := filepath.Join(t.TempDir(), "cursor.json")
	watch := append([]string{"watch", "R1", "--session", sessA, "--cursor-file", cpPath}, dirFlag...)

	out := &syncBuf{}
	// --since pins the start, so bob's line is seen even if it lands first.
	done := runRoomTrapped(t, out, append(watch, "--since", "3")...)
	if _, err := c.say(context.Background(), "R1", sessB, "primeira"); err != nil {
		t.Fatalf("say: %v", err)
	}
	waitOutput(t, out, "primeira") // the watch is in its loop

	sendSelf(t, syscall.SIGTERM)
	waitExit(t, done, "room watch")
	assertLockFree(t, cpPath)

	if _, err := c.say(context.Background(), "R1", sessB, "segunda"); err != nil {
		t.Fatalf("say: %v", err)
	}
	if _, err := runRoom(t, append(watch, "--once")...); err != nil {
		t.Fatalf("second watch on the same session: %v", err)
	}
}

// F-0467 criterion 2: a signal while the watch still resolves its start
// cursor (lock taken, loop not reached) releases the lock too.
func TestRoomWatchSignalDuringStartReleasesLock(t *testing.T) {
	resumed := make(chan struct{}, 1)
	unblock := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/sessions/resume") {
			select {
			case resumed <- struct{}{}:
			default:
			}
			select {
			case <-r.Context().Done():
			case <-unblock:
			}
		}
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(unblock) }) // LIFO: frees a stuck handler before srv.Close
	port := srv.Listener.Addr().(*net.TCPAddr).Port

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".cernerc"), []byte("project_id = \"p\"\nbackend = \"/dev/null\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cpPath := filepath.Join(t.TempDir(), "cursor.json")
	done := runRoomTrapped(t, &syncBuf{}, "watch", "R1", "--session", "s", "--cursor-file", cpPath,
		"--dir", dir, "--port", strconv.Itoa(port))
	select {
	case <-resumed:
	case <-time.After(5 * time.Second):
		t.Fatal("the watch never asked the server for its start cursor")
	}

	sendSelf(t, syscall.SIGTERM)
	waitExit(t, done, "room watch")
	assertLockFree(t, cpPath)
}

// T-0020 crit. 1 and 2
// E-0006 crit. 5 and 6: `room watch --once` takes --settle (1.5s by default)
// and hands it to the loop, so a line said after the first one is printed
// lands in the same run. A minute-long window keeps the test off the clock;
// the close ends the run at once.
func TestRoomWatchSettleFlagKeepsOnceOpen(t *testing.T) {
	if f := newRoomWatchCmd().Flags().Lookup("settle"); f == nil || f.DefValue != "1.5s" {
		t.Fatalf("--settle flag = %+v, want it with a 1.5s default", f)
	}
	dir, port := newRoomTestServer(t, "proj-watch-settle")
	dirFlag := []string{"--dir", dir, "--port", strconv.Itoa(port)}
	_, facilitator := openRoomCLI(t, dirFlag, "topic")
	sessA := joinRoomAs(t, dirFlag, "R1", "ana")
	sessB := joinRoomAs(t, dirFlag, "R1", "bob") // R1 now at seq 3 (open, 2 joins)
	c, err := newRoomClient(dir, port)
	if err != nil {
		t.Fatalf("newRoomClient: %v", err)
	}
	cpPath := filepath.Join(t.TempDir(), "cursor.json")

	out := &syncBuf{}
	done := runRoomTrapped(t, out, append([]string{"watch", "R1", "--session", sessA, "--once", "--settle", "1m", "--since", "3", "--cursor-file", cpPath}, dirFlag...)...)
	if _, err := c.say(context.Background(), "R1", sessB, "primeira"); err != nil {
		t.Fatalf("say: %v", err)
	}
	waitOutput(t, out, "primeira")
	if _, err := c.say(context.Background(), "R1", sessB, "segunda"); err != nil {
		t.Fatalf("say: %v", err)
	}
	waitOutput(t, out, "segunda")
	if _, err := c.close(context.Background(), "R1", facilitator, room.Minutes{Conclusion: "ok", Rationale: "ok"}, false); err != nil {
		t.Fatalf("close: %v", err)
	}
	waitExit(t, done, "room watch --once --settle")
	if cp, ok, err := loadCheckpoint(cpPath); err != nil || !ok || cp.LastSeq != 6 {
		t.Fatalf("checkpoint = %+v (ok=%v err=%v), want last_seq 6 (the close)", cp, ok, err)
	}
}

// B-0084, review round 2: the watch cursor's path joins the room id too, and
// lockCheckpoint creates its directory before any server call, so
// `watch ../../x` is refused before anything lands outside the cache.
func TestRoomWatchRefusesRoomIDThatIsAPath(t *testing.T) {
	dir, port := newRoomTestServer(t, "proj-watch-path")
	dirFlag := []string{"--dir", dir, "--port", strconv.Itoa(port)}
	cache := os.Getenv("CERNE_CACHE_DIR")
	if _, err := checkpointPath("proj-watch-path", "../x", "s"); !errors.Is(err, errRoomIDNotLocal) {
		t.Errorf("checkpointPath(../x) err = %v, want errRoomIDNotLocal", err)
	}
	_, err := runRoom(t, append([]string{"watch", "../../x", "--session", "abc", "--once"}, dirFlag...)...)
	if !errors.Is(err, errRoomIDNotLocal) {
		t.Errorf("watch ../../x err = %v, want errRoomIDNotLocal", err)
	}
	if _, err := os.Stat(filepath.Join(cache, "x")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("watch ../../x created %s: %v", filepath.Join(cache, "x"), err)
	}
}
