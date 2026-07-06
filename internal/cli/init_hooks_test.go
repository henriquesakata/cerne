package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestInitInstallsHooks (F-0139 crit. 5): a fresh `cerne init` ships the
// project with cerne's Claude Code hooks — the SessionStart registration
// reminder included — so a new project is born with the rules injected, no
// separate `cerne hook install` step.
func TestInitInstallsHooks(t *testing.T) {
	cacheRoot := t.TempDir()
	t.Setenv("CERNE_CACHE_DIR", cacheRoot)
	origin := newOrigin(t)
	projDir := t.TempDir()

	out, err := execInit(t, "--name", "Hooked", "--backend", origin, "--dir", projDir)
	if err != nil {
		t.Fatalf("init: %v\noutput: %s", err, out)
	}

	for _, script := range []string{"cerne-register-reminder.sh", "register-reference-reminder.sh"} {
		if _, err := os.Stat(filepath.Join(projDir, ".claude", "hooks", script)); err != nil {
			t.Errorf("init did not install %s: %v", script, err)
		}
	}
	raw, err := os.ReadFile(filepath.Join(projDir, ".claude", "settings.json"))
	if err != nil {
		t.Fatalf("settings.json missing after init: %v", err)
	}
	if !strings.Contains(string(raw), "cerne-register-reminder.sh") || !strings.Contains(string(raw), "SessionStart") {
		t.Fatalf("settings.json does not wire the SessionStart reminder:\n%s", raw)
	}
}
