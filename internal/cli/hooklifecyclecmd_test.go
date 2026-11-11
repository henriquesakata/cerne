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
	// Open clarifications are the human's (F-0175); every other impediment,
	// the soft card ones included (F-0336), is the agent's to fix.
	if !strings.Contains(out, "fix every impediment it lists except open clarifications, which you leave for the human with the approve request") ||
		strings.Contains(out, "until it reports approvable") || strings.Contains(out, "fix the hard impediments") {
		t.Fatalf("reminder does not leave only open clarifications to the human:\n%s", out)
	}
	// F-0336 crit. 8: the whole card in the same turn — spec, tags, capability —
	// each with its command, before the check.
	tags, capab, check := strings.Index(out, "cerne feature retag F-0042"), strings.Index(out, "cerne feature capability F-0042 C-"), strings.Index(out, "--check")
	if !strings.Contains(out, "Complete the card NOW") || tags < 0 || capab < 0 || tags > check || capab > check {
		t.Fatalf("reminder does not ask for spec, tags and capability before the check:\n%s", out)
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

// F-0312 C13: a promote mints a stub candidate, so it gets the same
// write-the-spec-now reminder as feature new — and a refused promote, whose
// message cites the existing F-, stays silent.
func TestHookLifecycleReminderOnIdeaPromote(t *testing.T) {
	out, err := execCerneStdin(t, hookJSON(t, "sess-lc", "/tmp/proj", "Bash",
		map[string]any{"command": "cerne idea promote I-0003"},
		map[string]any{"stdout": "I-0003: active -> promoted (F-0042)\nNext step: complete the spec of F-0042."}),
		"hook", "lifecycle-reminder")
	if err != nil {
		t.Fatalf("hook lifecycle-reminder: %v\noutput: %s", err, out)
	}
	for _, want := range []string{"additionalContext", "F-0042", "same turn", "cerne feature approve F-0042 --check", "STOP"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
	out, _ = execCerneStdin(t, hookJSON(t, "sess-lc", "/tmp/proj", "Bash",
		map[string]any{"command": "cerne idea promote I-0003"},
		map[string]any{"stdout": "Error: the idea is not active: I-0003 is promoted (promoted to F-0042)"}),
		"hook", "lifecycle-reminder")
	if strings.TrimSpace(out) != "" {
		t.Fatalf("a refused promote produced a reminder:\n%s", out)
	}
}
