package app

import (
	"os"
	"testing"
)

// TestMain keeps the subscription-CLI detection (F-0414) off: no test of this
// package may reach a real claude/codex/gemini on PATH. It also drops the
// agent-shell markers of Claude Code, Cursor and Copilot CLI (B-0030, F-0428,
// F-0444) inherited from the invoking environment: a suite run from such an
// agent would otherwise act as an agent wherever a test clears CERNE_ACTOR to
// play the human (N-0058); tests that need one set it per case.
func TestMain(m *testing.M) {
	os.Setenv("CERNE_NO_CLI_DETECT", "1")
	for _, marker := range []string{"CLAUDECODE", "CURSOR_AGENT", "COPILOT_CLI", "CODEX_THREAD_ID"} {
		os.Unsetenv(marker)
	}
	os.Unsetenv("CURSOR_CONVERSATION_ID") // F-0525: Cursor's conversation id must not leak into tests
	// And the host settings (a [llm] saved from the UI reaches every LoadConfig).
	dir, err := os.MkdirTemp("", "cerne-config-")
	if err != nil {
		panic(err)
	}
	os.Setenv("CERNE_CONFIG_DIR", dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
