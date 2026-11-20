package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A value that lives only in the global git config must not skip the local
// write (the go-git branch reads only the local config).
func TestRegisterMergeDriverViaGitIgnoresGlobalConfig(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not available")
	}
	projDir, _, _ := setupGitProject(t)
	runGit(t, projDir, "config", "--remove-section", "merge.cerne-lock")
	global := filepath.Join(t.TempDir(), "gitconfig")
	body := "[merge \"cerne-lock\"]\n\tname = " + lockMergeName + "\n\tdriver = " + strings.ReplaceAll(lockMergeDriverCommand(), `"`, `\"`) + "\n"
	if err := os.WriteFile(global, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	if err := registerMergeDriverViaGit(projDir); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(runGit(t, projDir, "config", "--local", "--get", "merge.cerne-lock.driver")); got != lockMergeDriverCommand() {
		t.Fatalf("local driver = %q, want it written locally", got)
	}
}
