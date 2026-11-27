package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/hooks"
	"github.com/henriquesakata/cerne/internal/skills"
)

var cursorItemNames = []string{
	"cursor: rules file",
	"cursor: hooks file",
	"cursor: mcp server",
	"cursor: skill",
}

// cursorSandboxItemName returns the sandbox item name. It may be absent
// when the project has no .cernerc (no expected dirs).
const cursorSandboxItemName = "cursor: sandbox file"

// TestCursorItems (F-0526 crit. 3): with Cursor detected, the inventory lists
// managed items — absent, ok — with a fix when missing/divergent.
func TestCursorItems(t *testing.T) { // F-0526 crit. 3
	proj := t.TempDir()
	// Detect cursor: create the .cursor/ directory (DetectProviders checks dir existence).
	if err := os.MkdirAll(filepath.Join(proj, ".cursor"), 0o755); err != nil {
		t.Fatal(err)
	}

	check := func(wantState string) {
		t.Helper()
		inv := Inspect(Options{ProjDir: proj})
		for _, n := range cursorItemNames {
			it, ok := findItem(inv, "providers", n)
			if !ok {
				t.Fatalf("item %q absent (state want %s)", n, wantState)
			}
			if it.State != wantState {
				t.Errorf("%s: state %q, want %q", n, it.State, wantState)
			}
			if wantState != StateOK && it.Fix == "" {
				t.Errorf("%s: no fix for a %s item", n, wantState)
			}
			if len(it.Consumers) != 1 || it.Consumers[0] != "cursor" {
				t.Errorf("%s: consumers %v, want [cursor]", n, it.Consumers)
			}
		}
	}

	check(StateMissing)

	// InstallNetwork installs rules, hooks, MCP and sandbox.
	if _, err := hooks.InstallNetwork(proj, false, hooks.ProviderCursor); err != nil {
		t.Fatal(err)
	}
	if _, err := skills.Install(proj, hooks.ProviderCursor); err != nil {
		t.Fatal(err)
	}
	check(StateOK)

	// Corrupt each managed file → divergent.
	for _, p := range []string{
		".cursor/rules/cerne.mdc",
		".cursor/hooks.json",
		".cursor/skills/cerne-authoring/SKILL.md",
	} {
		if err := os.WriteFile(filepath.Join(proj, filepath.FromSlash(p)), []byte(`{"x":"y"}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// mcp.json: a present but wrong cerne entry → divergent.
	if err := os.WriteFile(filepath.Join(proj, ".cursor", "mcp.json"),
		[]byte(`{"mcpServers":{"cerne":{"command":"wrong"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	check(StateDivergent)
}

// TestCursorItemsAbsentWhenNotDetected (F-0526 crit. 3): no .cursor dir → no items.
func TestCursorItemsAbsentWhenNotDetected(t *testing.T) { // F-0526 crit. 3
	inv := Inspect(Options{ProjDir: t.TempDir()})
	if _, ok := findItem(inv, "providers", "cursor: rules file"); ok {
		t.Fatal("cursor items listed for a project without .cursor/")
	}
}

// TestCursorSkillsInventoried (F-0526 crit. 2): skills in .cursor/skills/ and
// .agents/skills/ appear with the correct consumers.
func TestCursorSkillsInventoried(t *testing.T) { // F-0526 crit. 2
	proj := t.TempDir()
	home := t.TempDir()
	// Project .cursor/skills/cerne-authoring (managed, installed).
	if _, err := skills.Install(proj, hooks.ProviderCursor); err != nil {
		t.Fatal(err)
	}
	// Project .agents/skills/my-skill (third-party).
	writeFile(t, filepath.Join(proj, ".agents", "skills", "my-skill", "SKILL.md"), "# my skill\n")
	// User ~/.cursor/skills/user-skill.
	writeFile(t, filepath.Join(home, ".cursor", "skills", "user-skill", "SKILL.md"), "# user\n")
	// User ~/.agents/skills/agent-skill.
	writeFile(t, filepath.Join(home, ".agents", "skills", "agent-skill", "SKILL.md"), "# agent\n")

	inv := Inspect(Options{ProjDir: proj, Home: home})

	// .cursor/skills/cerne-authoring → consumers ["cursor"].
	// There may be two cerne-authoring items (.claude and .cursor copies);
	// look for the cursor-specific one.
	var cursorAuthoring *Item
	for ci := range inv.Classes {
		if inv.Classes[ci].Name != "skills" {
			continue
		}
		for ii := range inv.Classes[ci].Items {
			item := &inv.Classes[ci].Items[ii]
			if item.Name == "cerne-authoring" && len(item.Consumers) == 1 && item.Consumers[0] == "cursor" {
				cursorAuthoring = item
			}
		}
	}
	if cursorAuthoring == nil {
		t.Fatal("cursor cerne-authoring skill absent from inventory")
	}
	it := *cursorAuthoring

	// .agents/skills/my-skill → consumers ["cursor","codex","gemini","copilot"].
	agentConsumers := []string{"cursor", "codex", "gemini", "copilot"}
	mySkill, myOK := findItem(inv, "skills", "my-skill")
	if !myOK {
		t.Fatal(".agents/skills/my-skill absent from inventory")
	}
	for i, c := range agentConsumers {
		if i >= len(mySkill.Consumers) || mySkill.Consumers[i] != c {
			t.Errorf("my-skill consumers %v, want %v", mySkill.Consumers, agentConsumers)
			break
		}
	}

	// ~/.cursor/skills/user-skill → consumers ["cursor"], OutsideRepo=true.
	userSkill, userOK := findItem(inv, "skills", "user-skill")
	if !userOK {
		t.Fatal("~/.cursor/skills/user-skill absent from inventory")
	}
	if len(userSkill.Consumers) != 1 || userSkill.Consumers[0] != "cursor" {
		t.Errorf("user cursor skill consumers %v, want [cursor]", userSkill.Consumers)
	}
	if !userSkill.OutsideRepo {
		t.Error("user cursor skill should be OutsideRepo=true")
	}

	// ~/.agents/skills/agent-skill → consumers ["cursor","codex","gemini","copilot"], OutsideRepo=true.
	agentSkill, agentOK := findItem(inv, "skills", "agent-skill")
	if !agentOK {
		t.Fatal("~/.agents/skills/agent-skill absent from inventory")
	}
	if len(agentSkill.Consumers) == 0 || agentSkill.Consumers[0] != "cursor" {
		t.Errorf("agent-skill consumers %v, want cursor first", agentSkill.Consumers)
	}
	_ = it // used above for cursorAuthoring check
}

// TestCursorHooksJsonInventoried (F-0526 crit. 2): hooks in .cursor/hooks.json
// appear as hook items with consumers ["cursor"] and without leaking secrets.
func TestCursorHooksJsonInventoried(t *testing.T) { // F-0526 crit. 2
	proj := t.TempDir()
	home := t.TempDir()

	// Project .cursor/hooks.json with sessionStart entry.
	sessionCmd := "sh -c 'cerne hook session-context --format cursor 2>/dev/null || true'"
	hooksDoc := map[string]any{
		"version": 1,
		"hooks": map[string]any{
			"sessionStart": []any{
				map[string]any{"command": sessionCmd, "secret": "do-not-leak"},
			},
		},
	}
	raw, _ := json.MarshalIndent(hooksDoc, "", "  ")
	writeFile(t, filepath.Join(proj, ".cursor", "hooks.json"), string(raw))

	// User ~/.cursor/hooks.json with another entry.
	userHooksDoc := map[string]any{
		"version": 1,
		"hooks": map[string]any{
			"postToolUse": []any{
				map[string]any{"command": "echo user-cursor-hook"},
			},
		},
	}
	raw, _ = json.MarshalIndent(userHooksDoc, "", "  ")
	writeFile(t, filepath.Join(home, ".cursor", "hooks.json"), string(raw))

	inv := Inspect(Options{ProjDir: proj, Home: home})

	// Look for a hook item from the cursor hooks file.
	foundCursorHook := false
	for _, c := range inv.Classes {
		if c.Name != "hooks" {
			continue
		}
		for _, it := range c.Items {
			if len(it.Consumers) == 1 && it.Consumers[0] == "cursor" {
				foundCursorHook = true
				// Must not contain secrets.
				if it.Detail == "do-not-leak" || it.Name == "do-not-leak" {
					t.Errorf("cursor hook item leaks secret field: %+v", it)
				}
			}
		}
	}
	if !foundCursorHook {
		t.Error("no hook item with consumers=[cursor] found in inventory")
	}
}

// TestCursorCoverageWithDotCursorPresent (F-0526 crit. 5): with .cursor/
// present, every managed Cursor file that is missing or divergent is a gap of
// the cursor card, with the state and the command that repairs it; once the
// install is whole, none is.
func TestCursorCoverageWithDotCursorPresent(t *testing.T) { // F-0526 crit. 5
	proj := t.TempDir()
	writeFile(t, filepath.Join(proj, ".cursor", "rules", "cerne.mdc"), "rules\n") // divergent

	managedGaps := func() map[string]Gap {
		t.Helper()
		out := map[string]Gap{}
		for _, c := range Inspect(Options{ProjDir: proj}).Coverage {
			if c.Provider != "cursor" {
				continue
			}
			if !c.Detected {
				t.Fatal("cursor not detected despite .cursor/ present")
			}
			for _, g := range c.Gaps {
				if strings.HasPrefix(g.Name, "cursor: ") {
					out[g.Name] = g
				}
			}
		}
		return out
	}

	const hook, skill = "cerne hook install --provider cursor", "cerne skill install"
	got := managedGaps()
	for name, want := range map[string]Gap{
		"cursor: rules file": {Kind: StateMissing, Detail: ".cursor/rules/cerne.mdc is divergent", Fix: hook},
		"cursor: hooks file": {Kind: StateMissing, Detail: ".cursor/hooks.json is missing", Fix: hook},
		"cursor: mcp server": {Kind: StateMissing, Detail: ".cursor/mcp.json is missing", Fix: hook},
		"cursor: skill":      {Kind: StateMissing, Detail: ".cursor/skills/cerne-authoring/SKILL.md is missing", Fix: skill},
	} {
		g := got[name]
		if g.Kind != want.Kind || g.Detail != want.Detail || g.Fix != want.Fix || g.Class != "providers" {
			t.Errorf("gap %q = %+v, want %+v", name, g, want)
		}
	}

	if _, err := hooks.InstallNetwork(proj, false, hooks.ProviderCursor); err != nil {
		t.Fatal(err)
	}
	if _, err := skills.Install(proj, hooks.ProviderCursor); err != nil {
		t.Fatal(err)
	}
	if got := managedGaps(); len(got) != 0 {
		t.Errorf("gaps after a whole install: %+v", got)
	}
}
