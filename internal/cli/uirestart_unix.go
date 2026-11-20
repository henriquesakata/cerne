//go:build unix

package cli

import (
	"fmt"
	"net"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// dupListener keeps the panel's socket open across exec: a copy of its
// descriptor without close-on-exec, so the port is never free while this
// process turns into the new binary (F-0478 criterion 3).
func dupListener(ln net.Listener) (*os.File, error) {
	tl, ok := ln.(*net.TCPListener)
	if !ok {
		return nil, fmt.Errorf("a %T cannot be handed over", ln)
	}
	f, err := tl.File()
	if err != nil {
		return nil, err
	}
	if _, err := unix.FcntlInt(f.Fd(), unix.F_SETFD, 0); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

// restartUI replaces this process with the binary on disk: same PID, same
// terminal, so Ctrl+C still ends the panel (criterion 4), and the socket
// handed down. It returns only when exec failed.
func restartUI(rr *restartRequest) error {
	env := append(restartEnv(), restartedEnv+"=1")
	if rr.file != nil {
		env = append(env, fmt.Sprintf("%s=%d", listenFDEnv, rr.file.Fd()))
	}
	return syscall.Exec(rr.path, os.Args, env)
}
