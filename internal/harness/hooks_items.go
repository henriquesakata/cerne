package harness

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"gitlab.com/rockt/cerne/internal/hooks"
)

// fixHookInstall is the repair command shown for a managed hook capability
// that drifted or went missing.
const fixHookInstall = "cerne hook install"

// hookItems builds the Hooks class: managed capabilities (script + wiring
// evaluated separately, reported on one line — spec criterion 4), unmanaged
// scripts on disk, third-party wirings from any layer, and the git hook slots.
func hookItems(opts Options, ls layerSet) []Item {
	var items []Item

	managed := map[string]bool{}
	for _, a := range hooks.Assets() {
		managed[a.Script] = true
		items = append(items, managedCapability(opts.ProjDir, a, ls))
	}

	// Scripts on disk outside the managed set: present, no canonical to
	// compare against, no fix to suggest.
	hooksDir := filepath.Join(opts.ProjDir, ".claude", "hooks")
	if entries, err := os.ReadDir(hooksDir); err == nil {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			if !e.IsDir() && !managed[e.Name()] {
				names = append(names, e.Name())
			}
		}
		sort.Strings(names)
		for _, n := range names {
			items = append(items, Item{
				Class:  "hooks",
				Name:   n,
				Path:   filepath.Join(".claude", "hooks", n),
				Origin: OriginProject,
				State:  StateUnmanaged,
			})
		}
	}

	// Wirings that don't run a managed script: third-party hooks (e.g. a
	// user-level rtk rewrite hook). The executable must resolve or the line
	// is broken, naming the missing binary (criterion 5).
	seen := map[string]bool{}
	for _, w := range ls.hooks {
		if isManagedCommand(w.Command, managed) {
			continue
		}
		name, state, detail := resolveCommand(w.Command, opts.ProjDir)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		items = append(items, Item{
			Class:       "hooks",
			Name:        name,
			Path:        w.Event + eventMatcherSuffix(w.Matcher),
			Origin:      w.Origin,
			State:       state,
			Detail:      detail,
			OutsideRepo: w.Origin == OriginUser || w.Origin == OriginPlugin,
		})
	}

	// Managed .git/hooks slots — only meaningful inside a git repository.
	if fi, err := os.Stat(filepath.Join(opts.ProjDir, ".git")); err == nil && fi.IsDir() {
		for _, s := range hooks.GitHookStates(opts.ProjDir) {
			it := Item{
				Class:  "hooks",
				Name:   s.Name,
				Path:   filepath.Join(".git", "hooks", s.Name),
				Origin: OriginLocal,
				State:  s.State,
				Detail: s.Detail,
			}
			if s.State != StateOK && s.Detail == "" {
				it.Fix = fixHookInstall
			}
			items = append(items, it)
		}
	}

	return items
}

// managedCapability evaluates one cerne-installed capability: the script's
// drift against the embed AND the wiring in the effective settings, shown on
// the same line. Either half absent makes the capability missing.
func managedCapability(projDir string, a hooks.Asset, ls layerSet) Item {
	script := hooks.ScriptState(projDir, a.Script)
	_, wired := ls.wiringFor(a.Script)

	it := Item{
		Class:  "hooks",
		Name:   a.Script,
		Path:   filepath.Join(".claude", "hooks", a.Script),
		Origin: OriginProject,
	}
	switch {
	case script == hooks.FileOK && wired:
		it.State = StateOK
	case script == hooks.FileDivergent && wired:
		it.State = StateDivergent
		it.Detail = "script differs from the canonical asset of this cerne build"
		it.Fix = fixHookInstall
	case !wired && script != hooks.FileMissing:
		it.State = StateMissing
		it.Detail = "script present but no settings.json layer wires the " + a.Event + " event"
		it.Fix = fixHookInstall
	case wired && script == hooks.FileMissing:
		it.State = StateMissing
		it.Detail = "wired in settings.json but the script is absent on disk"
		it.Fix = fixHookInstall
	default:
		it.State = StateMissing
		it.Detail = "script and wiring both absent"
		it.Fix = fixHookInstall
	}
	return it
}

// isManagedCommand reports whether a wiring command runs one of the managed
// scripts.
func isManagedCommand(cmd string, managed map[string]bool) bool {
	for script := range managed {
		if strings.Contains(cmd, script) {
			return true
		}
	}
	return false
}

// resolveCommand extracts what a hook command executes and checks it is
// runnable. A shell wrapper around a project script is attributed to the
// script file; a bare binary is resolved on PATH.
func resolveCommand(cmd, projDir string) (name, state, detail string) {
	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		return "", "", ""
	}
	exe := strings.Trim(fields[0], `"'`)
	if exe == "bash" || exe == "sh" {
		if len(fields) < 2 {
			return "", "", ""
		}
		target := strings.Trim(fields[1], `"'`)
		target = strings.ReplaceAll(target, "$CLAUDE_PROJECT_DIR", projDir)
		name = filepath.Base(target)
		if _, err := os.Stat(target); err != nil {
			return name, StateBroken, "script " + target + " not found on disk"
		}
		return name, StateConfigured, ""
	}
	name = filepath.Base(exe)
	if strings.ContainsRune(exe, os.PathSeparator) {
		if _, err := os.Stat(exe); err != nil {
			return name, StateBroken, "binary " + exe + " not found"
		}
		return name, StateConfigured, ""
	}
	if _, err := exec.LookPath(exe); err != nil {
		return name, StateBroken, "binary \"" + exe + "\" not resolvable on PATH"
	}
	return name, StateConfigured, ""
}

// eventMatcherSuffix renders the wiring location shown as the item path.
func eventMatcherSuffix(matcher string) string {
	if matcher == "" {
		return ""
	}
	return "/" + matcher
}
