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

// F-0484 crit. 4: inside Codex, a sync failure on a writable folder points to
// the sandbox, not to deleting the cache; outside Codex the message is today's.
func TestCacheRefreshErrorInsideCodex(t *testing.T) {
	repoDir := filepath.Join(t.TempDir(), "clone")
	cause := errors.New("dial git://127.0.0.1/r.git: Operation not permitted")
	const today = "syncing the cache clone at REPO failed (delete that directory and re-run the command to force a fresh clone): dial git://127.0.0.1/r.git: Operation not permitted"

	t.Setenv("CODEX_THREAD_ID", "")
	got := cacheRefreshError("syncing the cache clone", repoDir, "git://127.0.0.1/r.git", cause).Error()
	if got != strings.ReplaceAll(today, "REPO", repoDir) {
		t.Errorf("outside Codex = %q", got)
	}

	t.Setenv("CODEX_THREAD_ID", "019e-thread")
	err := cacheRefreshError("syncing the cache clone", repoDir, "git://127.0.0.1/r.git", cause)
	got = err.Error()
	if !strings.Contains(got, "syncing the cache clone at "+repoDir+" failed") || !strings.Contains(got, cause.Error()) {
		t.Errorf("lost the original message: %q", got)
	}
	for _, want := range []string{"Codex sandbox", "cerne hook install --provider codex --network"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q: %q", want, got)
		}
	}
	if strings.Contains(got, "delete that directory") {
		t.Errorf("must not advise deleting inside Codex: %q", got)
	}
	if !errors.Is(err, cause) {
		t.Errorf("cause not wrapped")
	}
}

// F-0484 critic #6: a cut network can surface as EPERM wrapped in a net error;
// inside Codex, on a writable folder, that is still the network hint.
func TestCacheRefreshErrorEPERMInsideCodexOnWritableFolder(t *testing.T) {
	repoDir := filepath.Join(t.TempDir(), "clone")
	cause := &net.OpError{Op: "dial", Net: "tcp", Err: os.NewSyscallError("connect", syscall.EPERM)}

	t.Setenv("CODEX_THREAD_ID", "019e-thread")
	got := cacheRefreshError("syncing the cache clone", repoDir, "git://127.0.0.1/r.git", cause).Error()
	if !strings.Contains(got, "cerne hook install --provider codex --network") || strings.Contains(got, "does not accept writes") {
		t.Errorf("inside Codex, writable folder: %q", got)
	}

	// Outside Codex: unchanged (the B-0052 message).
	t.Setenv("CODEX_THREAD_ID", "")
	got = cacheRefreshError("syncing the cache clone", repoDir, "x", cause).Error()
	if !strings.Contains(got, "does not accept writes") || strings.Contains(got, "--network") {
		t.Errorf("outside Codex changed: %q", got)
	}

	// Inside Codex on an unwritable folder: B-0052.
	if os.Geteuid() != 0 {
		ro := t.TempDir()
		if err := os.Chmod(ro, 0o555); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(ro, 0o755) })
		t.Setenv("CODEX_THREAD_ID", "019e-thread")
		got = cacheRefreshError("syncing the cache clone", filepath.Join(ro, "clone"), "x", cause).Error()
		if !strings.Contains(got, "does not accept writes") || strings.Contains(got, "--network") {
			t.Errorf("unwritable folder inside Codex: %q", got)
		}
	}
}

// An unwritable folder keeps the B-0052 message even inside Codex.
func TestCacheRefreshErrorUnwritableInsideCodexKeepsB0052(t *testing.T) {
	t.Setenv("CODEX_THREAD_ID", "019e-thread")
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	ro := t.TempDir()
	if err := os.Chmod(ro, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(ro, 0o755) })
	repoDir := filepath.Join(ro, "clone")
	err := cacheRefreshError("syncing the cache clone", repoDir, "x", &os.PathError{Op: "mkdir", Path: repoDir, Err: os.ErrPermission})
	if !strings.Contains(err.Error(), "does not accept writes") || strings.Contains(err.Error(), "--network") {
		t.Errorf("B-0052 message changed: %v", err)
	}
}
