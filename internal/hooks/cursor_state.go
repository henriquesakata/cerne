package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

// CursorRule returns the canonical content of .cursor/rules/cerne.mdc — the
// same string installCursor writes. Consumers compare against it to detect drift.
func CursorRule() string { return cursorRules }

// cursorMCPServer is the read-only cerne server as Cursor's .cursor/mcp.json
// wants it (no tools list — Cursor resolves them dynamically, unlike Copilot).
func cursorMCPServer() map[string]any {
	return map[string]any{"type": "stdio", "command": "cerne", "args": []any{"mcp"}}
}

// CursorRuleState compares .cursor/rules/cerne.mdc with CursorRule():
// FileOK when matching, FileDivergent when different, FileMissing when absent.
// A non-NotExist read error returns FileOK (fail-open, same as CopilotHooksState).
func CursorRuleState(projDir string) string {
	disk, err := os.ReadFile(filepath.Join(projDir, ".cursor", "rules", "cerne.mdc"))
	if os.IsNotExist(err) {
		return FileMissing
	}
	if err != nil {
		return FileOK // unreadable → fail-open
	}
	if string(disk) == CursorRule() {
		return FileOK
	}
	return FileDivergent
}

// cursorSessionStartCmd is the distinctive part of the managed sessionStart
// command that installCursor writes into .cursor/hooks.json.
const cursorSessionStartCmd = "cerne hook session-context --format cursor"

// CursorHooksState checks whether .cursor/hooks.json exists and contains a
// sessionStart entry with the managed cerne command:
//   - FileMissing if absent
//   - FileDivergent if invalid JSON or no sessionStart entry with the managed command
//   - FileOK when valid JSON containing the managed sessionStart command
//     (extra third-party or additional cerne hooks are allowed)
func CursorHooksState(projDir string) string {
	raw, err := os.ReadFile(filepath.Join(projDir, ".cursor", "hooks.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return FileMissing
		}
		return FileOK // unreadable → fail-open
	}
	var doc struct {
		Hooks map[string][]struct {
			Command string `json:"command"`
		} `json:"hooks"`
	}
	if json.Unmarshal(raw, &doc) != nil {
		return FileDivergent
	}
	for _, entry := range doc.Hooks["sessionStart"] {
		if strings.Contains(entry.Command, cursorSessionStartCmd) {
			return FileOK
		}
	}
	return FileDivergent
}

// cursorSandboxDirs returns the portable paths that cerne expects in
// .cursor/sandbox.json for projDir. Returns nil when .cernerc is absent or
// unreadable (no sandbox paths required), and nil on any other error.
func cursorSandboxDirs(projDir string) []string {
	portable, _, _, err := sandboxPaths(projDir)
	if err != nil {
		return nil
	}
	return portable
}

// CursorSandboxState checks .cursor/sandbox.json:
//   - When no extra dirs are required (cursorSandboxDirs returns nil/empty):
//     FileOK when file absent or valid JSON; FileDivergent when invalid JSON.
//   - When extra dirs are required:
//     FileMissing when file absent; FileDivergent when invalid JSON or missing
//     any expected dir; FileOK when all expected dirs are present.
func CursorSandboxState(projDir string) string {
	expectedDirs := cursorSandboxDirs(projDir)
	raw, err := os.ReadFile(filepath.Join(projDir, ".cursor", "sandbox.json"))
	if err != nil {
		if os.IsNotExist(err) {
			if len(expectedDirs) == 0 {
				return FileOK
			}
			return FileMissing
		}
		return FileOK // unreadable → fail-open
	}
	// File exists: validate JSON.
	var doc map[string]any
	if json.Unmarshal(raw, &doc) != nil {
		return FileDivergent
	}
	if len(expectedDirs) == 0 {
		return FileOK
	}
	// Check all expected dirs are present.
	paths, ok := stringList(doc["additionalReadwritePaths"])
	if !ok {
		return FileDivergent
	}
	for _, want := range expectedDirs {
		found := false
		for _, have := range paths {
			if have == want {
				found = true
				break
			}
		}
		if !found {
			return FileDivergent
		}
	}
	return FileOK
}

// CursorMCPState compares mcpServers["cerne"] in .cursor/mcp.json against the
// canonical Cursor MCP server:
//   - FileMissing if file absent or "cerne" key absent
//   - FileDivergent if invalid JSON or entry differs
//   - FileOK when matching (other mcpServers entries are ignored)
func CursorMCPState(projDir string) string {
	raw, err := os.ReadFile(filepath.Join(projDir, ".cursor", "mcp.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return FileMissing
		}
		return FileOK // unreadable → fail-open
	}
	var doc struct {
		Servers map[string]any `json:"mcpServers"`
	}
	if json.Unmarshal(raw, &doc) != nil {
		return FileDivergent
	}
	cur, has := doc.Servers["cerne"]
	if !has {
		return FileMissing
	}
	var want any
	wantRaw, _ := json.Marshal(cursorMCPServer())
	_ = json.Unmarshal(wantRaw, &want)
	if reflect.DeepEqual(cur, want) {
		return FileOK
	}
	return FileDivergent
}
