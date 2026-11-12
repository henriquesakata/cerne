//go:build !windows

package e2e_test

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// F-0374: scripts/quickstart-check.sh runs the marked README blocks against a
// cerne binary. These tests use a fake binary; nothing is downloaded.

// fakeCerne logs every call to $FAKE_LOG. `ui` answers HTTP 200 on --port
// unless FAKE_UI=hang (never answers); `boom` always fails.
const fakeCerne = `#!/bin/sh
echo "$@" >>"$FAKE_LOG"
case "$1" in
  --version) [ "${FAKE_VERSION_EMPTY:-}" = 1 ] || echo "cerne v9.9.9" ;;
  pwd) pwd >>"$FAKE_LOG" ;;
  boom) exit 1 ;;
  ui)
    echo $$ >"$FAKE_PIDFILE"
    [ "${FAKE_UI:-}" = hang ] && exec sleep 600
    port=$4
    exec python3 -c "
import http.server,sys
class H(http.server.BaseHTTPRequestHandler):
    def do_GET(s):
        s.send_response(200); s.end_headers(); s.wfile.write(b'[]')
    def log_message(*a): pass
http.server.HTTPServer(('127.0.0.1',$port),H).serve_forever()"
    ;;
esac
`

func runQuickstartCheck(t *testing.T, readme string, env ...string) (out string, calls string, err error) {
	t.Helper()
	if _, perr := exec.LookPath("python3"); perr != nil {
		t.Skip("python3 not available for the fake cerne ui")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "cerne")
	if werr := os.WriteFile(bin, []byte(fakeCerne), 0o755); werr != nil {
		t.Fatal(werr)
	}
	md := filepath.Join(dir, "README.md")
	if werr := os.WriteFile(md, []byte(readme), 0o644); werr != nil {
		t.Fatal(werr)
	}
	log := filepath.Join(dir, "calls.log")
	cmd := exec.Command("sh", "../scripts/quickstart-check.sh", md, bin)
	cmd.Env = append(os.Environ(), append([]string{"FAKE_LOG=" + log, "FAKE_PIDFILE=" + filepath.Join(dir, "ui.pid"), "QUICKSTART_UI_TIMEOUT=3"}, env...)...)
	b, err := cmd.CombinedOutput()
	c, _ := os.ReadFile(log)
	return string(b), string(c), err
}

func TestQuickstartCheckRunsOnlyMarkedBlocks(t *testing.T) {
	readme := "# x\n\n```bash\ncerne unmarked\n```\n\n<!-- quickstart -->\n```bash\n# a comment\ncerne init --name p\n```\n\ntext\n\n<!-- quickstart -->\n\n```bash\ncerne blank-line-between\n```\n"
	out, calls, err := runQuickstartCheck(t, readme)
	if err != nil {
		t.Fatalf("want success: %v\n%s", err, out)
	}
	if !strings.Contains(calls, "init --name p") {
		t.Errorf("marked block did not run: %q", calls)
	}
	if strings.Contains(calls, "unmarked") || strings.Contains(calls, "blank-line-between") {
		t.Errorf("a block without the mark directly before it ran: %q", calls)
	}
}

func TestQuickstartCheckNamesFailingCommandAndVersion(t *testing.T) {
	readme := "<!-- quickstart -->\n```bash\ncerne init --name p\ncerne boom\ncerne never-reached\n```\n"
	out, calls, err := runQuickstartCheck(t, readme)
	if err == nil {
		t.Fatalf("want failure\n%s", out)
	}
	if !strings.Contains(out, "quickstart: 'cerne boom' failed with cerne v9.9.9") {
		t.Errorf("failure does not name command and version:\n%s", out)
	}
	if strings.Contains(calls, "never-reached") {
		t.Errorf("ran past the failure: %q", calls)
	}
}

func TestQuickstartCheckUIIsProbedAndStopped(t *testing.T) {
	readme := "<!-- quickstart -->\n```bash\ncerne ui        # your dashboard\ncerne init --name after\n```\n"
	out, calls, err := runQuickstartCheck(t, readme)
	if err != nil {
		t.Fatalf("want success: %v\n%s", err, out)
	}
	if !strings.Contains(calls, "ui --no-browser --port ") || !strings.Contains(calls, "init --name after") {
		t.Errorf("ui not started in background or later line skipped: %q", calls)
	}
}

func TestQuickstartCheckUIThatNeverAnswersFailsInsteadOfHanging(t *testing.T) {
	readme := "<!-- quickstart -->\n```bash\ncerne ui\n```\n"
	out, _, err := runQuickstartCheck(t, readme, "FAKE_UI=hang")
	if err == nil || !strings.Contains(out, "quickstart: 'cerne ui' failed with cerne") {
		t.Fatalf("want a named failure, got err=%v\n%s", err, out)
	}
}

func TestQuickstartCheckNoMarkedBlockIsAnError(t *testing.T) {
	_, _, err := runQuickstartCheck(t, "```bash\ncerne init\n```\n")
	if err == nil {
		t.Fatal("a file with no marked block must not pass silently")
	}
}

func TestQuickstartCheckCRLFMarkdown(t *testing.T) {
	readme := "<!-- quickstart -->\r\n```bash\r\ncerne init --name p\r\n```\r\n"
	out, calls, err := runQuickstartCheck(t, readme)
	if err != nil {
		t.Fatalf("CRLF fixture must run: %v\n%s", err, out)
	}
	if strings.Contains(calls, "\r") || !strings.Contains(calls, "init --name p") {
		t.Errorf("a carriage return reached the command: %q", calls)
	}
}

func TestQuickstartCheckNonBashFenceAfterMarkerIsNotRun(t *testing.T) {
	readme := "<!-- quickstart -->\n```sh\ncerne shfence\n```\n\n<!-- quickstart -->\n```bash\ncerne init --name p\n```\n"
	_, calls, err := runQuickstartCheck(t, readme)
	if err != nil || strings.Contains(calls, "shfence") {
		t.Fatalf("a non-bash fence ran: err=%v calls=%q", err, calls)
	}
}

func TestQuickstartCheckBlocksDoNotShareState(t *testing.T) {
	readme := "<!-- quickstart -->\n```bash\ncd /\n```\n\n<!-- quickstart -->\n```bash\ncerne pwd\n```\n"
	_, calls, err := runQuickstartCheck(t, readme)
	if err != nil {
		t.Fatalf("err=%v calls=%q", err, calls)
	}
	for _, l := range strings.Split(calls, "\n") {
		if l == "/" {
			t.Fatalf("cd leaked across blocks: %q", calls)
		}
	}
}

func TestQuickstartCheckRefusesSudoAndPipeToShell(t *testing.T) {
	for _, line := range []string{"sudo cerne init", "curl -fsSL https://example.invalid/x | sh", "curl x |bash"} {
		out, calls, err := runQuickstartCheck(t, "<!-- quickstart -->\n```bash\n"+line+"\n```\n")
		if err == nil || !strings.Contains(out, "refusing") || strings.Contains(calls, "init") {
			t.Errorf("%q must be refused: err=%v out=%s calls=%q", line, err, out, calls)
		}
	}
}

func TestQuickstartCheckStopsTheUIServer(t *testing.T) {
	dir := t.TempDir()
	pidfile := filepath.Join(dir, "ui.pid")
	out, _, err := runQuickstartCheck(t, "<!-- quickstart -->\n```bash\ncerne ui\n```\n", "FAKE_PIDFILE="+pidfile)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	b, rerr := os.ReadFile(pidfile)
	if rerr != nil {
		t.Fatalf("fake ui never recorded its pid: %v", rerr)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	if pid <= 0 || syscall.Kill(pid, 0) == nil {
		t.Errorf("fake ui server (pid %d) is still alive after the check", pid)
	}
}

func TestQuickstartCheckBusyPortFailsNamed(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, aerr := ln.Accept()
			if aerr != nil {
				return
			}
			c.Close()
		}
	}()
	port := strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
	out, calls, rerr := runQuickstartCheck(t, "<!-- quickstart -->\n```bash\ncerne ui\n```\n", "QUICKSTART_UI_PORT="+port)
	if rerr == nil || !strings.Contains(out, "no free port") || strings.Contains(calls, "ui --no-browser") {
		t.Fatalf("a busy port must fail without starting ui: err=%v out=%s calls=%q", rerr, out, calls)
	}
}

func TestQuickstartCheckUnknownVersionAndReleaseTag(t *testing.T) {
	out, _, err := runQuickstartCheck(t, "<!-- quickstart -->\n```bash\ncerne boom\n```\n", "FAKE_VERSION_EMPTY=1", "QUICKSTART_TAG=v1.2.3")
	if err == nil || !strings.Contains(out, "failed with cerne unknown (release v1.2.3)") {
		t.Fatalf("want unknown version and tag in the failure line: %v\n%s", err, out)
	}
}
