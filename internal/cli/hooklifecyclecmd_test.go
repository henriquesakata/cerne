package cli

import (
	"strings"
	"testing"
)

// TestHookLifecycleReminderOnFeatureNew (F-0170 crit. 1): a successful
// `cerne feature new` seen by the PostToolUse hook emits additionalContext
// telling the agent to write the real spec in this same turn and stop at the
// human approve gate.
func TestHookLifecycleReminderOnFeatureNew(t *testing.T) {
	out, err := execCerneStdin(t, hookJSON(t, "sess-lc", "/tmp/proj", "Bash",
		map[string]any{"command": `cerne feature new "Nova coisa" --context "why"`},
		map[string]any{"stdout": "F-0042 created (candidate) with its context seeded into the spec."}),
		"hook", "lifecycle-reminder")
	if err != nil {
		t.Fatalf("hook lifecycle-reminder: %v\noutput: %s", err, out)
	}
	for _, want := range []string{"additionalContext", "PostToolUse", "F-0042", "spec", "same turn", "approve", "STOP"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
	// F-0308 crit. 6: the rehearsal comes before asking a human for the approve.
	check, ask := strings.Index(out, "cerne feature approve F-0042 --check"), strings.Index(out, "ask a human")
	if check < 0 || ask < 0 || check > ask {
		t.Fatalf("reminder does not send the agent to --check before asking a human:\n%s", out)
	}
}

// TestHookLifecycleReminderOnFeatureStart (F-0170 crit. 2): a confirmed
// `cerne feature start` emits additionalContext pointing at the active notes
// (conventions) and real-time progress discipline.
func TestHookLifecycleReminderOnFeatureStart(t *testing.T) {
	out, err := execCerneStdin(t, hookJSON(t, "sess-lc", "/tmp/proj", "Bash",
		map[string]any{"command": "cerne feature start F-0042"},
		map[string]any{"stdout": "F-0042: approved → in_development"}),
		"hook", "lifecycle-reminder")
	if err != nil {
		t.Fatalf("hook lifecycle-reminder: %v\noutput: %s", err, out)
	}
	for _, want := range []string{"F-0042", "note list", "plan", "result", "cerne sync"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
}

// TestHookLifecycleReminderSilence (F-0170 crit. 3/6): refused or unrelated
// commands — including a duplicate-guard refusal whose message cites F- ids of
// the similar features — and malformed payloads produce no output and no error.
func TestHookLifecycleReminderSilence(t *testing.T) {
	for name, stdin := range map[string]string{
		"guard refusal citing ids": hookJSON(t, "s", "/tmp/proj", "Bash",
			map[string]any{"command": `cerne feature new "Dup" --context "x"`},
			map[string]any{"stdout": "Error: a similar live feature exists: F-0007 — join it instead"}),
		"start refused (no transition)": hookJSON(t, "s", "/tmp/proj", "Bash",
			map[string]any{"command": "cerne feature start F-0042"},
			map[string]any{"stdout": "Error: plan is still the scaffold stub"}),
		"unrelated command": hookJSON(t, "s", "/tmp/proj", "Bash",
			map[string]any{"command": "go test ./..."},
			map[string]any{"stdout": "F-0001 created (candidate)"}),
		"not json": "garbage{",
	} {
		out, err := execCerneStdin(t, stdin, "hook", "lifecycle-reminder")
		if err != nil {
			t.Fatalf("%s: errored: %v\noutput: %s", name, err, out)
		}
		if strings.TrimSpace(out) != "" {
			t.Fatalf("%s: want silence, got:\n%s", name, out)
		}
	}
}
