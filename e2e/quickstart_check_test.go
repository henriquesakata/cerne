//go:build !windows

package e2e_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// F-0374: scripts/quickstart-check.sh runs the marked README blocks against a
// cerne binary. These tests use a fake binary; nothing is downloaded.

// fakeCerne logs every call to $FAKE_LOG. `ui` answers HTTP 200 on --port
// unless FAKE_UI=hang (never answers); `boom` always fails.
const fakeCerne = `#!/bin/sh
echo "$@" >>"$FAKE_LOG"
case "$1" in
  --version) echo "cerne v9.9.9" ;;
  boom) exit 1 ;;
  ui)
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
	cmd.Env = append(os.Environ(), append([]string{"FAKE_LOG=" + log, "QUICKSTART_UI_TIMEOUT=3"}, env...)...)
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
	if !strings.Contains(out, "quickstart: 'cerne boom' failed with cerne cerne v9.9.9") {
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
