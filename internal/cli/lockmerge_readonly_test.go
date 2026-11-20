package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// lockGitDir makes .git unwritable (a sandbox that protects it): git and go-git
// both fail to rewrite .git/config inside it.
func lockGitDir(t *testing.T, projDir string) {
	t.Helper()
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("permission bits are not enforced here")
	}
	gitDir := filepath.Join(projDir, ".git")
	cfg := filepath.Join(gitDir, "config")
	if err := os.Chmod(cfg, 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(gitDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(gitDir, 0o755); os.Chmod(cfg, 0o644) })
}

// F-0474 crit. 5: the driver already registered with the expected values means
// no write, so a sandbox protecting .git produces no warning; without the
// driver it still tries (and fails) as before.
func TestEnsureLockMergeDriverSkipsWriteWhenRegistered(t *testing.T) {
	projDir, _, _ := setupGitProject(t)
	lockGitDir(t, projDir)
	if err := ensureLockMergeDriver(projDir); err != nil {
		t.Fatalf("already registered, want nil without a write: %v", err)
	}
}

func TestEnsureLockMergeDriverSkipsWriteWhenRegisteredViaGitBinary(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not available")
	}
	projDir, _, _ := setupGitProject(t)
	runGit(t, projDir, "config", "extensions.worktreeConfig", "true")
	lockGitDir(t, projDir)
	if err := ensureLockMergeDriver(projDir); err != nil {
		t.Fatalf("already registered (git binary path), want nil: %v", err)
	}
}

func TestEnsureLockMergeDriverStillWritesWhenMissing(t *testing.T) {
	projDir, _, _ := setupGitProject(t)
	runGit(t, projDir, "config", "--remove-section", "merge.cerne-lock")
	lockGitDir(t, projDir)
	if err := ensureLockMergeDriver(projDir); err == nil {
		t.Fatal("driver missing and .git unwritable: want the write error as before")
	}
}
