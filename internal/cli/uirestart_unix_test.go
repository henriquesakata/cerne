//go:build unix

package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

type uiProc struct {
	cmd  *exec.Cmd
	out  *lockedBuffer
	done chan error
}

// startUIHelper runs `<cmdPath> ui` for the project in dir, as a real
// process of the test binary (uiHelperMain).
func startUIHelper(t *testing.T, cmdPath string, port int, dir string) *uiProc {
	t.Helper()
	p := &uiProc{out: &lockedBuffer{}, done: make(chan error, 1)}
	p.cmd = exec.Command(cmdPath, "ui", "--port", strconv.Itoa(port), "--no-browser", "--pull-interval", "0")
	p.cmd.Dir = dir
	p.cmd.Env = append(os.Environ(), "CERNE_TEST_UI_HELPER=1")
	p.cmd.Stdout, p.cmd.Stderr = p.out, p.out
	if err := p.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { p.done <- p.cmd.Wait() }()
	t.Cleanup(func() {
		_ = p.cmd.Process.Signal(os.Interrupt)
		select {
		case <-p.done:
		case <-time.After(10 * time.Second):
			_ = p.cmd.Process.Kill()
		}
		if t.Failed() {
			t.Logf("output of %s:\n%s", dir, p.out.String())
		}
	})
	return p
}

func waitHostVersion(t *testing.T, base, want string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last string
	for time.Now().Before(deadline) {
		if r, err := probeClient.Get(base + "/api/projects"); err == nil {
			var b struct {
				Version string `json:"version"`
			}
			_ = json.NewDecoder(r.Body).Decode(&b)
			r.Body.Close()
			if last = b.Version; last == want {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("panel version = %q, want %q", last, want)
}

// TestUIRestartKeepsPortAndProjects (F-0478 criteria 3 and 4): a host and an
// attached process; the binary on disk is replaced and the restart asked.
// The port answers with the new version and both projects, the host keeps
// its PID (exec, not a new process), no one else ever binds the port in the
// meantime, and Ctrl+C still ends the restarted host cleanly.
func TestUIRestartKeepsPortAndProjects(t *testing.T) {
	cmdPath := filepath.Join(t.TempDir(), "cerne")
	installUIHelper(t, cmdPath, "v0.0.1-a", time.Now().Add(-time.Hour))
	dirs := superviseDirs(t, "proj-a", "proj-b")
	port := freePort(t)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	base := "http://" + addr

	host := startUIHelper(t, cmdPath, port, dirs[0])
	waitPIDs(t, base, 15*time.Second, "proj-a")
	startUIHelper(t, cmdPath, port, dirs[1])
	waitPIDs(t, base, 15*time.Second, "proj-a", "proj-b")

	var stolen atomic.Int32
	stop := make(chan struct{})
	stealerDone := make(chan struct{})
	go func() {
		defer close(stealerDone)
		for {
			select {
			case <-stop:
				return
			default:
			}
			if ln, err := net.Listen("tcp", addr); err == nil {
				stolen.Add(1)
				ln.Close()
			}
			time.Sleep(2 * time.Millisecond)
		}
	}()

	installUIHelper(t, cmdPath, "v0.0.2-b", time.Now())
	r, err := http.Post(base+"/api/restart", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != http.StatusAccepted {
		t.Fatalf("restart: %d, want 202", r.StatusCode)
	}
	waitHostVersion(t, base, "v0.0.2-b", 15*time.Second)
	waitPIDs(t, base, 15*time.Second, "proj-a", "proj-b")
	close(stop)
	<-stealerDone
	if n := stolen.Load(); n > 0 {
		t.Fatalf("another process bound the port %d time(s) during the restart", n)
	}
	select {
	case err := <-host.done:
		t.Fatalf("the host process exited (%v): the restart did not keep its PID", err)
	default:
	}

	if err := host.cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-host.done:
		if err != nil {
			t.Fatalf("restarted host left with %v on Ctrl+C, want exit 0", err)
		}
		host.done <- nil // for the cleanup
	case <-time.After(10 * time.Second):
		t.Fatal("the restarted host did not exit on Ctrl+C")
	}
}

// B-0067: a panel stuck before it serves (in the field, the wait for a backend
// lock) has bound the port, and signal.NotifyContext swallows Ctrl+C and
// SIGTERM while nothing watches the context. A second signal must end the
// process, so nobody needs kill -9 to free the port.
func TestUISecondSignalEndsStuckStart(t *testing.T) {
	t.Setenv("CERNE_TEST_UI_STUCK", "1")
	cmdPath := filepath.Join(t.TempDir(), "cerne")
	installUIHelper(t, cmdPath, "v0.0.1-a", time.Now())
	port := freePort(t)
	p := startUIHelper(t, cmdPath, port, superviseDirs(t, "proj-a")[0])
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	for deadline := time.Now().Add(15 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if c, err := net.Dial("tcp", addr); err == nil {
			c.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the stuck panel never bound the port")
		}
	}
	for range 2 {
		if err := p.cmd.Process.Signal(syscall.SIGTERM); err != nil {
			t.Fatal(err)
		}
		time.Sleep(300 * time.Millisecond) // let the first one be handled
	}
	select {
	case <-p.done:
		p.done <- nil // for the cleanup
	case <-time.After(5 * time.Second):
		t.Fatal("a second SIGTERM did not end a panel stuck before serving")
	}
}

// Critic 4 (F-0478): handing the socket over must leave the listener that is
// still serving in non-blocking mode (os.File.Fd would switch the shared open
// file description to blocking, and a blocked Accept stalls the shutdown),
// and the copy must survive exec (no close-on-exec).
func TestDupListenerKeepsListenerNonBlocking(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	f, fd, err := dupListener(ln)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rc, err := ln.(*net.TCPListener).SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var flags int
	if err := rc.Control(func(p uintptr) { flags, _ = unix.FcntlInt(p, unix.F_GETFL, 0) }); err != nil {
		t.Fatal(err)
	}
	if flags&unix.O_NONBLOCK == 0 {
		t.Fatal("the serving listener became blocking")
	}
	fdFlags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0)
	if err != nil {
		t.Fatal(err)
	}
	if fdFlags&unix.FD_CLOEXEC != 0 {
		t.Fatal("the handed-down copy is close-on-exec")
	}
}
