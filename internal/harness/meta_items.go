package harness

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gitlab.com/rockt/cerne/internal/hooks"
	"gitlab.com/rockt/cerne/internal/project"
)

// StateDegraded marks a provider capability the surface cannot host (F-0174)
// — not broken, a documented limitation.
const StateDegraded = "degraded"

// cerneItems builds the cerne class: the tool itself as a harness piece —
// backend remote, docs.lock pin, the .cernerc keys that govern the harness
// (absent keys shown as their defaults), and whether .docs/ is materialized.
func cerneItems(opts Options, ls *layerSet) []Item {
	var items []Item

	rcPath := filepath.Join(opts.ProjDir, ".cernerc")
	if _, err := os.Stat(rcPath); err != nil {
		ls.layers = append(ls.layers, Layer{Path: rcPath, Readable: false, Error: readErrReason(err)})
	} else if cfg, _, err := project.LoadConfig(opts.ProjDir); err != nil {
		ls.layers = append(ls.layers, Layer{Path: rcPath, Readable: false, Error: err.Error()})
	} else {
		ls.layers = append(ls.layers, Layer{Path: rcPath, Readable: true})
		items = append(items, Item{
			Class: "cerne", Name: "backend", Path: ".cernerc", Origin: OriginProject,
			State: StateConfigured, Detail: cfg.Backend,
		})
		items = append(items, cernercKeyItems(cfg)...)
	}

	if lock, err := project.LoadLock(opts.ProjDir); err == nil {
		items = append(items, Item{
			Class: "cerne", Name: "docs.lock", Path: "docs.lock", Origin: OriginProject,
			State: StateConfigured, Detail: "pin: " + lock.Snapshot,
		})
	} else {
		items = append(items, Item{
			Class: "cerne", Name: "docs.lock", Path: "docs.lock", Origin: OriginProject,
			State: StateMissing, Detail: "no committed pin — run a cerne operation on the pin branch",
		})
	}

	docsItem := Item{Class: "cerne", Name: ".docs", Path: ".docs/", Origin: OriginLocal}
	if fi, err := os.Stat(filepath.Join(opts.ProjDir, ".docs", "CONTEXT.md")); err == nil && !fi.IsDir() {
		docsItem.State = StateOK
		docsItem.Detail = "projection materialized"
	} else {
		docsItem.State = StateMissing
		docsItem.Detail = "projection not materialized"
		docsItem.Fix = "cerne pull"
	}
	items = append(items, docsItem)

	return items
}

// cernercKeyItems renders the effective value of each harness-governing
// .cernerc key; a key left unset shows its default, marked as such.
func cernercKeyItems(cfg project.Config) []Item {
	boolVal := func(set bool, v bool) (string, bool) {
		return fmt.Sprintf("%v", v), set
	}
	type key struct {
		name  string
		value string
		isSet bool
	}
	keys := []key{
		{"gates.approve", cfg.Gates.ApproveGate(), cfg.Gates.Approve != ""},
		{"gates.accept", cfg.Gates.AcceptGate(), cfg.Gates.Accept != ""},
		{"work_gate", fmt.Sprintf("%v", cfg.Gates.WorkGateEnabled()), cfg.Gates.WorkGate != ""},
		{"commit_trailer", fmt.Sprintf("%v", cfg.Gates.CommitTrailerEnabled()), cfg.Gates.CommitTrailer != ""},
	}
	v, set := boolVal(cfg.CompileOnPush, cfg.CompileOnPush)
	keys = append(keys, key{"compile_on_push", v, set})
	keys = append(keys, key{"pin_branch", orDefault(cfg.PinBranch, "(unpinned)"), cfg.PinBranch != ""})
	v, set = boolVal(cfg.SyncSkills, cfg.SyncSkills)
	keys = append(keys, key{"sync_skills", v, set})
	v, set = boolVal(cfg.OKF, cfg.OKF)
	keys = append(keys, key{"okf", v, set})

	items := make([]Item, 0, len(keys))
	for _, k := range keys {
		detail := k.value
		if !k.isSet {
			detail = "default: " + k.value
		}
		items = append(items, Item{
			Class: "cerne", Name: k.name, Path: ".cernerc", Origin: OriginProject,
			State: StateConfigured, Detail: detail,
		})
	}
	return items
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// contextItems lists the agent context files: present without a canonical to
// compare against, so always unmanaged — presence and origin only.
func contextItems(opts Options) []Item {
	var items []Item
	for _, name := range []string{"CLAUDE.md", "AGENTS.md"} {
		if _, err := os.Stat(filepath.Join(opts.ProjDir, name)); err == nil {
			items = append(items, Item{
				Class: "context", Name: name, Path: name,
				Origin: OriginProject, State: StateUnmanaged,
			})
		}
	}
	if opts.Home != "" {
		p := filepath.Join(opts.Home, ".claude", "CLAUDE.md")
		if _, err := os.Stat(p); err == nil {
			items = append(items, Item{
				Class: "context", Name: "~/.claude/CLAUDE.md", Path: p,
				Origin: OriginUser, State: StateUnmanaged, OutsideRepo: true,
			})
		}
	}
	return items
}

// pluginItems lists the marketplace plugins the effective settings enable:
// version from the local registry when installed, broken when the registry
// has no trace of an enabled plugin. Disabled installs are not "used" and
// stay out of the inventory.
func pluginItems(opts Options, ls *layerSet) []Item {
	if opts.Home == "" {
		return nil
	}
	installed := map[string]string{} // name@marketplace -> version
	regPath := filepath.Join(opts.Home, ".claude", "plugins", "installed_plugins.json")
	if raw, err := os.ReadFile(regPath); err == nil {
		var doc struct {
			Plugins map[string][]struct {
				Version string `json:"version"`
			} `json:"plugins"`
		}
		if err := json.Unmarshal(raw, &doc); err != nil {
			ls.layers = append(ls.layers, Layer{Path: regPath, Readable: false, Error: err.Error()})
		} else {
			ls.layers = append(ls.layers, Layer{Path: regPath, Readable: true})
			for name, entries := range doc.Plugins {
				v := ""
				if len(entries) > 0 {
					v = entries[0].Version
				}
				installed[name] = v
			}
		}
	}

	names := make([]string, 0, len(ls.plugins))
	for name, enabled := range ls.plugins {
		if enabled {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	items := make([]Item, 0, len(names))
	for _, name := range names {
		it := Item{
			Class: "plugins", Name: name, Path: regPath,
			Origin: OriginPlugin, OutsideRepo: true,
		}
		if v, ok := installed[name]; ok {
			it.State = StateConfigured
			it.Detail = "version " + orDefault(v, "unknown")
		} else {
			it.State = StateBroken
			it.Detail = "enabled in settings but not in the installed-plugins registry"
		}
		items = append(items, it)
	}
	return items
}

// providerItems renders the F-0174 parity matrix for each provider whose
// config directory exists in the project — the same table the installer uses.
func providerItems(opts Options) []Item {
	var items []Item
	for _, p := range hooks.DetectProviders(opts.ProjDir) {
		for _, row := range hooks.ParityRows() {
			it := Item{
				Class:  "providers",
				Name:   string(p) + ": " + row.Capability,
				Path:   "." + string(p) + "/",
				Origin: OriginProject,
			}
			if deg, reason := row.Degraded(p); deg {
				it.State = StateDegraded
				it.Detail = reason
			} else {
				it.State = StateConfigured
				it.Detail = row.For(p)
			}
			items = append(items, it)
		}
	}
	return items
}

// configItems lists the settings layers as inventory lines, so the screen's
// Configuração section shows where each layer lives and whether it loaded.
func configItems(opts Options, ls layerSet) []Item {
	items := make([]Item, 0, len(ls.layers))
	for _, l := range ls.layers {
		name := filepath.Base(l.Path)
		origin := OriginProject
		outside := false
		// Project prefix wins over home prefix: a project checked out under
		// the user's home is still the project.
		inProject := strings.HasPrefix(l.Path, opts.ProjDir+string(filepath.Separator))
		switch {
		case inProject && strings.HasSuffix(name, ".local.json"):
			origin = OriginLocal
		case inProject:
			origin = OriginProject
		case opts.Home != "" && strings.HasPrefix(l.Path, opts.Home+string(filepath.Separator)):
			origin = OriginUser
			outside = true
		}
		it := Item{
			Class: "config", Name: name, Path: l.Path,
			Origin: origin, State: StateConfigured, OutsideRepo: outside,
		}
		if !l.Readable {
			if l.Error == "not present" {
				continue // an absent optional layer is not an inventory line
			}
			it.State = StateBroken
			it.Detail = l.Error
		}
		items = append(items, it)
	}
	return items
}
