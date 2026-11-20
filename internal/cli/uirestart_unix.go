//go:build unix

package cli

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"syscall"

	"golang.org/x/sys/unix"
)

// dupListener keeps the panel's socket open across exec: a copy of its
// descriptor without close-on-exec, so the port is never free while this
// process turns into the new binary (F-0478 criterion 3). The descriptor is
// read through SyscallConn, never os.File.Fd: Fd switches the open file
// description, shared with the listener still serving, to blocking mode, and
// a blocked Accept would stall the shutdown.
func dupListener(ln net.Listener) (*os.File, int, error) {
	tl, ok := ln.(*net.TCPListener)
	if !ok {
		return nil, -1, fmt.Errorf("a %T cannot be handed over", ln)
	}
	f, err := tl.File()
	if err != nil {
		return nil, -1, err
	}
	rc, err := f.SyscallConn()
	if err != nil {
		f.Close()
		return nil, -1, err
	}
	fd, ferr := -1, error(nil)
	if err := rc.Control(func(p uintptr) {
		fd = int(p)
		_, ferr = unix.FcntlInt(p, unix.F_SETFD, 0)
	}); err != nil || ferr != nil {
		f.Close()
		return nil, -1, fmt.Errorf("clear close-on-exec: %w", errors.Join(err, ferr))
	}
	return f, fd, nil
}

// keepForRetry hands the socket of a failed restart to the next serveUI: a
// fresh descriptor through the environment, the copy closed here so no two
// os.File values own the same number (the collector would close it twice).
func keepForRetry(rr *restartRequest) {
	if rr.file == nil {
		return
	}
	nfd, err := syscall.Dup(rr.fd)
	rr.file.Close()
	if err == nil {
		os.Setenv(listenFDEnv, strconv.Itoa(nfd))
	}
}

// restartUI replaces this process with the binary on disk: same PID, same
// terminal, so Ctrl+C still ends the panel (criterion 4), and the socket
// handed down. It returns only when exec failed.
func restartUI(rr *restartRequest) error {
	env := append(restartEnv(), restartedEnv+"=1")
	if rr.file != nil {
		env = append(env, fmt.Sprintf("%s=%d", listenFDEnv, rr.fd))
	}
	return syscall.Exec(rr.path, os.Args, env)
}
