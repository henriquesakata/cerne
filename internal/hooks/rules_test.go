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

// TestRegisterRulesPlanningClosesOnStart (F-0341): the pickup rule tells the
// agent the planning work closes by itself at `feature start`, in both
// session-start copies.
func TestRegisterRulesPlanningClosesOnStart(t *testing.T) {
	body, err := os.ReadFile("assets/cerne-register-reminder.sh")
	if err != nil {
		t.Fatalf("read asset: %v", err)
	}
	const want = "It closes by itself at `cerne feature start` (F-0341)."
	for name, text := range map[string]string{"RegisterRules": RegisterRules, "cerne-register-reminder.sh": string(body)} {
		if !strings.Contains(text, want) {
			t.Errorf("%s missing %q", name, want)
		}
	}
}

// TestRegisterRulesCompleteCard (F-0336 crit. 8): the session rule asks for
// the whole card in the registration act — spec, tags and capability — in
// both session-start copies, not the spec alone.
func TestRegisterRulesCompleteCard(t *testing.T) {
	body, err := os.ReadFile("assets/cerne-register-reminder.sh")
	if err != nil {
		t.Fatalf("read asset: %v", err)
	}
	for name, text := range map[string]string{"RegisterRules": RegisterRules, "cerne-register-reminder.sh": string(body)} {
		for _, want := range []string{"Register it complete in that same act", "--facet", "--component", "cerne feature capability <id> C-"} {
			if !strings.Contains(text, want) {
				t.Errorf("%s missing %q", name, want)
			}
		}
	}
}

// TestSessionRules_LightMode (F-0364, Ajustes 2 e 3): the dynamic session
// text drops the GAUNTLET item in light mode and keeps it otherwise; the
// static copies keep it with the light-mode escape; no copy cites the stray
// F-0545 id any more.
func TestSessionRules_LightMode(t *testing.T) {
	if s := SessionRules(false); strings.Contains(s, "GAUNTLET") || strings.Contains(s, "Barra:") || !strings.Contains(s, "HUMAN GATES") || !strings.Contains(s, "DUPLICATE GUARD") {
		t.Fatalf("light mode session rules:\n%s", s)
	}
	if s := SessionRules(true); !strings.Contains(s, "GAUNTLET (F-0270)") || strings.Contains(s, "skip it when") {
		t.Fatalf("gauntlet-on session rules:\n%s", s)
	}
	body, err := os.ReadFile("assets/cerne-register-reminder.sh")
	if err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{"RegisterRules": RegisterRules, "cerne-register-reminder.sh": string(body)} {
		if strings.Contains(text, "F-0545") || !strings.Contains(text, `(skip it when .cernerc sets [gates] gauntlet = "off")`) {
			t.Errorf("%s: static GAUNTLET item lacks the light-mode escape or cites F-0545", name)
		}
	}
}
