package cli

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// F-0512 crit. 5: inside Cursor (CURSOR_AGENT=1), a clone or sync failure on a
// writable cache folder points at the Cursor sandbox's network and at
// --network, never at deleting the cache; Codex keeps its own hint.
func TestCacheRefreshErrorInsideCursor(t *testing.T) {
	repoDir := filepath.Join(t.TempDir(), "clone")
	t.Setenv("CODEX_THREAD_ID", "")
	t.Setenv("CURSOR_AGENT", "1")
	causes := []error{
		errors.New("dial tcp github.com:22: connect: operation not permitted"),
		&net.OpError{Op: "dial", Net: "tcp", Err: os.NewSyscallError("connect", syscall.EPERM)},
	}
	for _, cause := range causes {
		err := cacheRefreshError("cloning the backend", repoDir, "git@github.com:o/r.git", cause)
		got := err.Error()
		for _, want := range []string{"cloning the backend at " + repoDir + " failed", "Cursor sandbox", "cerne hook install --provider cursor --network"} {
			if !strings.Contains(got, want) {
				t.Errorf("missing %q: %q", want, got)
			}
		}
		if strings.Contains(got, "delete that directory") || strings.Contains(got, "does not accept writes") {
			t.Errorf("wrong advice inside Cursor: %q", got)
		}
		if !errors.Is(err, cause) {
			t.Errorf("cause not wrapped: %q", got)
		}
	}

	// Review #4: a local backend has no host for --network to allow, so the
	// hint points at the folder install instead, and never at deleting.
	local := filepath.Join(t.TempDir(), "docs.git")
	got := cacheRefreshError("cloning the backend", repoDir, local, causes[0]).Error()
	if strings.Contains(got, "--network") || strings.Contains(got, "delete that directory") || !strings.Contains(got, "cerne hook install --provider cursor") {
		t.Errorf("local backend inside Cursor: %q", got)
	}

	// A Codex session keeps the Codex hint.
	t.Setenv("CODEX_THREAD_ID", "019e-thread")
	got = cacheRefreshError("cloning the backend", repoDir, "x", causes[0]).Error()
	if !strings.Contains(got, "--provider codex --network") || strings.Contains(got, "cursor") {
		t.Errorf("Codex hint changed: %q", got)
	}

	// An unwritable folder inside Cursor keeps the B-0052 message.
	if os.Geteuid() != 0 {
		t.Setenv("CODEX_THREAD_ID", "")
		ro := t.TempDir()
		if err := os.Chmod(ro, 0o555); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(ro, 0o755) })
		got = cacheRefreshError("cloning the backend", filepath.Join(ro, "clone"), "x", causes[1]).Error()
		if !strings.Contains(got, "does not accept writes") || strings.Contains(got, "--network") {
			t.Errorf("unwritable folder inside Cursor: %q", got)
		}
	}
}
