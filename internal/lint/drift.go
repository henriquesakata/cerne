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

// DriftFindings flags implemented features whose module's code changed since the
// pinned project_head baseline (F-0113) — the shipped spec/result may now be
// stale. It is a pure function of its inputs (no I/O): the caller resolves the
// changed paths (gitback.ChangedPaths) and the baseline (project_head sidecar).
//
// It is inert — returns nil — when there is nothing to go on: no module→paths
// map configured, no baseline recorded, or no changed paths. A changed path is
// attributed to every module whose globs match it; a module is skipped when it
// still has a live feature (candidate/approved/in_development), because that
// active work already tracks the change (this also spares the feature being
// built on the current branch). Deterministic: modules and features are walked
// in sorted order.
func DriftFindings(changed []string, baseline string, modulePaths map[string][]string, features []store.Feature) []Finding {
	if baseline == "" || len(modulePaths) == 0 || len(changed) == 0 {
		return nil
	}

	// Attribute each changed path to the modules whose globs match it.
	byModule := make(map[string][]string)
	for _, p := range changed {
		for module, globs := range modulePaths {
			for _, g := range globs {
				if matchGlob(g, p) {
					byModule[module] = append(byModule[module], p)
					break
				}
			}
		}
	}
	if len(byModule) == 0 {
		return nil
	}

	// Group features by module and note which modules have live work.
	implementedByModule := make(map[string][]store.Feature)
	moduleHasLive := make(map[string]bool)
	for _, f := range features {
		if liveFeatureStates[f.State] {
			moduleHasLive[f.Module] = true
		}
		if f.State == domain.StateImplemented {
			implementedByModule[f.Module] = append(implementedByModule[f.Module], f)
		}
	}

	modules := make([]string, 0, len(byModule))
	for m := range byModule {
		modules = append(modules, m)
	}
	sort.Strings(modules)

	var findings []Finding
	for _, module := range modules {
		if moduleHasLive[module] {
			continue // active work already tracks this scope
		}
		impl := implementedByModule[module]
		if len(impl) == 0 {
			continue
		}
		paths := byModule[module]
		sort.Strings(paths)
		detail := driftDetail(baseline, paths)
		sort.Slice(impl, func(i, j int) bool { return impl[i].ID < impl[j].ID })
		for _, f := range impl {
			findings = append(findings, Finding{
				Rule:   RuleDriftSuspect,
				Entity: f.ID,
				Detail: detail,
			})
		}
	}
	return findings
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
