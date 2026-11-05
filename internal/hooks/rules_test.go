package hooks

import (
	"os"
	"strings"
	"testing"
)

// TestRegisterRulesIncludesPickupInstruction (F-0208 crit. 3): the session
// reminder every agent reads at boot instructs opening the pickup work before
// writing the plan, not just at `feature start`.
func TestRegisterRulesIncludesPickupInstruction(t *testing.T) {
	for _, want := range []string{
		"cerne work start",
		"planejamento",
		"--ref F-NNNN",
		"F-0208",
	} {
		if !strings.Contains(RegisterRules, want) {
			t.Fatalf("RegisterRules missing %q:\n%s", want, RegisterRules)
		}
	}
}

// TestRegisterReminderScriptMatchesGoRules: the shell asset installed into
// projects (cerne-register-reminder.sh) carries the same pickup instruction
// as the Go copy — both are read at session start, one per provider surface.
func TestRegisterReminderScriptMatchesGoRules(t *testing.T) {
	body, err := os.ReadFile("assets/cerne-register-reminder.sh")
	if err != nil {
		t.Fatalf("read asset: %v", err)
	}
	for _, want := range []string{
		"cerne work start",
		"planejamento",
		"--ref F-NNNN",
		"F-0208",
	} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("cerne-register-reminder.sh missing %q", want)
		}
	}
}

// TestRegisterRulesBarIsAProposal (F-0308 crit. 10): the GAUNTLET rule says the
// agent's Barra: line is a proposal, the human approve is its acceptance, and
// the check comes before the approve request — in both session-start copies.
func TestRegisterRulesBarIsAProposal(t *testing.T) {
	body, err := os.ReadFile("assets/cerne-register-reminder.sh")
	if err != nil {
		t.Fatalf("read asset: %v", err)
	}
	for name, text := range map[string]string{"RegisterRules": RegisterRules, "cerne-register-reminder.sh": string(body)} {
		for _, want := range []string{"is a proposal", "the human approve is its acceptance", "cerne feature approve <id> --check"} {
			if !strings.Contains(text, want) {
				t.Errorf("%s missing %q", name, want)
			}
		}
	}
}
