//go:build !windows

package server

import "syscall"

// ownProcessGroup starts a child as the leader of a new process group, out of
// reach of a signal sent to ours (a terminal Ctrl-C on `cerne ui`).
func ownProcessGroup() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setpgid: true} }
