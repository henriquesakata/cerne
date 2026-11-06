//go:build !windows

package server

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestWikiCompileCmdOwnProcessGroup pins C3 of F-0316: the on-demand compile
// child leads its own process group, so a Ctrl-C on `cerne ui` (a signal to
// the terminal's foreground group) does not kill it mid-run with the compile
// lock held.
func TestWikiCompileCmdOwnProcessGroup(t *testing.T) {
	c := wikiCompileCmd("/x/cerne", "/p")
	if !slices.Equal(c.Args, []string{"/x/cerne", "compile", "--dir", "/p"}) || c.Dir != "/p" {
		t.Fatalf("cmd = %v in %q", c.Args, c.Dir)
	}
	if c.SysProcAttr == nil || !c.SysProcAttr.Setpgid {
		t.Fatalf("SysProcAttr = %+v, want Setpgid", c.SysProcAttr)
	}

	// And the attribute really detaches: a child started with it is not in
	// this process's group.
	s := wikiCompileCmd("/bin/sleep", "5")
	s.Args = []string{"/bin/sleep", "5"}
	s.Dir = ""
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Process.Kill(); _ = s.Wait() })
	pgid, err := syscall.Getpgid(s.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	if pgid == syscall.Getpgrp() || pgid != s.Process.Pid {
		t.Fatalf("child pgid = %d, ours = %d, child pid = %d: want the child leading its own group", pgid, syscall.Getpgrp(), s.Process.Pid)
	}
}

// TestRunBoundedKillsGroupOnTimeout pins C9 of F-0316: a recompile past its
// deadline is killed with its whole process group (the provider call it is
// stuck in included), answers errWikiCompileTimeout, and frees the compile
// lock it held — so the retry is not 409 busy for the next 30 min. A lock
// another process holds is left alone.
func TestRunBoundedKillsGroupOnTimeout(t *testing.T) {
	dir := t.TempDir()
	lock := filepath.Join(dir, "repo.compile.lock")
	grandchild := filepath.Join(dir, "grandchild.pid")
	// The shell plays a stuck compile: it takes the lock with its own pid and
	// waits on a grandchild in its group.
	c := exec.Command("/bin/sh", "-c", `echo $$ > "$1"; sleep 30 & echo $! > "$2"; wait`, "sh", lock, grandchild)
	c.SysProcAttr = ownProcessGroup()
	start := time.Now()
	_, err := runBounded(c, 500*time.Millisecond, lock)
	if !errors.Is(err, errWikiCompileTimeout) {
		t.Fatalf("err = %v, want errWikiCompileTimeout", err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("runBounded took %v past a 500ms deadline", d)
	}
	if _, err := os.Stat(lock); !os.IsNotExist(err) {
		t.Fatalf("the killed run's lock is still there: %v", err)
	}
	data, err := os.ReadFile(grandchild)
	if err != nil {
		t.Fatal(err)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	deadline := time.Now().Add(3 * time.Second)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			t.Fatalf("grandchild %d outlived the group kill", pid)
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Someone else's lock survives a timeout.
	if err := os.WriteFile(lock, []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c = exec.Command("/bin/sleep", "30")
	c.SysProcAttr = ownProcessGroup()
	if _, err := runBounded(c, 200*time.Millisecond, lock); !errors.Is(err, errWikiCompileTimeout) {
		t.Fatalf("err = %v, want errWikiCompileTimeout", err)
	}
	if _, err := os.Stat(lock); err != nil {
		t.Fatalf("another holder's lock was removed: %v", err)
	}

	// A run inside the deadline returns its output and exit as is.
	out, err := runBounded(exec.Command("/bin/sh", "-c", "echo done"), time.Minute, lock)
	if err != nil || strings.TrimSpace(string(out)) != "done" {
		t.Fatalf("in-time run = %q, %v", out, err)
	}
}
