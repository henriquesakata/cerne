//go:build windows

package server

import (
	"os"
	"syscall"
)

// ownProcessGroup starts a child in a new process group, so a console Ctrl-C
// on `cerne ui` is not delivered to it.
func ownProcessGroup() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}

// killGroup kills p. ponytail: Windows has no group kill through os; a
// grandchild may outlive it — a job object if that ever matters.
func killGroup(p *os.Process) { _ = p.Kill() }
