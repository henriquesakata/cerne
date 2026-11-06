package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMain neutralizes CERNE_ACTOR inherited from the invoking environment
// (agent sessions carry CERNE_ACTOR=agent, stamped by `cerne hook install`,
// which would trip the F-0131 human gate on approve/accept): tests act as a
// human by default; gate tests opt back in per-case via t.Setenv.
//
// It also points the user-level config dir (the global model catalog, F-0311)
// at a throwaway dir: no test reads or writes the real profile. Likewise the
// Claude Code config dir (the transcripts cost reads, F-0309) and its
// CLAUDECODE marker: no test reads real transcripts or inherits the agent's
// harness; cost tests opt in per-case via t.Setenv.
func TestMain(m *testing.M) {
	os.Unsetenv("CERNE_ACTOR")
	dir, err := os.MkdirTemp("", "cerne-config-")
	if err != nil {
		panic(err)
	}
	os.Setenv("CERNE_CONFIG_DIR", dir)
	os.Unsetenv("CLAUDECODE")
	os.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(dir, "claude"))
	claudeRoots = func() []string { return strings.Split(os.Getenv("CLAUDE_CONFIG_DIR"), ",") }
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
