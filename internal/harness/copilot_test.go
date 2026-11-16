package harness

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/henriquesakata/cerne/internal/hooks"
	"github.com/henriquesakata/cerne/internal/skills"
)

var copilotItemNames = []string{"copilot: hooks file", "copilot: skill", "copilot: instructions", "copilot: mcp server"}

// TestCopilotItems (F-0444 crit. 5): with Copilot detected, the inventory lists
// its four install items — absent, divergent, ok — with a fix to run.
func TestCopilotItems(t *testing.T) {
	proj := t.TempDir()
	writeFile(t, filepath.Join(proj, ".github", "copilot-instructions.md"), "team\n")

	check := func(want string) {
		t.Helper()
		inv := Inspect(Options{ProjDir: proj})
		for _, n := range copilotItemNames {
			it, ok := findItem(inv, "providers", n)
			if !ok {
				t.Fatalf("item %q absent", n)
			}
			if it.State != want {
				t.Errorf("%s: state %q, want %q", n, it.State, want)
			}
			if want != StateOK && it.Fix == "" {
				t.Errorf("%s: no fix for a %s item", n, want)
			}
			if len(it.Consumers) != 1 || it.Consumers[0] != "copilot" {
				t.Errorf("%s: consumers %v", n, it.Consumers)
			}
		}
	}
	check(StateMissing)

	if _, err := hooks.Install(proj, hooks.ProviderCopilot); err != nil {
		t.Fatal(err)
	}
	if _, err := skills.Install(proj, hooks.ProviderCopilot); err != nil {
		t.Fatal(err)
	}
	check(StateOK)

	for _, p := range []string{".github/hooks/cerne.json", ".github/skills/cerne-authoring/SKILL.md", ".github/instructions/cerne.instructions.md", ".mcp.json"} {
		if err := os.WriteFile(filepath.Join(proj, filepath.FromSlash(p)), []byte(`{"mcpServers":{"cerne":{"command":"x"}}}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	check(StateDivergent)
}

// TestCopilotItemsAbsentWhenNotDetected: no Copilot files, no items.
func TestCopilotItemsAbsentWhenNotDetected(t *testing.T) {
	inv := Inspect(Options{ProjDir: t.TempDir()})
	if _, ok := findItem(inv, "providers", "copilot: hooks file"); ok {
		t.Fatal("copilot items listed for a project without Copilot files")
	}
}
