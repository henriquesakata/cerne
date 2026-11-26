package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/app"
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
	// F-0478: a copy of this test binary stands in for the cerne on disk in
	// the restart test (uiHelperMain).
	if os.Getenv("CERNE_TEST_UI_HELPER") == "1" {
		os.Exit(uiHelperMain())
	}
	// F-0414: never probe PATH for a subscription CLI (children inherit this).
	os.Setenv("CERNE_NO_CLI_DETECT", "1")
	os.Unsetenv("CERNE_ACTOR")
	os.Unsetenv("CURSOR_AGENT") // Cursor's agent-shell marker counts as an agent too (F-0428)
	os.Unsetenv("COPILOT_CLI")  // and Copilot CLI's (F-0444); CLAUDECODE is dropped below (B-0030)
	os.Unsetenv("CODEX_THREAD_ID") // and Codex's (B-0051)
	// B-0084: room join/open store a credential keyed by CERNE_SESSION, and
	// room commands read it and the CERNE_ROOM_* variables; room tests opt in.
	os.Unsetenv("CERNE_SESSION")
	os.Unsetenv("CERNE_ROOM_SESSION")
	os.Unsetenv("CERNE_ROOM_FACILITATOR")
	dir, err := os.MkdirTemp("", "cerne-config-")
	if err != nil {
		panic(err)
	}
	os.Setenv("CERNE_CONFIG_DIR", dir)
	os.Unsetenv("CLAUDECODE")
	os.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(dir, "claude"))
	os.Setenv("CURSOR_CONFIG_DIR", filepath.Join(dir, "cursor")) // F-0513: nor Cursor's
	claudeRoots = func() []string { return strings.Split(os.Getenv("CLAUDE_CONFIG_DIR"), ",") }
	app.CostRoots = claudeRoots
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
