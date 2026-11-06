//go:build !windows

package server

import (
	"os"
	"syscall"
)

// ownProcessGroup starts a child as the leader of a new process group, out of
// reach of a signal sent to ours (a terminal Ctrl-C on `cerne ui`).
func ownProcessGroup() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setpgid: true} }

// killGroup kills the process group p leads (ownProcessGroup): p and every
// child it started, the provider call it is stuck in included.
func killGroup(p *os.Process) { _ = syscall.Kill(-p.Pid, syscall.SIGKILL) }
