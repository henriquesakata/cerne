package cli

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestRoomWatchRequiresSession(t *testing.T) {
	_, err := runRoom(t, "watch", "R1")
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
		loopErr = roomWatchLoop(ctx, &out, c, "R1", sessA, cpPath, 2, false)
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
		loopErr = roomWatchLoop(ctx, &out, c, "R1", sessA, cpPath, 3, true)
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
	runRoom(t, append([]string{"open", "topic"}, dirFlag...)...)
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
		loopErr = roomWatchLoop(ctx, &out, c, "R1", sess, cpPath, 2, false)
		close(done)
	}()

	time.Sleep(200 * time.Millisecond)
	if _, err := c.close(context.Background(), "R1"); err != nil {
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
		_ = roomWatchLoop(ctx, &out, c, "R1", sessA, cpPath, 3, true)
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
