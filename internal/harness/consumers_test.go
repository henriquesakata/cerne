package harness

import (
	"path/filepath"
	"reflect"
	"testing"
)

// TestConsumersFor: deterministic attribution per class/origin (F-0259 + F-0526
// crit. 1) — which AI providers can consume each inventory item.
func TestConsumersFor(t *testing.T) { // F-0526 crit. 1
	all := []string{"claude", "cursor", "codex", "gemini", "copilot"}
	tests := []struct {
		name string
		item Item
		want []string
	}{
		{"cerne class is provider-agnostic", Item{Class: "cerne", Name: "docs.lock"}, all},
		// --- hooks ---
		{"path binary serves any agent", Item{Class: "hooks", Name: "rtk", Origin: OriginPath}, all},
		{"git commit-msg hook serves any agent", Item{Class: "hooks", Name: "commit-msg", Origin: OriginLocal}, all},
		{"git prepare-commit-msg hook serves any agent", Item{Class: "hooks", Name: "prepare-commit-msg", Origin: OriginProject}, all},
		{"subagent-context is claude-only (SubagentStart path)", Item{Class: "hooks", Name: "cerne-subagent-context.sh", Path: "SubagentStart", Origin: OriginProject}, []string{"claude"}},
		{"third-party SubagentStart wiring is claude-only", Item{Class: "hooks", Name: "rtk", Path: "SubagentStart", Origin: OriginUser}, []string{"claude"}},
		{"cursor project hook entry is cursor-only", Item{Class: "hooks", Name: "cerne", Origin: OriginCursorProject}, []string{"cursor"}},
		{"cursor user hook entry is cursor-only", Item{Class: "hooks", Name: "cerne", Origin: OriginCursorUser}, []string{"cursor"}},
		{"project hook reads claude and cursor", Item{Class: "hooks", Name: "cerne-work-gate.sh", Origin: OriginProject}, []string{"claude", "cursor"}},
		// --- skills ---
		{"claude project skill reads claude and cursor", Item{Class: "skills", Name: "cerne-authoring", Path: ".claude/skills/cerne-authoring/SKILL.md", Origin: OriginProject}, []string{"claude", "cursor"}},
		{"cursor project skill is cursor-only", Item{Class: "skills", Name: "cerne-authoring", Path: ".cursor/skills/cerne-authoring/SKILL.md", Origin: OriginProject}, []string{"cursor"}},
		{"agents project skill reads cursor/codex/gemini/copilot", Item{Class: "skills", Name: "s", Path: ".agents/skills/s/SKILL.md", Origin: OriginProject}, []string{"cursor", "codex", "gemini", "copilot"}},
		// A project checked out under ~/.cursor/worktrees/ (Cursor's parallel
		// agents) is still the project: its .agents skill is not Cursor's alone.
		{"agents skill of a project under ~/.cursor/worktrees", Item{Class: "skills", Name: "s", Path: "/home/.cursor/worktrees/repo/abc/.agents/skills/s", Origin: OriginProject}, []string{"cursor", "codex", "gemini", "copilot"}},
		{"user cursor skill is cursor-only", Item{Class: "skills", Name: "u", Path: "/home/.cursor/skills/u/SKILL.md", Origin: OriginUser}, []string{"cursor"}},
		// --- config ---
		{".cernerc serves all providers", Item{Class: "config", Name: ".cernerc", Origin: OriginProject}, all},
		{"cursor hooks.json config is cursor-only", Item{Class: "config", Name: "hooks.json", Path: "/proj/.cursor/hooks.json", Origin: OriginProject}, []string{"cursor"}},
		{"settings layer reads claude and cursor", Item{Class: "config", Name: "settings.json", Origin: OriginProject}, []string{"claude", "cursor"}},
		// Cursor reads neither MCP file (F-0512, R-0147): their config lines
		// name the readers their servers have.
		{"project .mcp.json layer is not cursor's", Item{Class: "config", Name: ".mcp.json", Path: "/proj/.mcp.json", Origin: OriginProject}, []string{"claude"}},
		{"user .claude.json layer is not cursor's", Item{Class: "config", Name: ".claude.json", Path: "/home/.claude.json", Origin: OriginUser}, []string{"claude"}},
		// --- plugins ---
		{"marketplace plugin reads claude and cursor", Item{Class: "plugins", Name: "caveman", Origin: OriginPlugin}, []string{"claude", "cursor"}},
		// --- mcp ---
		{"project .mcp.json is claude's when the project has no Copilot", Item{Class: "mcp", Name: "context-mode", Origin: OriginProject}, []string{"claude"}},
		{"user mcp declaration is claude's", Item{Class: "mcp", Name: "context-mode", Origin: OriginUser}, []string{"claude"}},
		// --- context ---
		{"project CLAUDE.md feeds claude and cursor", Item{Class: "context", Name: "CLAUDE.md", Origin: OriginProject}, []string{"claude", "cursor"}},
		{"user CLAUDE.md feeds claude only", Item{Class: "context", Name: "~/.claude/CLAUDE.md", Origin: OriginUser}, []string{"claude"}},
		{"AGENTS.md feeds codex, cursor and copilot", Item{Class: "context", Name: "AGENTS.md"}, []string{"codex", "cursor", "copilot"}},
		// --- providers ---
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

// TestConsumersForCursorProjectDetectedNoFalseGaps (F-0526 crit. 5):
// a project with only .cernerc and .claude/* (no .cursor/) has cursor
// Detected=false and no false "bound to a surface" gaps for shared surfaces.
func TestConsumersForCursorProjectDetectedNoFalseGaps(t *testing.T) { // F-0526 crit. 5
	proj := t.TempDir()
	writeFile(t, filepath.Join(proj, ".cernerc"), "project_id = \"test\"\n")
	writeFile(t, filepath.Join(proj, ".claude", "settings.json"), settingsWiring("x.sh"))
	writeFile(t, filepath.Join(proj, ".claude", "hooks", "x.sh"), "#!/bin/sh\n")
	writeFile(t, filepath.Join(proj, "CLAUDE.md"), "# project\n")

	inv := Inspect(Options{ProjDir: proj})

	// cursor must NOT be detected.
	var cursorCov ProviderCoverage
	for _, c := range inv.Coverage {
		if c.Provider == "cursor" {
			cursorCov = c
		}
	}
	if cursorCov.Detected {
		t.Error("cursor must not be detected with only .claude/* and .cernerc")
	}

	// No gap should say "bound to a surface cursor does not read" for
	// .cernerc, settings.json, CLAUDE.md, .claude hooks, or plugins.
	for _, g := range cursorCov.Gaps {
		if g.Kind == StateDegraded && g.Class != "providers" {
			t.Errorf("false gap in cursor coverage (non-provider degraded): %+v", g)
		}
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
	layer := Item{Class: "config", Name: ".mcp.json", Path: "/proj/.mcp.json", Origin: OriginProject} // F-0526 crit. 1
	if got := consumersFor(layer, true); !reflect.DeepEqual(got, []string{"claude", "copilot"}) {
		t.Errorf("project .mcp.json config line with copilot: %v", got)
	}
}

// TestConfigLineOfAnMCPFileMatchesItsServers (F-0526 crit. 1): through
// Inspect, the config line of .mcp.json and of ~/.claude.json names the same
// readers as the servers declared in it.
func TestConfigLineOfAnMCPFileMatchesItsServers(t *testing.T) { // F-0526 crit. 1
	proj, home := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(proj, ".mcp.json"), `{"mcpServers":{"s":{"command":"/bin/sh"}}}`)
	writeFile(t, filepath.Join(home, ".claude.json"), `{"mcpServers":{"u":{"command":"/bin/sh"}}}`)
	inv := Inspect(Options{ProjDir: proj, Home: home})
	for layer, server := range map[string]string{".mcp.json": "s", ".claude.json": "u"} {
		cfg, ok := findItem(inv, "config", layer)
		srv, ok2 := findItem(inv, "mcp", server)
		if !ok || !ok2 || !reflect.DeepEqual(cfg.Consumers, srv.Consumers) {
			t.Errorf("%s: config line consumers %v, its server's %v", layer, cfg.Consumers, srv.Consumers)
		}
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
