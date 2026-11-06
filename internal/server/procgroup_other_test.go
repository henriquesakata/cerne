//go:build !windows

package server

import (
	"slices"
	"syscall"
	"testing"
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
