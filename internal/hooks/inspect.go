package hooks

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
)

// This file is the read-only sister of Install (F-0251): it exposes the asset
// table and per-asset drift so the harness inventory can report state without
// a second copy of what "canonical" means. Nothing here writes.

// Asset is one managed Claude Code hook capability: the script cerne installs
// under .claude/hooks/ plus the settings.json wiring that makes it fire.
type Asset struct {
	Script  string // file name under .claude/hooks/
	Command string // settings.json command line
	Event   string // hook event the wiring registers under
	Matcher string // matcher, "" for matcher-less events
}

// Assets returns the managed hook capabilities, in install order.
func Assets() []Asset {
	return []Asset{
		{Script: hookScript, Command: hookCommand, Event: "PostToolUse", Matcher: "Bash"},
		{Script: registerScript, Command: registerCommand, Event: "SessionStart"},
		{Script: subagentScript, Command: subagentCommand, Event: "SubagentStart"},
		{Script: bindScript, Command: bindCommand, Event: "PostToolUse", Matcher: "Bash"},
		{Script: gateScript, Command: gateCommand, Event: "PreToolUse", Matcher: gateMatcher},
		{Script: lifecycleScript, Command: lifecycleCommand, Event: "PostToolUse", Matcher: "Bash"},
		{Script: stopScript, Command: stopCommand, Event: "Stop"},
	}
}

// File states reported by the inspection — the managed-artifact vocabulary of
// the harness inventory.
const (
	FileOK        = "ok"
	FileDivergent = "divergent"
	FileMissing   = "missing"
)

// ScriptState compares the installed copy of script (under .claude/hooks/)
// against the canonical embed, byte for byte — the same equality writeIfChanged
// uses, without the write.
func ScriptState(projDir, script string) string {
	canonical := []byte(mustRead("assets/" + script))
	disk, err := os.ReadFile(filepath.Join(projDir, ".claude", "hooks", script))
	switch {
	case err != nil:
		return FileMissing
	case bytes.Equal(disk, canonical):
		return FileOK
	default:
		return FileDivergent
	}
}

// GitHookState is the state of one managed .git/hooks slot.
type GitHookState struct {
	Name   string // hook slot (prepare-commit-msg, commit-msg)
	State  string // FileOK / FileMissing / FileDivergent
	Detail string
}

// GitHookStates inspects the two git hook slots Install manages. A foreign
// hook without our content/marker reports divergent with the manual snippet —
// install deliberately skips it, so "reinstall" is not the whole fix.
func GitHookStates(projDir string) []GitHookState {
	out := make([]GitHookState, 0, 2)

	trailerPath := filepath.Join(projDir, ".git", "hooks", trailerHookName)
	if raw, err := os.ReadFile(trailerPath); err != nil {
		out = append(out, GitHookState{Name: trailerHookName, State: FileMissing})
	} else if string(raw) == trailerHookContent {
		out = append(out, GitHookState{Name: trailerHookName, State: FileOK})
	} else {
		out = append(out, GitHookState{Name: trailerHookName, State: FileDivergent, Detail: "foreign hook in slot — integrate manually: " + trailerSnippet})
	}

	guardPath := filepath.Join(projDir, ".git", "hooks", guardHookName)
	if raw, err := os.ReadFile(guardPath); err != nil {
		out = append(out, GitHookState{Name: guardHookName, State: FileMissing})
	} else if strings.Contains(string(raw), guardMarker) {
		out = append(out, GitHookState{Name: guardHookName, State: FileOK})
	} else {
		out = append(out, GitHookState{Name: guardHookName, State: FileDivergent, Detail: "foreign hook in slot — integrate manually: " + guardSnippet})
	}
	return out
}
