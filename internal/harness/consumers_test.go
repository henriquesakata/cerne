package harness

import (
	"path/filepath"
	"reflect"
	"testing"
)

// TestConsumersFor: deterministic attribution per class/origin (F-0259
// criteria 1-2) — which AI providers can consume each inventory item.
func TestConsumersFor(t *testing.T) {
	all := []string{"claude", "cursor", "codex", "gemini", "copilot"}
	tests := []struct {
		name string
		item Item
		want []string
	}{
		{"cerne class is provider-agnostic", Item{Class: "cerne", Name: "docs.lock"}, all},
		{"path binary serves any agent", Item{Class: "hooks", Name: "rtk", Origin: OriginPath}, all},
		{"project hook is a claude surface", Item{Class: "hooks", Name: "cerne-work-gate.sh", Origin: OriginProject}, []string{"claude"}},
		{"skill is a claude surface", Item{Class: "skills", Name: "cerne-authoring", Origin: OriginProject}, []string{"claude"}},
		{"settings layer is a claude surface", Item{Class: "config", Name: "settings.json", Origin: OriginProject}, []string{"claude"}},
		{"marketplace plugin is claude-only", Item{Class: "plugins", Name: "caveman", Origin: OriginPlugin}, []string{"claude"}},
		{"project .mcp.json is claude's when the project has no Copilot", Item{Class: "mcp", Name: "context-mode", Origin: OriginProject}, []string{"claude"}},
		{"user mcp declaration is claude's", Item{Class: "mcp", Name: "context-mode", Origin: OriginUser}, []string{"claude"}},
		{"CLAUDE.md feeds claude", Item{Class: "context", Name: "CLAUDE.md"}, []string{"claude"}},
		{"user CLAUDE.md feeds claude", Item{Class: "context", Name: "~/.claude/CLAUDE.md"}, []string{"claude"}},
		{"AGENTS.md feeds codex, cursor and copilot", Item{Class: "context", Name: "AGENTS.md"}, []string{"codex", "cursor", "copilot"}},
		{"parity row belongs to its provider", Item{Class: "providers", Name: "cursor: Stop closeout"}, []string{"cursor"}},
		{"copilot parity row", Item{Class: "providers", Name: "copilot: Work gate"}, []string{"copilot"}},
		{"gemini parity row", Item{Class: "providers", Name: "gemini: Trust prerequisite"}, []string{"gemini"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := consumersFor(tt.item, false); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("consumersFor(%+v) = %v, want %v", tt.item, got, tt.want)
			}
		})
	}
}

// TestConsumersForMCPWithCopilot (F-0444): the project .mcp.json is Copilot's
// workspace file too, but only a project that uses Copilot counts it.
func TestConsumersForMCPWithCopilot(t *testing.T) {
	it := Item{Class: "mcp", Name: "context-mode", Origin: OriginProject}
	if got := consumersFor(it, true); !reflect.DeepEqual(got, []string{"claude", "copilot"}) {
		t.Errorf("with copilot: %v", got)
	}
	user := Item{Class: "mcp", Name: "x", Origin: OriginUser}
	if got := consumersFor(user, true); !reflect.DeepEqual(got, []string{"claude"}) {
		t.Errorf("user declaration with copilot: %v", got)
	}
}

// TestInspectMCPConsumersFollowCopilotDetection: end to end through Inspect.
func TestInspectMCPConsumersFollowCopilotDetection(t *testing.T) {
	proj := t.TempDir()
	writeFile(t, filepath.Join(proj, ".mcp.json"), `{"mcpServers":{"s":{"command":"/bin/sh"}}}`)
	it, ok := findItem(Inspect(Options{ProjDir: proj}), "mcp", "s")
	if !ok || reflect.DeepEqual(it.Consumers, []string{"claude", "copilot"}) {
		t.Fatalf("non-Copilot project: %+v", it)
	}
	writeFile(t, filepath.Join(proj, ".github", "copilot-instructions.md"), "x")
	it, _ = findItem(Inspect(Options{ProjDir: proj}), "mcp", "s")
	if !reflect.DeepEqual(it.Consumers, []string{"claude", "copilot"}) {
		t.Fatalf("Copilot project: %+v", it)
	}
}

// TestInspectAnnotatesConsumers: every item of a real inventory carries a
// non-empty consumers list (criterion 1).
func TestInspectAnnotatesConsumers(t *testing.T) {
	proj := t.TempDir()
	writeFile(t, filepath.Join(proj, ".claude", "settings.json"), settingsWiring("x.sh"))
	writeFile(t, filepath.Join(proj, ".claude", "hooks", "x.sh"), "#!/bin/sh\n")
	writeFile(t, filepath.Join(proj, "AGENTS.md"), "# agents\n")

	inv := Inspect(Options{ProjDir: proj})
	seen := 0
	for _, c := range inv.Classes {
		for _, it := range c.Items {
			seen++
			if len(it.Consumers) == 0 {
				t.Errorf("item %s/%s has no consumers", c.Name, it.Name)
			}
		}
	}
	if seen == 0 {
		t.Fatal("inventory came back empty")
	}
}
