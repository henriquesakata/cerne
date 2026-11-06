//go:build windows

package server

import "syscall"

// ownProcessGroup starts a child in a new process group, so a console Ctrl-C
// on `cerne ui` is not delivered to it.
func ownProcessGroup() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}
