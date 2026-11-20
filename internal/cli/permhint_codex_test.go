package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
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

// An unwritable folder keeps the B-0052 message even inside Codex.
func TestCacheRefreshErrorUnwritableInsideCodexKeepsB0052(t *testing.T) {
	t.Setenv("CODEX_THREAD_ID", "019e-thread")
	repoDir := filepath.Join(t.TempDir(), "clone")
	err := cacheRefreshError("syncing the cache clone", repoDir, "x", &os.PathError{Op: "mkdir", Path: repoDir, Err: os.ErrPermission})
	if !strings.Contains(err.Error(), "does not accept writes") || strings.Contains(err.Error(), "--network") {
		t.Errorf("B-0052 message changed: %v", err)
	}
}
