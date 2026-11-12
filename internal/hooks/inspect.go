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

// GitHookState is the state of one managed git hook slot.
type GitHookState struct {
	Name     string // hook slot (prepare-commit-msg, commit-msg)
	State    string // FileOK / FileMissing / FileDivergent
	Detail   string
	Path     string // file read, relative to projDir when inside it (F-0360)
	External bool   // owned by a hook manager (core.hooksPath): install skips it
}

// GitHookStates inspects the two git hook slots Install manages. A foreign
// hook without our content/marker reports divergent with the manual snippet —
// install deliberately skips it, so "reinstall" is not the whole fix. With a
// core.hooksPath the slots belong to the hook manager (F-0360): each is ok when
// the file the manager runs carries the cerne invocation, missing otherwise.
func GitHookStates(projDir string) []GitHookState {
	if hp, set := GitHooksPath(projDir); set {
		return []GitHookState{
			managedSlot(projDir, hp, trailerHookName, trailerSnippet),
			managedSlot(projDir, hp, guardHookName, guardSnippet),
		}
	}
	out := make([]GitHookState, 0, 2)

	trailerRel := filepath.Join(".git", "hooks", trailerHookName)
	if raw, err := os.ReadFile(filepath.Join(projDir, trailerRel)); err != nil {
		out = append(out, GitHookState{Name: trailerHookName, State: FileMissing, Path: trailerRel})
	} else if string(raw) == trailerHookContent {
		out = append(out, GitHookState{Name: trailerHookName, State: FileOK, Path: trailerRel})
	} else {
		out = append(out, GitHookState{Name: trailerHookName, State: FileDivergent, Path: trailerRel, Detail: "foreign hook in slot — integrate manually: " + trailerSnippet})
	}

	guardRel := filepath.Join(".git", "hooks", guardHookName)
	if raw, err := os.ReadFile(filepath.Join(projDir, guardRel)); err != nil {
		out = append(out, GitHookState{Name: guardHookName, State: FileMissing, Path: guardRel})
	} else if strings.Contains(string(raw), guardMarker) {
		out = append(out, GitHookState{Name: guardHookName, State: FileOK, Path: guardRel})
	} else {
		out = append(out, GitHookState{Name: guardHookName, State: FileDivergent, Path: guardRel, Detail: "foreign hook in slot — integrate manually: " + guardSnippet})
	}
	return out
}

// managedSlot looks for the cerne invocation of one slot under a hook
// manager's core.hooksPath: <hooksPath>/<slot>, and for husky 9 (hooksPath
// ending in .husky/_) first .husky/<slot>, the file its runner executes.
func managedSlot(projDir, hooksPath, slot, snippet string) GitHookState {
	dir := hooksPath
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(projDir, dir)
	}
	candidates := []string{filepath.Join(dir, slot)}
	if clean := filepath.Clean(dir); filepath.Base(clean) == "_" && filepath.Base(filepath.Dir(clean)) == ".husky" {
		candidates = append([]string{filepath.Join(filepath.Dir(dir), slot)}, candidates...)
	}
	invocation, _, _ := strings.Cut(snippet, ` "$1"`)
	st := GitHookState{Name: slot, State: FileMissing, External: true,
		Detail: "core.hooksPath=" + hooksPath + " — add to your hook manager: " + snippet}
	found := ""
	for _, c := range candidates {
		raw, err := os.ReadFile(c)
		if err != nil {
			continue
		}
		if found == "" {
			found = c
		}
		if callsCerne(string(raw), invocation) {
			found, st.State, st.Detail = c, FileOK, ""
			break
		}
	}
	if found == "" {
		found = candidates[len(candidates)-1]
	}
	st.Path = found
	if rel, ok := relInside(projDir, found); ok {
		st.Path = rel
	}
	return st
}

// relInside returns path relative to root when it lies inside it, resolving
// symlinks on both sides (/var vs /private/var on macOS) when the plain paths
// disagree.
func relInside(root, path string) (string, bool) {
	inside := func(r, p string) (string, bool) {
		rel, err := filepath.Rel(r, p)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", false
		}
		return rel, true
	}
	if rel, ok := inside(root, path); ok {
		return rel, true
	}
	r, err1 := filepath.EvalSymlinks(root)
	p, err2 := filepath.EvalSymlinks(path)
	if err1 != nil || err2 != nil {
		return "", false
	}
	return inside(r, p)
}

// callsCerne reports whether a hook body runs the invocation on a line that is
// not a shell comment.
func callsCerne(body, invocation string) bool {
	for _, line := range strings.Split(body, "\n") {
		if t := strings.TrimSpace(line); !strings.HasPrefix(t, "#") && strings.Contains(t, invocation) {
			return true
		}
	}
	return false
}
