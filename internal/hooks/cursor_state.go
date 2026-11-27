package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
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

// CursorSandboxState checks the folders cerne needs (sandboxPaths, portable
// and machine-specific) against what Cursor's sandbox opens: the union of the
// project's .cursor/sandbox.json and ~/.cursor/sandbox.json (F-0512). With a
// machine-specific folder the install writes nothing to the project file and
// prints the block to merge into the user file, so the user file counts too.
//   - FileDivergent: the project file is not JSON with a string list, or the
//     union lacks a needed folder;
//   - FileMissing: a folder is needed and neither file exists;
//   - FileOK: nothing is needed (no .cernerc) or the union holds every folder.
//
// An unreadable file fails open (FileOK), as CopilotHooksState does.
func CursorSandboxState(projDir string) string {
	inProject, pState := sandboxFolders(filepath.Join(projDir, ".cursor", "sandbox.json"))
	portable, machine, _, err := sandboxPaths(projDir)
	need := slices.Concat(portable, machine)
	switch {
	case pState == "":
		return FileOK
	case pState == FileDivergent:
		return FileDivergent
	case err != nil || len(need) == 0:
		return FileOK
	}
	inUser, uState := []string(nil), FileMissing
	if home, err := os.UserHomeDir(); err == nil {
		inUser, uState = sandboxFolders(filepath.Join(home, ".cursor", "sandbox.json"))
	}
	have := slices.Concat(inProject, inUser)
	switch {
	case uState == "":
		return FileOK
	case !slices.ContainsFunc(need, func(f string) bool { return !slices.Contains(have, f) }):
		return FileOK
	case pState == FileMissing && uState == FileMissing:
		return FileMissing
	}
	return FileDivergent
}

// sandboxFolders reads additionalReadwritePaths from one sandbox.json:
// FileMissing when absent, FileDivergent when it is not JSON or not a string
// list, "" when unreadable (the caller fails open), FileOK otherwise.
func sandboxFolders(path string) ([]string, string) {
	raw, err := os.ReadFile(path)
	switch {
	case os.IsNotExist(err):
		return nil, FileMissing
	case err != nil:
		return nil, ""
	}
	var doc map[string]any
	if json.Unmarshal(raw, &doc) != nil {
		return nil, FileDivergent
	}
	paths, ok := stringList(doc["additionalReadwritePaths"])
	if !ok {
		return nil, FileDivergent
	}
	return paths, FileOK
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
