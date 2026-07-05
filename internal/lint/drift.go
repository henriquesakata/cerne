package lint

import (
	"fmt"
	"sort"
	"strings"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

// driftMaxPaths caps how many changed paths a drift-suspect Detail lists before
// collapsing the rest into a "+k mais" tail.
const driftMaxPaths = 5

// DriftFindings flags implemented features whose code scope changed since the
// pinned project_head baseline (F-0113) — the shipped spec/result may now be
// stale. It is a pure function of its inputs (no I/O): the caller resolves the
// changed paths (gitback.ChangedPaths) and the baseline (project_head sidecar).
//
// Two additive scope axes map a changed path to features (F-0116): modulePaths
// (module → globs; the feature's Module) and componentPaths (component → globs;
// the feature's Components tags — the axis that works in a moduleless project).
// It is inert — returns nil — when there is nothing to go on: neither map
// configured, no baseline recorded, or no changed paths. A scope is skipped
// when a live feature (candidate/approved/in_development) still carries it,
// because that active work already tracks the change (this also spares the
// feature being built on the current branch). A feature hit by both axes yields
// ONE finding (dedup by entity; modules are walked first). Deterministic:
// scopes and features are walked in sorted order.
func DriftFindings(changed []string, baseline string, modulePaths, componentPaths map[string][]string, features []store.Feature) []Finding {
	if baseline == "" || (len(modulePaths) == 0 && len(componentPaths) == 0) || len(changed) == 0 {
		return nil
	}

	byModule := matchScopes(changed, modulePaths)
	byComponent := matchScopes(changed, componentPaths)
	if len(byModule) == 0 && len(byComponent) == 0 {
		return nil
	}

	// Group features per axis and note which scopes still have live work.
	implementedByModule := make(map[string][]store.Feature)
	moduleHasLive := make(map[string]bool)
	implementedByComponent := make(map[string][]store.Feature)
	componentHasLive := make(map[string]bool)
	for _, f := range features {
		live := liveFeatureStates[f.State]
		if live {
			moduleHasLive[f.Module] = true
		}
		if f.State == domain.StateImplemented {
			implementedByModule[f.Module] = append(implementedByModule[f.Module], f)
		}
		for _, c := range f.Components {
			if live {
				componentHasLive[c] = true
			}
			if f.State == domain.StateImplemented {
				implementedByComponent[c] = append(implementedByComponent[c], f)
			}
		}
	}

	var findings []Finding
	seen := make(map[string]bool)
	emit := func(byScope map[string][]string, hasLive map[string]bool, implemented map[string][]store.Feature) {
		scopes := make([]string, 0, len(byScope))
		for s := range byScope {
			scopes = append(scopes, s)
		}
		sort.Strings(scopes)
		for _, scope := range scopes {
			if hasLive[scope] {
				continue // active work already tracks this scope
			}
			impl := implemented[scope]
			if len(impl) == 0 {
				continue
			}
			paths := byScope[scope]
			sort.Strings(paths)
			detail := driftDetail(baseline, paths)
			sort.Slice(impl, func(i, j int) bool { return impl[i].ID < impl[j].ID })
			for _, f := range impl {
				if seen[f.ID] {
					continue // one finding per entity across axes (F-0116)
				}
				seen[f.ID] = true
				findings = append(findings, Finding{
					Rule:   RuleDriftSuspect,
					Entity: f.ID,
					Detail: detail,
				})
			}
		}
	}
	emit(byModule, moduleHasLive, implementedByModule)
	emit(byComponent, componentHasLive, implementedByComponent)
	return findings
}

// matchScopes attributes each changed path to every scope (module or component)
// whose globs match it.
func matchScopes(changed []string, scopePaths map[string][]string) map[string][]string {
	byScope := make(map[string][]string)
	for _, p := range changed {
		for scope, globs := range scopePaths {
			for _, g := range globs {
				if matchGlob(g, p) {
					byScope[scope] = append(byScope[scope], p)
					break
				}
			}
		}
	}
	return byScope
}

// matchGlob reports whether the slash path matches glob. Supported forms (v1):
// an exact path, or a "dir/**" suffix that matches anything under dir. Broader
// glob semantics (intra-segment '*') are out of scope for F-0113.
func matchGlob(glob, path string) bool {
	if glob == path {
		return true
	}
	if strings.HasSuffix(glob, "/**") {
		prefix := strings.TrimSuffix(glob, "**") // keep the trailing slash: "dir/"
		return strings.HasPrefix(path, prefix)
	}
	return false
}

// driftDetail renders the pt-BR detail: the short baseline sha and up to
// driftMaxPaths changed paths, the remainder collapsed into "(+k mais)".
func driftDetail(baseline string, paths []string) string {
	short := baseline
	if len(short) > 7 {
		short = short[:7]
	}
	shown, extra := paths, 0
	if len(paths) > driftMaxPaths {
		shown, extra = paths[:driftMaxPaths], len(paths)-driftMaxPaths
	}
	s := fmt.Sprintf("código mudou desde %s em: %s", short, strings.Join(shown, ", "))
	if extra > 0 {
		s += fmt.Sprintf(" (+%d mais)", extra)
	}
	return s
}
