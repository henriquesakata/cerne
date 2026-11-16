package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/mcp"
)

// TestDetectCopilot (F-0444 crit. 1): Copilot is detected by its own .github
// files, never by a bare .github/ (every repo has one for Actions).
func TestDetectCopilot(t *testing.T) {
	has := func(dir string) bool {
		for _, p := range DetectProviders(dir) {
			if p == ProviderCopilot {
				return true
			}
		}
		return false
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".github", "workflows"), 0o755); err != nil {
		t.Fatal(err)
	}
	if has(dir) {
		t.Fatal("a .github/ with only workflows must not detect copilot")
	}
	for _, marker := range []struct {
		rel  string
		file bool
	}{
		{".github/copilot-instructions.md", true},
		{".github/hooks", false},
		{".github/skills", false},
		{".github/agents", false},
	} {
		d := t.TempDir()
		p := filepath.Join(d, filepath.FromSlash(marker.rel))
		if marker.file {
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		} else if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		if !has(d) {
			t.Fatalf("%s must detect copilot", marker.rel)
		}
	}
	if ConfigDir(ProviderCopilot) != ".github" {
		t.Fatalf("ConfigDir(copilot) = %q", ConfigDir(ProviderCopilot))
	}
}

func copilotHooksPath(dir string) string {
	return filepath.Join(dir, ".github", "hooks", "cerne.json")
}

// TestInstallCopilotHooksFile (F-0444 crit. 1): the exact flat-entry file the
// Copilot hook reference documents, fail-open on both shells.
func TestInstallCopilotHooksFile(t *testing.T) {
	dir := t.TempDir()
	results, err := Install(dir, ProviderCopilot)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(copilotHooksPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Version int                         `json:"version"`
		Hooks   map[string][]map[string]any `json:"hooks"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, raw)
	}
	if doc.Version != 1 {
		t.Fatalf("version = %d", doc.Version)
	}
	type want struct{ event, matcher, inv string }
	for _, w := range []want{
		{"SessionStart", "", "cerne hook session-context --format copilot"},
		{"PreToolUse", "Edit|Write", "cerne hook work-gate --format copilot"},
		{"PostToolUse", "Bash", "cerne hook bind-work --format copilot"},
		{"PostToolUse", "Bash", "cerne hook provenance --format copilot"},
		{"Stop", "", "cerne hook stop-check"},
	} {
		var found map[string]any
		for _, e := range doc.Hooks[w.event] {
			if b, _ := e["bash"].(string); strings.Contains(b, w.inv+" 2>/dev/null") {
				found = e
			}
		}
		if found == nil {
			t.Fatalf("%s: no entry for %q in\n%s", w.event, w.inv, raw)
		}
		if found["type"] != "command" {
			t.Fatalf("%s: type = %v", w.event, found["type"])
		}
		if m, _ := found["matcher"].(string); m != w.matcher {
			t.Fatalf("%s %q: matcher = %q", w.event, w.inv, m)
		}
		ps, _ := found["powershell"].(string)
		if !strings.Contains(ps, "Get-Command cerne") || !strings.HasSuffix(ps, "exit 0") || !strings.Contains(ps, w.inv) {
			t.Fatalf("%s: powershell not fail-open: %q", w.event, ps)
		}
		env, _ := found["env"].(map[string]any)
		if env["CERNE_ACTOR"] != "agent" || env["CERNE_PROVIDER"] != "copilot" {
			t.Fatalf("%s: env = %v", w.event, env)
		}
	}
	if st := statuses(results)["copilot: hooks/cerne.json"]; st != StatusCreated {
		t.Fatalf("first run status = %v: %v", st, results)
	}
	// The handoff on compaction is not hosted: reported, not silent.
	// Every degraded parity row is a skipped line, matrix and install agree.
	for _, r := range ParityRows() {
		if d, _ := r.Degraded(ProviderCopilot); d {
			found := false
			for _, res := range results {
				if res.Status == StatusSkipped && strings.HasPrefix(res.Asset, "copilot: ") && res.Note != "" && strings.Contains(strings.ToLower(res.Asset+res.Note), strings.ToLower(strings.Fields(r.Capability)[0])) {
					found = true
				}
			}
			if !found {
				t.Errorf("degraded row %q has no skipped line: %v", r.Capability, results)
			}
		}
	}

	results, err = Install(dir, ProviderCopilot)
	if err != nil {
		t.Fatal(err)
	}
	if st := statuses(results)["copilot: hooks/cerne.json"]; st != StatusUnchanged {
		t.Fatalf("second run status = %v", st)
	}
}

// TestInstallCopilotHooksTouchesOnlyItsFile (crit. 1/6): neighbours are never
// read or touched; a corrupt cerne.json is left as is and reported skipped.
func TestInstallCopilotHooksTouchesOnlyItsFile(t *testing.T) {
	dir := t.TempDir()
	hd := filepath.Join(dir, ".github", "hooks")
	if err := os.MkdirAll(hd, 0o755); err != nil {
		t.Fatal(err)
	}
	team := filepath.Join(hd, "team.json")
	if err := os.WriteFile(team, []byte("not json at all"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(dir, ProviderCopilot); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(team); string(b) != "not json at all" {
		t.Fatalf("team.json touched: %q", b)
	}

	if err := os.WriteFile(copilotHooksPath(dir), []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	results, err := Install(dir, ProviderCopilot)
	if err != nil {
		t.Fatalf("a corrupt provider file must not fail the run: %v", err)
	}
	if b, _ := os.ReadFile(copilotHooksPath(dir)); string(b) != "{broken" {
		t.Fatalf("corrupt cerne.json rewritten: %q", b)
	}
	if st := statuses(results)["copilot: hooks/cerne.json"]; st != StatusSkipped {
		t.Fatalf("corrupt file status = %v", st)
	}
}

func wantMCPServer() map[string]any {
	var tools []any
	for _, n := range mcp.ToolNames {
		tools = append(tools, n)
	}
	return map[string]any{"type": "local", "command": "cerne", "args": []any{"mcp"}, "tools": tools}
}

func readMCP(t *testing.T, dir string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("invalid .mcp.json: %v\n%s", err, raw)
	}
	return m
}

// TestInstallCopilotMCP (F-0444 crit. 3): the read-only server lands in the
// root .mcp.json with the explicit tool list, beside the team's servers.
func TestInstallCopilotMCP(t *testing.T) {
	dir := t.TempDir()
	team := `{"mcpServers":{"team":{"command":"x"}},"other":1}`
	if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(team), 0o644); err != nil {
		t.Fatal(err)
	}
	results, err := Install(dir, ProviderCopilot)
	if err != nil {
		t.Fatal(err)
	}
	m := readMCP(t, dir)
	servers := m["mcpServers"].(map[string]any)
	if !reflect.DeepEqual(servers["cerne"], wantMCPServer()) {
		t.Fatalf("cerne server = %v", servers["cerne"])
	}
	if servers["team"] == nil || m["other"] == nil {
		t.Fatalf("team server or key lost: %v", m)
	}
	if st := statuses(results)["copilot: .mcp.json"]; st != StatusUpdated {
		t.Fatalf("status = %v", st)
	}
	// The cloud agent does not read .mcp.json: the block to paste is printed.
	var note string
	for _, r := range results {
		if r.Asset == "copilot: cloud agent MCP" {
			note = r.Note
			if r.Status != StatusSkipped {
				t.Fatalf("cloud line status = %v", r.Status)
			}
		}
	}
	if !strings.Contains(note, `"cerne"`) || !strings.Contains(note, "copilot-setup-steps.yml") || !strings.Contains(note, `"tools"`) {
		t.Fatalf("cloud note lacks the block or the setup-steps hint:\n%s", note)
	}

	results, _ = Install(dir, ProviderCopilot)
	if st := statuses(results)["copilot: .mcp.json"]; st != StatusUnchanged {
		t.Fatalf("second run = %v", st)
	}
}

// TestInstallCopilotMCPCreates: no .mcp.json yet → created.
func TestInstallCopilotMCPCreates(t *testing.T) {
	dir := t.TempDir()
	results, _ := Install(dir, ProviderCopilot)
	if st := statuses(results)["copilot: .mcp.json"]; st != StatusCreated {
		t.Fatalf("status = %v", st)
	}
	if !reflect.DeepEqual(readMCP(t, dir)["mcpServers"].(map[string]any)["cerne"], wantMCPServer()) {
		t.Fatal("server not written")
	}
}

// TestInstallCopilotMCPNeverClobbers (crit. 3/6): a different `cerne` entry and
// a corrupt file stay as they are, reported skipped.
func TestInstallCopilotMCPNeverClobbers(t *testing.T) {
	for name, content := range map[string]string{
		"divergent": `{"mcpServers":{"cerne":{"command":"/opt/mine"}}}`,
		"corrupt":   `{"mcpServers":`,
		"bad type":  `{"mcpServers":[]}`,
	} {
		dir := t.TempDir()
		path := filepath.Join(dir, ".mcp.json")
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		results, err := Install(dir, ProviderCopilot)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if b, _ := os.ReadFile(path); string(b) != content {
			t.Fatalf("%s: file rewritten: %s", name, b)
		}
		if st := statuses(results)["copilot: .mcp.json"]; st != StatusSkipped {
			t.Fatalf("%s: status = %v", name, st)
		}
	}
}

// TestCopilotStates (F-0444 crit. 5): the hooks file and the MCP server report
// ok / divergent / missing, and only a divergent hooks file is "stale" for the
// session-start check (a missing one is not nagged: the team may not want it).
func TestCopilotStates(t *testing.T) {
	dir := t.TempDir()
	if CopilotHooksState(dir) != FileMissing || CopilotMCPState(dir) != FileMissing {
		t.Fatal("empty project must be missing")
	}
	if len(Stale(dir)) != len(Assets()) {
		t.Fatalf("missing copilot file must not be stale: %v", Stale(dir))
	}
	if _, err := Install(dir, ProviderCopilot); err != nil {
		t.Fatal(err)
	}
	if CopilotHooksState(dir) != FileOK || CopilotMCPState(dir) != FileOK {
		t.Fatalf("fresh install: hooks %s mcp %s", CopilotHooksState(dir), CopilotMCPState(dir))
	}
	if err := os.WriteFile(copilotHooksPath(dir), []byte(`{"version":1,"hooks":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(`{"mcpServers":{"cerne":{"command":"x"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if CopilotHooksState(dir) != FileDivergent || CopilotMCPState(dir) != FileDivergent {
		t.Fatalf("edited: hooks %s mcp %s", CopilotHooksState(dir), CopilotMCPState(dir))
	}
	found := false
	for _, s := range Stale(dir) {
		if s == "copilot: .github/hooks/cerne.json" {
			found = true
		}
	}
	if !found {
		t.Fatalf("divergent copilot hooks file not in Stale: %v", Stale(dir))
	}
}
