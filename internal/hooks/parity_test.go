package hooks

import (
	"os"
	"strings"
	"testing"
)

// TestParityMatrixGolden (F-0174 crit. 6): the committed doc is exactly what
// the in-code table renders — the published matrix cannot drift from the
// installer. Regenerate with: go test ./internal/hooks/ -run Golden -update
// (or copy ParityMatrix() output into docs/provider-parity.md).
func TestParityMatrixGolden(t *testing.T) {
	raw, err := os.ReadFile("../../docs/provider-parity.md")
	if err != nil {
		t.Fatalf("golden missing (generate it from ParityMatrix()): %v", err)
	}
	// autocrlf materializes the file with CRLF on Windows checkouts (F-0185);
	// only EOL is normalized — any content byte still fails the compare.
	want := strings.ReplaceAll(string(raw), "\r\n", "\n")
	if got := ParityMatrix(); got != want {
		t.Fatalf("docs/provider-parity.md drifted from the code table — regenerate it from ParityMatrix().\n--- got ---\n%s", got)
	}
}

// TestParityMatrixCoversProviders: every provider column and the degraded
// marker are present — a row cannot silently omit a provider.
func TestParityMatrixCoversProviders(t *testing.T) {
	m := ParityMatrix()
	for _, want := range []string{"Claude Code", "Cursor", "Codex CLI", "Gemini CLI", "degraded"} {
		if !strings.Contains(m, want) {
			t.Fatalf("matrix missing %q", want)
		}
	}
}

// TestParityCopilotColumn (F-0444 crit. 4): every row states the real Copilot
// state, the degraded ones carry their reason, and the guide condition exists.
func TestParityCopilotColumn(t *testing.T) {
	deg := 0
	for _, r := range ParityRows() {
		if r.For(ProviderCopilot) == "" {
			t.Errorf("row %q has no Copilot cell", r.Capability)
		}
		if d, reason := r.Degraded(ProviderCopilot); d {
			deg++
			if reason == "" {
				t.Errorf("row %q: degraded without a reason", r.Capability)
			}
		}
	}
	if deg == 0 {
		t.Error("expected at least one degraded Copilot row (compaction handoff)")
	}
	if GuideCondition(ProviderCopilot) == "" {
		t.Error("no guide condition for copilot")
	}
	if !strings.Contains(ParityMatrix(), "GitHub Copilot") {
		t.Error("matrix lacks the Copilot column")
	}
}

// TestParityCopilotStatesTheProbedLimits (F-0444): the matrix carries the
// double execution, the unverified Stop block and the CLI-only actor marker.
func TestParityCopilotStatesTheProbedLimits(t *testing.T) {
	byCap := map[string]ParityRow{}
	for _, r := range ParityRows() {
		byCap[r.Capability] = r
	}
	if d, reason := byCap["Double execution with the .claude hooks"].Degraded(ProviderCopilot); !d || !strings.Contains(reason, "side effects") {
		t.Errorf("double execution row: degraded=%v reason=%q", d, reason)
	}
	if d, reason := byCap["Stop closeout"].Degraded(ProviderCopilot); !d || !strings.Contains(reason, "never exercised") {
		t.Errorf("stop row: degraded=%v reason=%q", d, reason)
	}
	actor := byCap["CERNE_ACTOR=agent (human gates)"].For(ProviderCopilot)
	for _, want := range []string{"Copilot CLI 1.0.112", "VS Code", "cloud agent", "unverified"} {
		if !strings.Contains(actor, want) {
			t.Errorf("actor cell lacks %q: %s", want, actor)
		}
	}
}

// TestParityWakeRow (F-0491 crit. 7): the matrix says which harness wakes an
// idle session — Claude Code, with the bypass caveat — and every other one is
// degraded with its reason; the Codex channel and prompt-notes rows include
// the prompt hook.
func TestParityWakeRow(t *testing.T) {
	var wake, channel, notes ParityRow
	for _, r := range ParityRows() {
		switch r.Capability {
		case "Wake an idle session (F-0491)":
			wake = r
		case "Project channel mid-session (F-0325, B-0054)":
			channel = r
		case "Notes matching the prompt (B-0020)":
			notes = r
		}
	}
	// The Codex prompt hook carries the prompt notes too (a deliberate side
	// effect): that row must not still say it is not wired.
	if d, _ := notes.Degraded(ProviderCodex); d || !strings.Contains(notes.Codex, "UserPromptSubmit") {
		t.Errorf("Codex prompt-notes cell = %q, want the wired UserPromptSubmit hook", notes.Codex)
	}
	if wake.Capability == "" {
		t.Fatal("no wake row in the parity matrix")
	}
	if d, _ := wake.Degraded(ProviderClaude); d || !strings.Contains(wake.Claude, "bypassPermissions") || !strings.Contains(wake.Claude, "next contact point") {
		t.Errorf("Claude Code cell = %q, want the inbox wake with the bypass caveat", wake.Claude)
	}
	for _, p := range []Provider{ProviderCursor, ProviderCodex, ProviderGemini, ProviderCopilot} {
		if d, reason := wake.Degraded(p); !d || reason == "" {
			t.Errorf("%s cell = %q, want degraded with a reason", p, wake.For(p))
		}
	}
	if !strings.Contains(channel.Codex, "UserPromptSubmit") {
		t.Errorf("Codex channel cell = %q, want the prompt hook", channel.Codex)
	}
}

// F-0512 crit. 6: the matrix states the Cursor sandbox file, what --network
// does there, and the Cursor MCP file — none of them degraded.
func TestParityCursorSandboxNetworkAndMCP(t *testing.T) {
	want := map[string][]string{
		"sandbox": {".cursor/sandbox.json", "additionalReadwritePaths", "~/.cursor/sandbox.json"},
		"network": {"--network", "networkPolicy.allow", "deny"},
		"mcp":     {".cursor/mcp.json", "cerne mcp"},
	}
	found := map[string]bool{}
	for _, r := range ParityRows() {
		c := strings.ToLower(r.Capability)
		for key, needles := range want {
			if !strings.Contains(c, key) {
				continue
			}
			found[key] = true
			if d, _ := r.Degraded(ProviderCursor); d {
				t.Errorf("row %q: Cursor cell degraded", r.Capability)
			}
			for _, n := range needles {
				if !strings.Contains(r.Cursor, n) {
					t.Errorf("row %q: Cursor cell lacks %q: %s", r.Capability, n, r.Cursor)
				}
			}
		}
	}
	for key := range want {
		if !found[key] {
			t.Errorf("no %s row in the parity matrix", key)
		}
	}
}

// F-0512 crit. 6 (review #6): a provider whose install writes no sandbox or
// MCP file gets n/a in those rows, so cerne harness reports no "missing" gap
// whose fix would write nothing.
func TestParitySandboxAndMCPRowsAreNotApplicableWhereNothingIsWritten(t *testing.T) {
	na := map[string][]Provider{
		"Agent sandbox":    {ProviderClaude, ProviderGemini, ProviderCopilot},
		"Network for pull": {ProviderClaude, ProviderGemini, ProviderCopilot},
		"Read-only MCP":    {ProviderClaude, ProviderCodex, ProviderGemini},
	}
	for _, r := range ParityRows() {
		for prefix, ps := range na {
			if !strings.HasPrefix(r.Capability, prefix) {
				continue
			}
			for _, p := range ps {
				if r.For(p) != NotApplicable {
					t.Errorf("row %q, %s = %q, want %q", r.Capability, p, r.For(p), NotApplicable)
				}
			}
		}
	}
}
