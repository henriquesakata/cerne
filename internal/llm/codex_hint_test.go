package llm

import (
	"strings"
	"testing"
)

// F-0484 crit. 4: inside Codex a failed AI CLI keeps its message and gains the
// sandbox hint; outside Codex the message is today's, byte for byte.
func TestDescribeExitInsideCodexAppendsSandboxHint(t *testing.T) {
	const today = `"claude" exited with exit status 1 (stdout): Not logged in`

	t.Setenv("CODEX_THREAD_ID", "")
	if got := describeExit("claude", "exit status 1", []byte("Not logged in"), nil); got != today {
		t.Errorf("outside Codex = %q, want %q", got, today)
	}

	t.Setenv("CODEX_THREAD_ID", "019e-thread")
	got := describeExit("claude", "exit status 1", []byte("Not logged in"), nil)
	if !strings.HasPrefix(got, today) {
		t.Errorf("inside Codex lost the original message: %q", got)
	}
	for _, want := range []string{"Codex sandbox", "cerne hook install --provider codex --network"} {
		if !strings.Contains(got, want) {
			t.Errorf("inside Codex = %q, missing %q", got, want)
		}
	}
}
