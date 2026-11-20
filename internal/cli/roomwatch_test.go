package cli

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
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
