package harness

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/henriquesakata/cerne/internal/hooks"
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
		// A healthy project script already has its own line from the disk
		// scan above — a second "configured" line for the wiring is noise.
		// A wiring pointing at a script that is NOT on disk stays: that is
		// the broken half the screen exists to show.
		if state == StateConfigured && projectScript(w.Command, opts.ProjDir) {
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
	if _, isRepo := hooks.GitHooksDir(opts.ProjDir); isRepo {
		for _, s := range hooks.GitHookStates(opts.ProjDir) {
			it := Item{
				Class:  "hooks",
				Name:   s.Name,
				Path:   s.Path,
				Origin: OriginLocal,
				State:  s.State,
				Detail: s.Detail,
			}
			switch {
			case s.External && filepath.IsAbs(s.Path):
				it.OutsideRepo = true
			case s.External: // the hook manager's file is versioned (F-0360)
				it.Origin = OriginProject
			case s.State != StateOK && s.Detail == "":
				it.Fix = fixHookInstall
			}
			items = append(items, it)
		}
	}

	// Cursor hook wirings from .cursor/hooks.json and ~/.cursor/hooks.json (F-0526).
	items = append(items, cursorHookItems(opts.ProjDir, opts.Home)...)

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
		it.Detail = "script differs from the canonical asset of this cerne build — cerne hook install replaces it and drops local lines; to turn the hook off for a process, set CERNE_NO_AGENT_HOOKS=1" // F-0360
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

// projectScript reports whether a shell-wrapper hook command targets a script
// inside the project tree.
func projectScript(cmd, projDir string) bool {
	fields := strings.Fields(cmd)
	if len(fields) < 2 {
		return false
	}
	exe := strings.Trim(fields[0], `"'`)
	if exe != "bash" && exe != "sh" {
		return false
	}
	target := strings.Trim(fields[1], `"'`)
	target = strings.ReplaceAll(target, "$CLAUDE_PROJECT_DIR", projDir)
	return strings.HasPrefix(target, projDir+string(os.PathSeparator))
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

// cursorHooksEntry is one entry from a .cursor/hooks.json hooks map, with only
// the fields we expose — the rest (env vars, secrets, etc.) are discarded.
type cursorHooksEntry struct {
	Command string `json:"command"`
}

// readCursorHooksLayers reads .cursor/hooks.json from the project and (when
// Home is set) from ~/.cursor/hooks.json. It returns a Layer entry for each
// file that was successfully found (present-only: absent files are not noisy
// missing layers). The callee merges these into ls.layers before Inspect runs.
func readCursorHooksLayers(projDir, home string) []Layer {
	type src struct {
		path string
	}
	var srcs []src
	srcs = append(srcs, src{filepath.Join(projDir, ".cursor", "hooks.json")})
	if home != "" {
		srcs = append(srcs, src{filepath.Join(home, ".cursor", "hooks.json")})
	}
	var out []Layer
	for _, s := range srcs {
		if _, err := os.Stat(s.path); os.IsNotExist(err) {
			continue // absent → no layer (keep Claude Code layer count stable)
		}
		if _, err := os.ReadFile(s.path); err != nil {
			out = append(out, Layer{Path: s.path, Readable: false, Error: err.Error()})
			continue
		}
		out = append(out, Layer{Path: s.path, Readable: true})
	}
	return out
}

// cursorHookItems builds hook Item objects from .cursor/hooks.json files
// (project and user), extracting only {event, command} without exposing other
// fields. Items use OriginCursorProject / OriginCursorUser so consumersFor
// assigns them to ["cursor"].
func cursorHookItems(projDir, home string) []Item {
	type src struct {
		path   string
		origin string
	}
	var srcs []src
	srcs = append(srcs, src{filepath.Join(projDir, ".cursor", "hooks.json"), OriginCursorProject})
	if home != "" {
		srcs = append(srcs, src{filepath.Join(home, ".cursor", "hooks.json"), OriginCursorUser})
	}

	seen := map[string]bool{}
	var items []Item
	for _, s := range srcs {
		raw, err := os.ReadFile(s.path)
		if err != nil {
			continue
		}
		var doc struct {
			Hooks map[string][]cursorHooksEntry `json:"hooks"`
		}
		if json.Unmarshal(raw, &doc) != nil {
			continue
		}
		outside := s.origin == OriginCursorUser
		for event, entries := range doc.Hooks {
			for _, e := range entries {
				if e.Command == "" {
					continue
				}
				// Extract the cerne invocation or the binary name from the command.
				name, state, detail := resolveCursorCommand(e.Command, projDir)
				if name == "" || seen[name+":"+event] {
					continue
				}
				seen[name+":"+event] = true
				items = append(items, Item{
					Class:       "hooks",
					Name:        name,
					Path:        event,
					Origin:      s.origin,
					State:       state,
					Detail:      detail,
					OutsideRepo: outside,
				})
			}
		}
	}
	return items
}

// resolveCursorCommand extracts the name and state of a cursor hook command.
// Cursor's flat commands are fail-open shell wrappers or bare binaries.
// cerne commands are always resolvable (the binary may be managed).
func resolveCursorCommand(cmd, projDir string) (name, state, detail string) {
	// Extract the cerne invocation from a fail-open wrapper.
	const marker = "cerne hook"
	if i := strings.Index(cmd, marker); i >= 0 {
		return "cerne", StateConfigured, ""
	}
	// Fall back to the standard resolver.
	return resolveCommand(cmd, projDir)
}
