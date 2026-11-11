// Package harness inspects the effective agent harness of a cerne project
// (F-0251): the merge of what cerne installs (hooks, skills, .cernerc) with
// what comes from outside the repo (user-level ~/.claude, marketplace plugins,
// MCP servers, PATH binaries, context files). Read-only by construction — it
// reports state, never repairs it.
package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/henriquesakata/cerne/internal/version"
)

// Origins: where an item comes from. 3 of the 5 live outside the repository —
// another machine or another dev does not inherit them on clone.
const (
	OriginProject = "project" // versioned in the repo
	OriginLocal   = "local"   // in the project tree, gitignored
	OriginUser    = "user"    // ~/.claude/**
	OriginPlugin  = "plugin"  // marketplace plugin
	OriginPath    = "path"    // external binary on PATH
)

// States. Managed artifacts (cerne-installed) get ok/divergent/missing;
// third-party artifacts get configured/broken/unmanaged — no canonical to
// diff against means no drift verdict, by design.
const (
	StateOK         = "ok"
	StateDivergent  = "divergent"
	StateMissing    = "missing"
	StateConfigured = "configured"
	StateBroken     = "broken"
	StateUnmanaged  = "unmanaged"
)

// Item is one line of the inventory.
type Item struct {
	Class       string   `json:"class"`
	Name        string   `json:"name"`
	Path        string   `json:"path"`
	Origin      string   `json:"origin"`
	State       string   `json:"state"`
	Detail      string   `json:"detail,omitempty"`
	Fix         string   `json:"fix,omitempty"`
	OutsideRepo bool     `json:"outsideRepo"`
	Consumers   []string `json:"consumers"`
}

// Class groups the inventory items of one section of the screen.
type Class struct {
	Name  string `json:"name"`
	Items []Item `json:"items"`
}

// Layer is one config source consulted; an unreadable layer is reported with
// its reason instead of failing the whole inventory (spec criterion 15).
type Layer struct {
	Path     string `json:"path"`
	Readable bool   `json:"readable"`
	Error    string `json:"error,omitempty"`
}

// Inventory is the full harness report — the /api/harness payload and the
// `cerne harness --json` output.
type Inventory struct {
	Version  string             `json:"version"`
	Classes  []Class            `json:"classes"`
	Layers   []Layer            `json:"layers"`
	Coverage []ProviderCoverage `json:"coverage"`
}

// Options parameterizes an inspection. Home is injectable so tests never read
// the developer's real ~; empty Home skips the user layer.
type Options struct {
	ProjDir string
	RepoDir string // backend clone, for learned-skill reconciliation; may be empty
	Home    string
}

// wiredHook is one hook command found in the effective settings merge.
type wiredHook struct {
	Event   string
	Matcher string
	Command string
	Origin  string
}

// layerSet is the resolved settings merge: every layer consulted, the union
// of hook wirings (Claude Code merges hooks arrays across files) and the
// effective enabledPlugins map (later layers override earlier ones).
type layerSet struct {
	layers  []Layer
	hooks   []wiredHook
	plugins map[string]bool
}

// wiringFor reports the first wiring whose command mentions script.
func (ls layerSet) wiringFor(script string) (wiredHook, bool) {
	for _, h := range ls.hooks {
		if strings.Contains(h.Command, script) {
			return h, true
		}
	}
	return wiredHook{}, false
}

// readLayers reads the three Claude Code settings layers (user, project,
// local). A missing or malformed file becomes an unreadable Layer with the
// reason — never an error.
func readLayers(projDir, home string) layerSet {
	type src struct {
		path   string
		origin string
	}
	var srcs []src
	if home != "" {
		srcs = append(srcs, src{filepath.Join(home, ".claude", "settings.json"), OriginUser})
	}
	srcs = append(srcs,
		src{filepath.Join(projDir, ".claude", "settings.json"), OriginProject},
		src{filepath.Join(projDir, ".claude", "settings.local.json"), OriginLocal},
	)
	ls := layerSet{plugins: map[string]bool{}}
	for _, s := range srcs {
		raw, err := os.ReadFile(s.path)
		if err != nil {
			ls.layers = append(ls.layers, Layer{Path: s.path, Readable: false, Error: readErrReason(err)})
			continue
		}
		hooks, plugins, err := parseSettings(raw, s.origin)
		if err != nil {
			ls.layers = append(ls.layers, Layer{Path: s.path, Readable: false, Error: err.Error()})
			continue
		}
		ls.layers = append(ls.layers, Layer{Path: s.path, Readable: true})
		ls.hooks = append(ls.hooks, hooks...)
		for name, enabled := range plugins {
			ls.plugins[name] = enabled
		}
	}
	return ls
}

func readErrReason(err error) string {
	if os.IsNotExist(err) {
		return "not present"
	}
	return err.Error()
}

// parseSettings extracts every hook wiring {event, matcher, command} and the
// enabledPlugins map from one settings document.
func parseSettings(raw []byte, origin string) ([]wiredHook, map[string]bool, error) {
	var doc struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
		EnabledPlugins map[string]bool `json:"enabledPlugins"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, nil, err
	}
	var out []wiredHook
	for event, entries := range doc.Hooks {
		for _, e := range entries {
			for _, h := range e.Hooks {
				out = append(out, wiredHook{Event: event, Matcher: e.Matcher, Command: h.Command, Origin: origin})
			}
		}
	}
	return out, doc.EnabledPlugins, nil
}

// Inspect builds the harness inventory for one project. Strictly read-only.
func Inspect(opts Options) Inventory {
	ls := readLayers(opts.ProjDir, opts.Home)
	cerne := cerneItems(opts, &ls)
	mcp := mcpItems(opts, &ls)
	plugins := pluginItems(opts, &ls)
	classes := []Class{
		{Name: "cerne", Items: cerne},
		{Name: "hooks", Items: hookItems(opts, ls)},
		{Name: "skills", Items: skillItems(opts, ls)},
		{Name: "plugins", Items: plugins},
		{Name: "mcp", Items: mcp},
		{Name: "config", Items: configItems(opts, ls)},
		{Name: "context", Items: contextItems(opts)},
		{Name: "providers", Items: providerItems(opts)},
	}
	annotateConsumers(classes)
	return Inventory{
		Version:  version.Short(),
		Classes:  classes,
		Layers:   ls.layers,
		Coverage: coverage(opts.ProjDir, classes),
	}
}
