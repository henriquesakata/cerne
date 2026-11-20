package cli

import (
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/guide"
	"github.com/henriquesakata/cerne/internal/project"
)

func initWithCLIs(t *testing.T, clis ...string) string {
	t.Helper()
	old := project.FoundSubscriptionCLIs
	project.FoundSubscriptionCLIs = func() []string { return clis }
	t.Cleanup(func() { project.FoundSubscriptionCLIs = old })
	out, err := execInit(t, "--name", "cx", "--dir", gitRepoDir(t))
	if err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	return out
}

// F-0474 crit. 4: a detected agent CLI that init did not connect gets its
// Connect step before "Ask your agent".
func TestInitClosingConnectsDetectedAgentFirst(t *testing.T) {
	out := initWithCLIs(t, "claude", "codex")
	connect := "  2. Connect codex (found on your PATH): cerne hook install --provider codex && cerne skill install"
	ask := `  3. Ask your agent: "` + guide.FirstFeatureAsk + `"`
	ci, ai := strings.Index(out, connect), strings.Index(out, ask)
	if ci < 0 || ai < 0 || ci > ai {
		t.Fatalf("Connect step must come before Ask:\n%s", out)
	}
	if !strings.Contains(out, "  4. Approve the spec on the dashboard.") {
		t.Errorf("steps not renumbered:\n%s", out)
	}
}

func TestInitClosingConnectsSeveralAgentsOnOneLine(t *testing.T) {
	out := initWithCLIs(t, "cursor-agent", "gemini")
	want := "  2. Connect cursor, gemini (found on your PATH): cerne hook install --provider cursor,gemini && cerne skill install"
	if !strings.Contains(out, want) {
		t.Fatalf("missing %q:\n%s", want, out)
	}
}

// No agent CLI on PATH: the closing is the one init always printed.
func TestInitClosingWithoutAgentCLIsIsUnchanged(t *testing.T) {
	out := initWithCLIs(t, "claude")
	if strings.Contains(out, "Connect ") {
		t.Fatalf("unexpected Connect step:\n%s", out)
	}
	for _, want := range []string{"  2. Ask your agent: ", "  3. Approve the spec on the dashboard."} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
}
