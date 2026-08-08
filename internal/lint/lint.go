// Package lint detects stale documentation in a cerne backend. Lifecycle
// state is data, so every rule is deterministic — no LLM involved.
//
// Only content documents of LIVE entities are scanned by the text rules:
// features in candidate/approved/in_development, active decisions and
// current references. Content of superseded/rejected/obsolete entities is
// history and is left alone.
package lint

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"gitlab.com/rockt/cerne/internal/dectmpl"
	"gitlab.com/rockt/cerne/internal/doccheck"
	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/mdlink"
	"gitlab.com/rockt/cerne/internal/plantmpl"
	"gitlab.com/rockt/cerne/internal/restmpl"
	"gitlab.com/rockt/cerne/internal/runtmpl"
	"gitlab.com/rockt/cerne/internal/spectmpl"
	"gitlab.com/rockt/cerne/internal/store"
	"gitlab.com/rockt/cerne/internal/wiki"
)

// Rule names, in deterministic report order.
const (
	RuleRefToSuperseded    = "ref-to-superseded"
	RuleBrokenLink         = "broken-link"
	RuleOrphanContent      = "orphan-content"
	RuleMissingGateDoc     = "missing-gate-doc"
	RuleWikiSourceUnknown  = "wiki-source-unknown"
	RuleParentNotFound     = "parent-not-found"
	RuleParentTerminal     = "parent-terminal"
	RuleEpicOpenChildren   = "epic-open-children"
	RuleEpicBehindChildren = "epic-behind-children"
	RuleEpicUnplanned      = "epic-unplanned"
	RuleStaleCandidate     = "stale-candidate"
	RuleStaleReady         = "stale-ready"
	RuleStaleWork          = "stale-work"
	RuleStaleNote          = "stale-note"
	RuleIncompleteDoc      = "incomplete-doc"
	RuleUntaggedLive       = "untagged-live"
	RuleUnregisteredSource = "unregistered-source"
	RuleDriftSuspect       = "drift-suspect"
	RuleOpenClarification  = "open-clarification"
	RuleCapabilityOrphan   = "capability-orphan"
)

// RuleOrder is the deterministic report order of the lint rules.
var RuleOrder = []string{RuleRefToSuperseded, RuleBrokenLink, RuleOrphanContent, RuleMissingGateDoc, RuleWikiSourceUnknown, RuleParentNotFound, RuleParentTerminal, RuleEpicOpenChildren, RuleEpicBehindChildren, RuleEpicUnplanned, RuleStaleCandidate, RuleStaleReady, RuleStaleWork, RuleStaleNote, RuleIncompleteDoc, RuleUntaggedLive, RuleUnregisteredSource, RuleDriftSuspect, RuleOpenClarification, RuleCapabilityOrphan}

var ruleRank = map[string]int{
	RuleRefToSuperseded:    0,
	RuleBrokenLink:         1,
	RuleOrphanContent:      2,
	RuleMissingGateDoc:     3,
	RuleWikiSourceUnknown:  4,
	RuleParentNotFound:     5,
	RuleParentTerminal:     6,
	RuleEpicOpenChildren:   7,
	RuleEpicBehindChildren: 8,
	RuleEpicUnplanned:      9,
	RuleStaleCandidate:     10,
	RuleStaleReady:         11,
	RuleStaleWork:          12,
	RuleStaleNote:          13,
	RuleIncompleteDoc:      14,
	RuleUntaggedLive:       15,
	RuleUnregisteredSource: 16,
	RuleDriftSuspect:       17,
	RuleOpenClarification:  18,
	RuleCapabilityOrphan:   19,
}

// Finding is one lint problem detected in the backend.
type Finding struct {
	Rule   string // one of the Rule* constants
	Entity string // entity ID the finding is about ("" for orphan files)
	File   string // rel slash path of the offending file ("" when entity-level)
	Detail string // human-readable pt-BR detail
}

// minTitleNeedleLen guards against noisy title matches (e.g. "One").
const minTitleNeedleLen = 4

// liveFeatureStates are the feature states whose content docs are scanned
// by the text rules. Implemented is settled history for scanning purposes,
// though it still has gate-doc invariants (rule missing-gate-doc).
var liveFeatureStates = map[domain.FeatureState]bool{
	domain.StateCandidate:     true,
	domain.StateApproved:      true,
	domain.StateInDevelopment: true,
}

// needle is one terminal entity scanned docs must not mention. Matching is a
// case-insensitive word-boundary substring on the ID, for features with a
// title of at least minTitleNeedleLen runes on the title, and on the original
// identifiers derived from the imported_from frontmatter of the entity's
// content doc. One finding per (doc, needle) pair: every hit of the same
// entity collapses into a single finding keeping the strongest detail —
// ID/title hits share the ID-based detail; imported hits fall back to the
// first matching original identifier.
type needle struct {
	id, title, state, use string
	rxID, rxTitle         *regexp.Regexp
	imported              []importedNeedle
}

// importedNeedle is one original identifier (pre-import) of a terminal
// entity, e.g. "case 0090" or "adr-045".
type importedNeedle struct {
	text string
	rx   *regexp.Regexp
}

// scannedDoc is one authored content document subject to the text rules: entity
// is the owning id and rel its backend path. The generated wiki is not scanned
// by these rules (F-0090) — wikiSourceFindings validates its provenance instead.
type scannedDoc struct {
	entity string
	rel    string // rel slash path within the backend
}

// Run lints the backend at repoDir and returns the findings in
// deterministic order: by Rule (RuleOrder), then File, then Detail
// (Entity as final tie-breaker). staleReadyDays is the project's
// stale_ready_days window (F-0131); 0 falls back to the 14-day default
// (mirroring project.Config.StaleReadyWindowDays, which the caller already
// applies) — pass staleReadyDays via RunConfig for callers that need a
// non-default window, or use Run for the common case with the default.
func Run(repoDir string, ignore ...string) ([]Finding, error) {
	return RunConfig(repoDir, 0, 0, ignore...)
}

// RunConfig is Run with explicit staleReadyDays (0 = default 14 days) and
// staleWorkHours (0 = default 8 hours, F-0130) windows. Kept separate from Run
// so every existing call site (CLI, server) keeps compiling unchanged;
// callers that read stale_ready_days/stale_work_hours from .cernerc pass them
// here instead.
func RunConfig(repoDir string, staleReadyDays, staleWorkHours int, ignore ...string) ([]Finding, error) {
	features, err := store.LoadAllFeatures(repoDir)
	if err != nil {
		return nil, err
	}
	decisions, err := store.LoadAllDecisions(repoDir)
	if err != nil {
		return nil, err
	}
	references, err := store.LoadAllReferences(repoDir)
	if err != nil {
		return nil, err
	}
	runbooks, err := store.LoadAllRunbooks(repoDir)
	if err != nil {
		return nil, err
	}
	notes, err := store.LoadAllNotes(repoDir)
	if err != nil {
		return nil, err
	}
	historicals, err := store.LoadAllHistoricals(repoDir)
	if err != nil {
		return nil, err
	}
	works, err := store.LoadAllWorks(repoDir)
	if err != nil {
		return nil, err
	}

	needles, err := buildNeedles(repoDir, features, decisions, references, runbooks, notes, historicals)
	if err != nil {
		return nil, err
	}
	docs, err := scannedDocs(repoDir, features, decisions, references, runbooks, notes)
	if err != nil {
		return nil, err
	}

	// The text rules (ref-to-superseded, broken-link) are authored-content rules
	// and run only over authored docs — the generated wiki regenerates from
	// source and is validated separately by wikiSourceFindings (F-0090).
	ids := knownEntityIDs(features, decisions, references, runbooks, notes, historicals)
	var findings []Finding
	for _, d := range docs {
		data, err := os.ReadFile(filepath.Join(repoDir, filepath.FromSlash(d.rel)))
		if err != nil {
			return nil, err
		}
		text := string(data)
		findings = append(findings, refFindings(d, text, needles)...)
		findings = append(findings, linkFindings(repoDir, d, text)...)
		findings = append(findings, wikiLinkFindings(d, text, ids)...)
		findings = append(findings, clarificationFindings(d, text)...)
		// unregistered-source is provenance of a decision — scoped to decisions and
		// feature spec/plan/result only (F-0107), never runbooks/references/notes/errata.
		if inUnregisteredSourceScope(d.rel) {
			findings = append(findings, unregisteredSourceFindings(d, text, references, ignore)...)
		}
	}

	orphans, err := orphanFindings(repoDir, features, decisions, references, runbooks, notes)
	if err != nil {
		return nil, err
	}
	findings = append(findings, orphans...)

	gates, err := gateFindings(repoDir, features)
	if err != nil {
		return nil, err
	}
	findings = append(findings, gates...)

	wikiSources, err := wikiSourceFindings(repoDir, features, decisions, references, runbooks, notes)
	if err != nil {
		return nil, err
	}
	findings = append(findings, wikiSources...)

	findings = append(findings, parentFindings(features)...)

	unplanned, err := epicUnplannedFindings(repoDir, features)
	if err != nil {
		return nil, err
	}
	findings = append(findings, unplanned...)

	stale, err := staleCandidateFindings(repoDir, features, time.Now())
	if err != nil {
		return nil, err
	}
	findings = append(findings, stale...)

	findings = append(findings, staleNoteFindings(features, decisions, notes)...)

	findings = append(findings, staleReadyFindings(features, staleReadyDays, time.Now())...)

	findings = append(findings, staleWorkFindings(works, staleWorkHours, time.Now())...)

	incomplete, err := incompleteDocFindings(repoDir, features, decisions, runbooks)
	if err != nil {
		return nil, err
	}
	findings = append(findings, incomplete...)

	findings = append(findings, untaggedLiveFindings(features, decisions, references, notes, runbooks)...)

	capabilities, err := store.LoadAllCapabilities(repoDir)
	if err != nil {
		return nil, err
	}
	findings = append(findings, capabilityOrphanFindings(features, capabilities)...)

	Sort(findings)
	return findings, nil
}

// capabilityOrphanFindings (F-0188): once the project curates a capability
// taxonomy (≥1 active node), a live feature not linked to any capability is
// invisible on the "by capability" mindmap — advisory to keep curation going.
// A project with no capabilities never opted in, so the rule stays silent.
func capabilityOrphanFindings(features []store.Feature, capabilities []store.Capability) []Finding {
	hasActive := false
	for _, c := range capabilities {
		if c.State == domain.CapabilityActive {
			hasActive = true
			break
		}
	}
	if !hasActive {
		return nil
	}
	var out []Finding
	for _, f := range features {
		live := f.State == domain.StateCandidate || f.State == domain.StateApproved || f.State == domain.StateInDevelopment
		if !live || len(f.Capabilities) > 0 {
			continue
		}
		out = append(out, Finding{
			Rule:   RuleCapabilityOrphan,
			Entity: f.ID,
			Detail: fmt.Sprintf("feature viva sem capacidade (%s) — vincule com 'cerne feature capability %s C-…'", f.Title, f.ID),
		})
	}
	return out
}

// Sort orders findings canonically in place: by Rule (RuleOrder), then File,
// then Detail, with Entity as the final tie-breaker. Exported so a caller that
// merges findings from Run with out-of-band findings (e.g. the drift-suspect
// rule, which needs the user's project repo Run never sees) can re-establish
// the deterministic order over the combined slice.
func Sort(findings []Finding) {
	sort.Slice(findings, func(i, j int) bool {
		a, b := findings[i], findings[j]
		if ruleRank[a.Rule] != ruleRank[b.Rule] {
			return ruleRank[a.Rule] < ruleRank[b.Rule]
		}
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Detail != b.Detail {
			return a.Detail < b.Detail
		}
		return a.Entity < b.Entity
	})
}

// incompleteDocFindings flags a real (non-stub) lifecycle doc of a live entity
// missing one or more canonical template sections — making the templates
// (spec/plan/result/ADR/runbook) enforced, not merely offered. Scope mirrors the
// text-scan rules: only live work is checked (features in
// candidate/approved/in_development, active decisions/runbooks). Imported docs
// (imported_from frontmatter) are grandfathered; stub docs are the gates' job;
// implemented features and superseded entities are settled history. Deterministic:
// it compares the doc's level-2 headings against the canonical *tmpl.Sections.
func incompleteDocFindings(repoDir string, features []store.Feature, decisions []store.Decision, runbooks []store.Runbook) ([]Finding, error) {
	var findings []Finding
	check := func(rel string, sections []string) error {
		data, err := os.ReadFile(filepath.Join(repoDir, filepath.FromSlash(rel)))
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if doccheck.FrontmatterValue(data, "imported_from") != "" {
			return nil // legacy imported doc — predates the templates
		}
		if !doccheck.HasRealContent(data) {
			return nil // still a stub — enforced by the lifecycle gates
		}
		if missing := missingSections(data, sections); len(missing) > 0 {
			findings = append(findings, Finding{
				Rule:   RuleIncompleteDoc,
				File:   rel,
				Detail: rel + ": missing template sections: " + strings.Join(missing, ", "),
			})
		}
		return nil
	}
	for _, f := range features {
		if !liveFeatureStates[f.State] {
			continue // only live work; implemented/terminal docs are settled history
		}
		if err := check("content/features/"+f.ID+"/spec.md", spectmpl.Sections); err != nil {
			return nil, err
		}
		if err := check("content/features/"+f.ID+"/plan.md", plantmpl.Sections); err != nil {
			return nil, err
		}
		if err := check("content/features/"+f.ID+"/result.md", restmpl.Sections); err != nil {
			return nil, err
		}
	}
	for _, d := range decisions {
		if d.State != domain.DecisionActive {
			continue
		}
		if err := check("content/decisions/"+d.ID+".md", dectmpl.Sections); err != nil {
			return nil, err
		}
	}
	for _, rb := range runbooks {
		if rb.State != domain.RunbookActive {
			continue
		}
		if err := check("content/runbooks/"+rb.ID+".md", runtmpl.Sections); err != nil {
			return nil, err
		}
	}
	return findings, nil
}

// missingSections returns the canonical sections absent from data as level-2
// (## ) headings, in canonical order. A heading present with "N/A" content still
// counts as present (right-sizing): only the heading is checked, not its body.
func missingSections(data []byte, sections []string) []string {
	present := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		if h, ok := strings.CutPrefix(strings.TrimSpace(line), "## "); ok {
			present[strings.TrimSpace(h)] = true
		}
	}
	var missing []string
	for _, s := range sections {
		if !present[s] {
			missing = append(missing, s)
		}
	}
	return missing
}

// parentFindings checks the epic (parent/child) invariants over the features,
// independent of any content doc:
//   - parent-not-found: a feature points at a parent that does not exist.
//   - parent-terminal: a live feature sits under an epic that is terminal
//     (superseded/rejected) — the children were orphaned.
//   - epic-open-children: an implemented or done feature still has open
//     (non-terminal) children — the epic was marked done but its decomposition
//     is not. done counts the same as implemented (F-0131): it is the
//     human-accepted successor, strictly further along, never less concluded.
//   - epic-behind-children: a not-yet-started epic (candidate/approved) already
//     has a child in development or beyond (implemented or done) — work began
//     but the epic was never planned/started. The forward mirror of
//     epic-open-children; the two are disjoint by epic state.
func parentFindings(features []store.Feature) []Finding {
	byID := make(map[string]store.Feature, len(features))
	for _, f := range features {
		byID[f.ID] = f
	}
	isLive := func(s domain.FeatureState) bool {
		return s == domain.StateCandidate || s == domain.StateApproved || s == domain.StateInDevelopment
	}
	isTerminal := func(s domain.FeatureState) bool {
		return s == domain.StateSuperseded || s == domain.StateRejected
	}

	var findings []Finding
	for _, f := range features {
		if f.Parent == "" {
			continue
		}
		parent, ok := byID[f.Parent]
		if !ok {
			findings = append(findings, Finding{
				Rule:   RuleParentNotFound,
				Entity: f.ID,
				Detail: fmt.Sprintf("%s points to epic %s, which does not exist", f.ID, f.Parent),
			})
			continue
		}
		if isLive(f.State) && isTerminal(parent.State) {
			findings = append(findings, Finding{
				Rule:   RuleParentTerminal,
				Entity: f.ID,
				Detail: fmt.Sprintf("%s is under epic %s, which is %s", f.ID, parent.ID, parent.State),
			})
		}
	}

	for epicID, children := range store.ChildrenByParent(features) {
		epic, ok := byID[epicID]
		if !ok {
			continue
		}
		switch epic.State {
		case domain.StateImplemented, domain.StateDone:
			open := 0
			for _, c := range children {
				if isLive(c.State) {
					open++
				}
			}
			if open > 0 {
				findings = append(findings, Finding{
					Rule:   RuleEpicOpenChildren,
					Entity: epicID,
					Detail: fmt.Sprintf("epic %s is %s, but has %d open child(ren)", epicID, epic.State, open),
				})
			}
		case domain.StateCandidate, domain.StateApproved:
			started := 0
			for _, c := range children {
				if c.State == domain.StateInDevelopment || c.State == domain.StateImplemented || c.State == domain.StateDone {
					started++
				}
			}
			if started > 0 {
				findings = append(findings, Finding{
					Rule:   RuleEpicBehindChildren,
					Entity: epicID,
					Detail: fmt.Sprintf("epic %s is %s, but %d child(ren) already entered development", epicID, epic.State, started),
				})
			}
		}
	}
	return findings
}

// epicUnplannedFindings (F-0235) flags a live epic (candidate/approved/
// in_development feature with at least one live child) whose plan.md is still
// the unfilled scaffold — the mechanical backstop for N-0031 ("épico nasce
// decomposto E planejado"). Unlike epic-behind-children, it does not require a
// child to have started: a candidate epic decomposed but never planned is
// already a finding. The two rules are not mutually exclusive by design (a
// stub-planned epic with a child already in development gets both) — they
// flag distinct, non-overlapping conditions.
func epicUnplannedFindings(repoDir string, features []store.Feature) ([]Finding, error) {
	byID := make(map[string]store.Feature, len(features))
	for _, f := range features {
		byID[f.ID] = f
	}
	isLive := func(s domain.FeatureState) bool {
		return s == domain.StateCandidate || s == domain.StateApproved || s == domain.StateInDevelopment
	}
	var findings []Finding
	for epicID, children := range store.ChildrenByParent(features) {
		epic, ok := byID[epicID]
		if !ok || !isLive(epic.State) {
			continue
		}
		hasLiveChild := false
		for _, c := range children {
			if isLive(c.State) {
				hasLiveChild = true
				break
			}
		}
		if !hasLiveChild {
			continue
		}
		rel := "content/features/" + epicID + "/plan.md"
		data, err := os.ReadFile(filepath.Join(repoDir, filepath.FromSlash(rel)))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		if err == nil && doccheck.HasRealContent(data) {
			continue // a real plan already exists
		}
		findings = append(findings, Finding{
			Rule:   RuleEpicUnplanned,
			Entity: epicID,
			Detail: fmt.Sprintf("epic %s has live children but no real plan.md yet — fill it before approve (see N-0031)", epicID),
		})
	}
	return findings, nil
}

// buildNeedles collects every terminal entity scanned docs must not mention:
// superseded/rejected features, superseded decisions, obsolete references and
// historical entities. Each needle also carries the original identifiers
// derived from the imported_from frontmatter of the entity's content doc (the
// spec for features), when present; for historical entities the declared
// aliases play that role.
func buildNeedles(repoDir string, features []store.Feature, decisions []store.Decision, references []store.Reference, runbooks []store.Runbook, notes []store.Note, historicals []store.Historical) ([]needle, error) {
	var needles []needle
	for _, f := range features {
		if f.State != domain.StateSuperseded && f.State != domain.StateRejected {
			continue
		}
		n := needle{id: f.ID, title: f.Title, state: string(f.State), use: f.SupersededBy, rxID: wordRx(f.ID)}
		if utf8.RuneCountInString(f.Title) >= minTitleNeedleLen {
			n.rxTitle = wordRx(f.Title)
		}
		var err error
		if n.imported, err = importedNeedles(repoDir, "content/features/"+f.ID+"/spec.md"); err != nil {
			return nil, err
		}
		needles = append(needles, n)
	}
	for _, d := range decisions {
		if d.State != domain.DecisionSuperseded {
			continue
		}
		n := needle{id: d.ID, title: d.Title, state: string(d.State), use: d.SupersededBy, rxID: wordRx(d.ID)}
		var err error
		if n.imported, err = importedNeedles(repoDir, "content/decisions/"+d.ID+".md"); err != nil {
			return nil, err
		}
		needles = append(needles, n)
	}
	for _, r := range references {
		if r.State != domain.ReferenceObsolete {
			continue
		}
		n := needle{id: r.ID, title: r.Title, state: string(r.State), rxID: wordRx(r.ID)}
		var err error
		if n.imported, err = importedNeedles(repoDir, "content/references/"+r.ID+".md"); err != nil {
			return nil, err
		}
		needles = append(needles, n)
	}
	for _, rb := range runbooks {
		if rb.State != domain.RunbookSuperseded {
			continue
		}
		n := needle{id: rb.ID, title: rb.Title, state: string(rb.State), use: rb.SupersededBy, rxID: wordRx(rb.ID)}
		var err error
		if n.imported, err = importedNeedles(repoDir, "content/runbooks/"+rb.ID+".md"); err != nil {
			return nil, err
		}
		needles = append(needles, n)
	}
	for _, nt := range notes {
		if nt.State != domain.NoteSuperseded {
			continue
		}
		n := needle{id: nt.ID, title: nt.Title, state: string(nt.State), use: nt.SupersededBy, rxID: wordRx(nt.ID)}
		if utf8.RuneCountInString(nt.Title) >= minTitleNeedleLen {
			n.rxTitle = wordRx(nt.Title)
		}
		var err error
		if n.imported, err = importedNeedles(repoDir, "content/notes/"+nt.ID+".md"); err != nil {
			return nil, err
		}
		needles = append(needles, n)
	}
	for _, h := range historicals {
		// A historical entity is born terminal; no content doc, so its
		// aliases (validated >= MinAliasLen at creation) are the imported-style
		// identifiers live docs must not mention. The Name is a generic domain
		// word (e.g. "Bills"/"Investments") — kept only as the finding label,
		// never a needle, or every prose use of the word false-flags (F-0092).
		n := needle{id: h.ID, title: h.Name, state: string(h.State), use: h.SupersededBy, rxID: wordRx(h.ID)}
		for _, alias := range h.Aliases {
			if utf8.RuneCountInString(alias) < minTitleNeedleLen {
				continue
			}
			n.imported = append(n.imported, importedNeedle{text: alias, rx: wordRx(alias)})
		}
		needles = append(needles, n)
	}
	return needles, nil
}

// importedNeedles reads the imported_from frontmatter of the terminal
// entity's content doc at rel and compiles the derived original identifiers;
// nil when the doc, the frontmatter or the key is absent.
func importedNeedles(repoDir, rel string) ([]importedNeedle, error) {
	data, err := os.ReadFile(filepath.Join(repoDir, filepath.FromSlash(rel)))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	source := doccheck.FrontmatterValue(data, "imported_from")
	if source == "" {
		return nil, nil
	}
	var ins []importedNeedle
	for _, text := range deriveImportedNeedles(source) {
		if utf8.RuneCountInString(text) < minTitleNeedleLen {
			continue
		}
		ins = append(ins, importedNeedle{text: text, rx: wordRx(text)})
	}
	return ins, nil
}

var (
	// reImportedCaseDir matches the folder name of an imported case dir
	// (NNNN_kebab-name), capturing the leading number.
	reImportedCaseDir = regexp.MustCompile(`^(\d+)_.+$`)
	// reImportedADR matches an imported ADR filename without extension,
	// capturing the short form prefix through the number ("adr-045").
	reImportedADR = regexp.MustCompile(`^(adr-\d+)-.+$`)
)

// deriveImportedNeedles maps an imported_from source path to the original
// identifiers other docs may use to mention it:
//   - case dir ".../0090_plugin-autonomous-agent/" → the folder name and
//     "case 0090";
//   - ADR file ".../adr-045-xyz.md" → the bare filename without extension
//     and "adr-045";
//   - anything else → the filename without extension.
func deriveImportedNeedles(source string) []string {
	base := path.Base(strings.TrimSuffix(source, "/"))
	if strings.HasSuffix(source, "/") {
		if m := reImportedCaseDir.FindStringSubmatch(base); m != nil {
			return []string{base, "case " + m[1]}
		}
		return []string{base}
	}
	name := strings.TrimSuffix(base, path.Ext(base))
	if m := reImportedADR.FindStringSubmatch(name); m != nil {
		return []string{name, m[1]}
	}
	return []string{name}
}

// wordRx compiles a case-insensitive word-boundary matcher for the literal s.
func wordRx(s string) *regexp.Regexp {
	return regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(s) + `\b`)
}

// wikiPagePaths returns the rel slash paths of every compiled wiki page under
// content/wiki/; empty when the directory is absent.
func wikiPagePaths(repoDir string) ([]string, error) {
	root := filepath.Join(repoDir, "content", "wiki")
	var rels []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}
		rel, err := filepath.Rel(repoDir, p)
		if err != nil {
			return err
		}
		rels = append(rels, filepath.ToSlash(rel))
		return nil
	})
	return rels, err
}

// wikiSourceFindings reports wiki pages whose provenance lists a source that
// resolves to no existing entity (a dangling synthesis input).
func wikiSourceFindings(repoDir string, features []store.Feature, decisions []store.Decision, references []store.Reference, runbooks []store.Runbook, notes []store.Note) ([]Finding, error) {
	known := make(map[string]bool, len(features)+len(decisions)+len(references)+len(runbooks)+len(notes))
	for _, f := range features {
		known[f.ID] = true
	}
	for _, d := range decisions {
		known[d.ID] = true
	}
	for _, r := range references {
		known[r.ID] = true
	}
	// Runbooks (F-0040) and notes (F-0044) are first-class entities that can
	// legitimately source a wiki synthesis, so they count as known too (F-0091).
	for _, rb := range runbooks {
		known[rb.ID] = true
	}
	for _, n := range notes {
		known[n.ID] = true
	}
	rels, err := wikiPagePaths(repoDir)
	if err != nil {
		return nil, err
	}
	var findings []Finding
	for _, rel := range rels {
		data, err := os.ReadFile(filepath.Join(repoDir, filepath.FromSlash(rel)))
		if err != nil {
			return nil, err
		}
		for _, src := range wiki.ParseProvenance(data).Sources {
			if !known[src] {
				findings = append(findings, Finding{Rule: RuleWikiSourceUnknown, File: rel, Detail: "unknown source: " + src})
			}
		}
	}
	return findings, nil
}

// scannedDocs lists the content documents of live entities: every *.md under
// content/features/<ID>/ for candidate/approved/in_development features,
// plus content/decisions/<ID>.md for active decisions and
// content/references/<ID>.md for current references, when present.
func scannedDocs(repoDir string, features []store.Feature, decisions []store.Decision, references []store.Reference, runbooks []store.Runbook, notes []store.Note) ([]scannedDoc, error) {
	var docs []scannedDoc
	for _, f := range features {
		if !liveFeatureStates[f.State] {
			continue
		}
		entries, err := os.ReadDir(filepath.Join(repoDir, "content", "features", f.ID))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			docs = append(docs, scannedDoc{entity: f.ID, rel: "content/features/" + f.ID + "/" + e.Name()})
		}
	}
	appendIfPresent := func(entity, rel string) error {
		_, err := os.Stat(filepath.Join(repoDir, filepath.FromSlash(rel)))
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		docs = append(docs, scannedDoc{entity: entity, rel: rel})
		return nil
	}
	for _, d := range decisions {
		if d.State != domain.DecisionActive {
			continue
		}
		if err := appendIfPresent(d.ID, "content/decisions/"+d.ID+".md"); err != nil {
			return nil, err
		}
	}
	for _, r := range references {
		if r.State != domain.ReferenceCurrent {
			continue
		}
		if err := appendIfPresent(r.ID, "content/references/"+r.ID+".md"); err != nil {
			return nil, err
		}
	}
	for _, rb := range runbooks {
		if rb.State != domain.RunbookActive {
			continue
		}
		if err := appendIfPresent(rb.ID, "content/runbooks/"+rb.ID+".md"); err != nil {
			return nil, err
		}
	}
	for _, nt := range notes {
		if nt.State != domain.NoteActive {
			continue
		}
		if err := appendIfPresent(nt.ID, "content/notes/"+nt.ID+".md"); err != nil {
			return nil, err
		}
	}
	// Errata is scanned regardless of entity state: it exists to correct
	// CONCLUDED (immutable) entities, so gating it by liveFeatureStates/active
	// would defeat its purpose — a stale reference in the errata of an
	// implemented feature must still surface (F-0072).
	for _, f := range features {
		if err := appendIfPresent(f.ID, "content/errata/"+f.ID+".md"); err != nil {
			return nil, err
		}
	}
	for _, d := range decisions {
		if err := appendIfPresent(d.ID, "content/errata/"+d.ID+".md"); err != nil {
			return nil, err
		}
	}
	for _, rb := range runbooks {
		if err := appendIfPresent(rb.ID, "content/errata/"+rb.ID+".md"); err != nil {
			return nil, err
		}
	}
	for _, nt := range notes {
		if err := appendIfPresent(nt.ID, "content/errata/"+nt.ID+".md"); err != nil {
			return nil, err
		}
	}
	return docs, nil
}

// stateLabel maps an English lifecycle state to the word used in
// user-facing ref-to-superseded findings (the rule's UI/CLI label is
// "mention of a superseded entity").
func stateLabel(state string) string {
	return state
}

// refFindings reports the terminal entities the document mentions. All hits
// of the same entity collapse into one finding: an ID or title hit keeps the
// ID-based detail; otherwise the first matching imported identifier names
// the mention.
func refFindings(d scannedDoc, text string, needles []needle) []Finding {
	var findings []Finding
	isErrata := strings.HasPrefix(d.rel, "content/errata/")
	for _, n := range needles {
		detail := matchNeedle(n, text)
		if detail == "" {
			continue
		}
		// F-0094: an entity's errata legitimately names what that entity
		// superseded — the supersession record is not stale debt. Tolerate when
		// the mention's successor (n.use) is the errata's own entity. Errata is
		// not materialized for editing, so it cannot carry the F-0089 marker;
		// this is its acknowledgment path. Other stale refs in an errata still
		// surface (F-0072 keeps scanning errata of concluded entities).
		if isErrata && n.use != "" && n.use == d.entity {
			continue
		}
		// An entity's own errata carries its id in the frontmatter (cerne_id)
		// and naturally names the entity. When that entity is itself superseded
		// — e.g. a runbook retired with no successor, so the F-0094 predecessor
		// tolerance above (n.use == d.entity) does not apply — this self-mention
		// is a record, not stale debt, and errata cannot carry the F-0089 marker.
		if isErrata && n.id == d.entity {
			continue
		}
		// F-0089: a per-mention marker (lint:ok-superseded) on the same line
		// declares the reference as an intentional historical mention, not a
		// debt. Suppress only when EVERY occurrence of n is acknowledged on its
		// own line — an unmarked mention elsewhere is still a debt, and the
		// marker's own line self-suppresses the id it names.
		if allMentionsAcknowledged(text, n) {
			continue
		}
		if n.use != "" {
			detail += " — use " + n.use
		}
		findings = append(findings, Finding{Rule: RuleRefToSuperseded, Entity: d.entity, File: d.rel, Detail: detail})
	}
	return findings
}

// matchNeedle returns the ref-to-superseded detail for the first matching
// mention (by id, title or imported identifier), or "" when n is absent.
func matchNeedle(n needle, text string) string {
	if n.rxID.MatchString(text) {
		return fmt.Sprintf("mentions %s (\"%s\"), which is %s", n.id, n.title, stateLabel(n.state))
	}
	if n.rxTitle != nil && n.rxTitle.MatchString(text) {
		return fmt.Sprintf("mentions %s (\"%s\"), which is %s", n.id, n.title, stateLabel(n.state))
	}
	for _, in := range n.imported {
		if in.rx.MatchString(text) {
			return fmt.Sprintf("mentions %s (%s \"%s\"), which is %s", in.text, n.id, n.title, stateLabel(n.state))
		}
	}
	return ""
}

// lineAround returns the single line of text containing the match at loc, so
// tolerance is judged on the mention's own line, not the whole document.
func lineAround(text string, loc []int) string {
	if loc == nil {
		return ""
	}
	start := strings.LastIndexByte(text[:loc[0]], '\n') + 1
	end := len(text)
	if nl := strings.IndexByte(text[loc[0]:], '\n'); nl >= 0 {
		end = loc[0] + nl
	}
	return text[start:end]
}

// ackRx matches the F-0089 acknowledgment marker on a line, capturing its
// comma-separated id list: <!-- lint:ok-superseded D-0003[, D-0005…] -->.
var ackRx = regexp.MustCompile(`(?i)<!--\s*lint:ok-superseded\s+([a-z]+-\d+(?:\s*,\s*[a-z]+-\d+)*)\s*-->`)

// acknowledgedOnLine reports whether line carries a lint:ok-superseded marker
// that names id (case-insensitive, space-tolerant). It declares a mention of a
// superseded entity on that line as intentional history, not a debt (F-0089).
func acknowledgedOnLine(line, id string) bool {
	m := ackRx.FindStringSubmatch(line)
	if m == nil {
		return false
	}
	want := strings.ToUpper(strings.TrimSpace(id))
	for _, part := range strings.Split(m[1], ",") {
		if strings.ToUpper(strings.TrimSpace(part)) == want {
			return true
		}
	}
	return false
}

// allMentionsAcknowledged reports whether every line on which needle n is
// mentioned (by id, title or imported identifier) carries a lint:ok-superseded
// marker naming n. Because refFindings collapses all hits of an entity into one
// finding, suppression is whole-entity: a single unmarked mention keeps the
// debt (F-0089, crit. 4-5). Returns false when n is absent.
func allMentionsAcknowledged(text string, n needle) bool {
	seen := false
	allMarked := func(rx *regexp.Regexp) bool {
		for _, loc := range rx.FindAllStringIndex(text, -1) {
			seen = true
			if !acknowledgedOnLine(lineAround(text, loc), n.id) {
				return false
			}
		}
		return true
	}
	if !allMarked(n.rxID) {
		return false
	}
	if n.rxTitle != nil && !allMarked(n.rxTitle) {
		return false
	}
	for _, in := range n.imported {
		if !allMarked(in.rx) {
			return false
		}
	}
	return seen
}

// linkFindings reports markdown links whose relative target does not exist
// when resolved from the document's directory inside the backend tree.
// External (http/https/mailto), pure-anchor and absolute targets are skipped;
// a fragment suffix is stripped before resolving.
func linkFindings(repoDir string, d scannedDoc, text string) []Finding {
	docDir := filepath.FromSlash(path.Dir(d.rel))
	_, broken := mdlink.ContentLinks(text, func(clean string) (string, bool) {
		full := filepath.Join(repoDir, docDir, filepath.FromSlash(clean))
		// Any stat failure (not just fs.ErrNotExist) means the target is
		// unreachable from the doc, e.g. characters invalid on the OS.
		if _, err := os.Stat(full); err != nil {
			return "", false
		}
		return full, true
	})
	findings := make([]Finding, 0, len(broken))
	for _, target := range broken {
		findings = append(findings, Finding{Rule: RuleBrokenLink, Entity: d.entity, File: d.rel, Detail: "broken link: " + target})
	}
	return findings
}

// knownEntityIDs collects every entity id, canonicalized, as the id-form
// wiki-link resolution set (F-0167): any kind resolves by id, historicals
// included (id mention of history is legitimate — F-0092 restricts only
// title/name matching, which id-form wiki-links never do).
func knownEntityIDs(features []store.Feature, decisions []store.Decision, references []store.Reference, runbooks []store.Runbook, notes []store.Note, historicals []store.Historical) map[string]bool {
	ids := map[string]bool{}
	for _, f := range features {
		ids[mdlink.CanonicalID(f.ID)] = true
	}
	for _, d := range decisions {
		ids[mdlink.CanonicalID(d.ID)] = true
	}
	for _, r := range references {
		ids[mdlink.CanonicalID(r.ID)] = true
	}
	for _, rb := range runbooks {
		ids[mdlink.CanonicalID(rb.ID)] = true
	}
	for _, n := range notes {
		ids[mdlink.CanonicalID(n.ID)] = true
	}
	for _, h := range historicals {
		ids[mdlink.CanonicalID(h.ID)] = true
	}
	return ids
}

// wikiLinkFindings reports id-form wiki-links whose id is not a known entity
// (F-0167). Resolution is by id only, case-insensitive; non-id [[...]] text is
// never extracted by mdlink, so this rule cannot false-positive on TOML
// tables or bracketed prose.
func wikiLinkFindings(d scannedDoc, text string, ids map[string]bool) []Finding {
	_, broken := mdlink.WikiLinks(text, func(target string) (string, bool) {
		id := mdlink.CanonicalID(target)
		return id, ids[id]
	})
	findings := make([]Finding, 0, len(broken))
	for _, target := range broken {
		findings = append(findings, Finding{Rule: RuleBrokenLink, Entity: d.entity, File: d.rel, Detail: "broken wiki-link: [[" + target + "]]"})
	}
	return findings
}

// normalizeURL canonicalizes a URL for source matching (D2): the #fragment is
// dropped and a trailing slash trimmed, but the scheme is kept so http and https
// stay distinct.
func normalizeURL(u string) string {
	if i := strings.IndexByte(u, '#'); i >= 0 {
		u = u[:i]
	}
	return strings.TrimRight(u, "/")
}

// inUnregisteredSourceScope reports whether a doc is subject to unregistered-source
// (F-0107): only decision docs and feature spec/plan/result carry decision
// provenance. Runbooks, references, notes and errata legitimately cite operational
// or self URLs and are out of scope.
func inUnregisteredSourceScope(rel string) bool {
	return strings.HasPrefix(rel, "content/features/") || strings.HasPrefix(rel, "content/decisions/")
}

// ignoredSourceURL reports whether a URL is never an external source and so is
// skipped by unregistered-source (F-0107): an internal/local host (`.local`,
// `localhost`, a private or loopback IP) or a URL containing any project-configured
// ignore substring (e.g. the project's own code repo host).
func ignoredSourceURL(u string, ignore []string) bool {
	for _, s := range ignore {
		if s != "" && strings.Contains(u, s) {
			return true
		}
	}
	parsed, err := url.Parse(u)
	if err != nil {
		return false
	}
	host := parsed.Hostname()
	if host == "localhost" || strings.HasSuffix(host, ".local") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil && (ip.IsPrivate() || ip.IsLoopback()) {
		return true
	}
	return false
}

// unregisteredSourceFindings reports external http(s) URLs cited in an authored
// decision or feature doc that no registered reference backs (RuleUnregisteredSource).
// Backing is the union (D1, F-0103): a *current* reference whose URL matches
// (normalized) OR one whose Informs backlinks the doc's entity. URLs that are never
// sources — internal/local hosts or a configured ignore substring (F-0107) — are
// skipped. The fix is to register the source (cerne reference new … --informs) or
// remove the bare URL — so a discarded dead-end never becomes a forced reference.
func unregisteredSourceFindings(d scannedDoc, text string, refs []store.Reference, ignore []string) []Finding {
	var findings []Finding
	for _, u := range OrphanSourceURLs(d.entity, text, refs, ignore) {
		findings = append(findings, Finding{
			Rule:   RuleUnregisteredSource,
			Entity: d.entity,
			File:   d.rel,
			Detail: "external URL " + u + " cited without a registered reference — register it with 'cerne reference new … --informs " + d.entity + "' or remove the URL",
		})
	}
	return findings
}

// OrphanSourceURLs reports the external http(s) URLs cited in an authored
// feature/decision doc that no current reference backs — the shared core of
// RuleUnregisteredSource, exported (F-0172) so the push-time provenance
// reminder and the lint backstop can never diverge. Backing is the union
// (D1, F-0103): a *current* reference whose URL matches (normalized) OR one
// whose Informs backlinks the doc's entity; internal/local hosts and the
// project's ignore substrings (F-0107) are skipped.
func OrphanSourceURLs(entity, text string, refs []store.Reference, ignore []string) []string {
	urls := mdlink.ExternalURLs(text)
	if len(urls) == 0 {
		return nil
	}
	backed := make(map[string]bool, len(refs))
	for _, r := range refs {
		if r.State != domain.ReferenceCurrent {
			continue
		}
		for _, id := range r.Informs {
			if id == entity {
				return nil // the doc's entity is already sourced (Informs backlink)
			}
		}
		if r.URL != "" {
			backed[normalizeURL(r.URL)] = true
		}
	}
	var orphans []string
	for _, u := range urls {
		if ignoredSourceURL(u, ignore) || backed[normalizeURL(u)] {
			continue
		}
		orphans = append(orphans, u)
	}
	return orphans
}

// orphanFindings reports content files whose entity JSON does not exist:
// every file under content/features/<ID>/ for an unknown <ID>, and every
// content/decisions/<ID>.md / content/references/<ID>.md (extension
// stripped) for an unknown <ID>.
func orphanFindings(repoDir string, features []store.Feature, decisions []store.Decision, references []store.Reference, runbooks []store.Runbook, notes []store.Note) ([]Finding, error) {
	featureIDs := make(map[string]bool, len(features))
	for _, f := range features {
		featureIDs[f.ID] = true
	}

	var findings []Finding
	featDir := filepath.Join(repoDir, "content", "features")
	entries, err := os.ReadDir(featDir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir() || featureIDs[e.Name()] {
			continue
		}
		walkErr := filepath.WalkDir(filepath.Join(featDir, e.Name()), func(p string, de fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if de.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(repoDir, p)
			if err != nil {
				return err
			}
			relSlash := filepath.ToSlash(rel)
			findings = append(findings, Finding{Rule: RuleOrphanContent, File: relSlash, Detail: "content without an entity: " + relSlash})
			return nil
		})
		if walkErr != nil {
			return nil, walkErr
		}
	}

	decisionIDs := make(map[string]bool, len(decisions))
	for _, d := range decisions {
		decisionIDs[d.ID] = true
	}
	referenceIDs := make(map[string]bool, len(references))
	for _, r := range references {
		referenceIDs[r.ID] = true
	}
	runbookIDs := make(map[string]bool, len(runbooks))
	for _, rb := range runbooks {
		runbookIDs[rb.ID] = true
	}
	noteIDs := make(map[string]bool, len(notes))
	for _, nt := range notes {
		noteIDs[nt.ID] = true
	}
	for _, kind := range []struct {
		subdir string
		ids    map[string]bool
	}{
		{"content/decisions", decisionIDs},
		{"content/runbooks", runbookIDs},
		{"content/notes", noteIDs},
		{"content/references", referenceIDs},
	} {
		entries, err := os.ReadDir(filepath.Join(repoDir, filepath.FromSlash(kind.subdir)))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			if kind.ids[strings.TrimSuffix(e.Name(), ".md")] {
				continue
			}
			rel := kind.subdir + "/" + e.Name()
			findings = append(findings, Finding{Rule: RuleOrphanContent, File: rel, Detail: "content without an entity: " + rel})
		}
	}
	return findings, nil
}

// staleCandidateDays is how long a feature may sit in candidate without a real
// spec before stale-candidate flags it. Long enough that register-at-start work
// (a feature minted the moment an agent picks up a task) is never noise; short
// enough that a candidate nobody followed through on surfaces instead of rotting
// on the board. This is the guard for the "register the task at the start" rule:
// it keeps the candidate inbox honest without policing fresh or progressing work.
const staleCandidateDays = 30

// staleCandidateFindings flags features parked in candidate past
// staleCandidateDays with no real spec.md — registered work that was never
// elaborated nor rejected. A candidate that already has a real spec is
// progressing (it just needs `approve`) and a candidate inside its grace window
// was likely just minted, so both stay silent; only the abandoned, specless
// ones are reported. now is injected so the rule is deterministic under test —
// Run passes time.Now().
func staleCandidateFindings(repoDir string, features []store.Feature, now time.Time) ([]Finding, error) {
	threshold := time.Duration(staleCandidateDays) * 24 * time.Hour
	var findings []Finding
	for _, f := range features {
		if f.State != domain.StateCandidate {
			continue
		}
		age := now.Sub(f.CreatedAt)
		if age < threshold {
			continue
		}
		real, err := isRealFile(filepath.Join(repoDir, "content", "features", f.ID, "spec.md"))
		if err != nil {
			return nil, err
		}
		if real {
			continue
		}
		findings = append(findings, Finding{
			Rule:   RuleStaleCandidate,
			Entity: f.ID,
			Detail: fmt.Sprintf("candidate for %d days without a spec — reject it or advance it with a spec", int(age.Hours()/24)),
		})
	}
	return findings, nil
}

// defaultStaleReadyDays mirrors project.Config.StaleReadyWindowDays' 14-day
// fallback (an unconfigured stale_ready_days). Kept local to lint so this
// package does not import project just for one constant.
const defaultStaleReadyDays = 14

// staleReadyFindings flags a feature parked in implemented ("Ready") past the
// project's stale_ready_days window (F-0131 crit. 6) with no human accept yet
// — the board differentiates "code done" from "a human validated it", and a
// Ready card that lingers is exactly the debt this advisory rule surfaces.
// done is excluded: it already crossed the human gate, so it is never
// "waiting" for one. windowDays<=0 falls back to the 14-day default; now is
// injected so the rule is deterministic under test (Run passes time.Now()).
func staleReadyFindings(features []store.Feature, windowDays int, now time.Time) []Finding {
	if windowDays <= 0 {
		windowDays = defaultStaleReadyDays
	}
	threshold := time.Duration(windowDays) * 24 * time.Hour
	var findings []Finding
	for _, f := range features {
		if f.State != domain.StateImplemented {
			continue
		}
		age := now.Sub(f.UpdatedAt)
		if age < threshold {
			continue
		}
		findings = append(findings, Finding{
			Rule:   RuleStaleReady,
			Entity: f.ID,
			Detail: fmt.Sprintf("implemented (Ready) for %d days without acceptance — review with 'cerne show %s --doc result', then run 'cerne feature accept %s' to validate", int(age.Hours()/24), f.ID, f.ID),
		})
	}
	return findings
}

// defaultStaleWorkHours mirrors project.Config.StaleWorkWindowHours' 8-hour
// fallback (an unconfigured stale_work_hours). Kept local to lint so this
// package does not import project just for one constant.
const defaultStaleWorkHours = 8

// staleWorkFindings flags an active work (agent activity, F-0130) whose
// UpdatedAt is older than the project's stale_work_hours window with no
// done/abandon yet — an agent that died without closing it never becomes an
// eternal ghost on the board. done/abandoned works are excluded: they already
// closed. hours<=0 falls back to the 8-hour default; now is injected so the
// rule is deterministic under test (Run passes time.Now()).
func staleWorkFindings(works []store.Work, hours int, now time.Time) []Finding {
	if hours <= 0 {
		hours = defaultStaleWorkHours
	}
	threshold := time.Duration(hours) * time.Hour
	var findings []Finding
	for _, w := range works {
		if w.State != domain.WorkActive {
			continue
		}
		age := now.Sub(w.UpdatedAt)
		if age < threshold {
			continue
		}
		findings = append(findings, Finding{
			Rule:   RuleStaleWork,
			Entity: w.ID,
			Detail: fmt.Sprintf("active for %.0fh without done/abandon — run 'cerne work done %s' or 'cerne work abandon %s'", age.Hours(), w.ID, w.ID),
		})
	}
	return findings
}

// staleNoteFindings flags an active note whose --informs targets are ALL
// terminal — every informed feature is implemented/rejected/superseded and every
// informed decision is superseded — so the work the note documented is settled.
// Such a note is a strong retirement candidate: surface it for a human to confirm
// it is still durable or `cerne note retire` it. It never auto-retires, since the
// gotcha learned building a feature is often the durable invariant worth keeping.
// A note with any live target, or no --informs at all (freeform knowledge with no
// binding), is presumed durable and left alone; a superseded note is history; a
// note reviewed as durable (`cerne note keep`, the Durable flag) is silenced.
func staleNoteFindings(features []store.Feature, decisions []store.Decision, notes []store.Note) []Finding {
	featureState := make(map[string]domain.FeatureState, len(features))
	for _, f := range features {
		featureState[f.ID] = f.State
	}
	decisionState := make(map[string]domain.DecisionState, len(decisions))
	for _, d := range decisions {
		decisionState[d.ID] = d.State
	}
	// classify reports whether an informed target is still live (work ongoing or
	// decision standing) plus its raw state label, or ok=false when unknown.
	classify := func(id string) (live bool, state string, ok bool) {
		if s, ok := featureState[id]; ok {
			return s == domain.StateCandidate || s == domain.StateApproved || s == domain.StateInDevelopment, string(s), true
		}
		if s, ok := decisionState[id]; ok {
			return s == domain.DecisionActive, string(s), true
		}
		return false, "", false
	}

	var findings []Finding
	for _, nt := range notes {
		if nt.State != domain.NoteActive || nt.Durable || len(nt.Informs) == 0 {
			continue
		}
		terminals := make([]string, 0, len(nt.Informs))
		allTerminal := true
		for _, target := range nt.Informs {
			live, state, ok := classify(target)
			if !ok || live {
				allTerminal = false
				break
			}
			terminals = append(terminals, target+" "+state)
		}
		if !allTerminal {
			continue
		}
		findings = append(findings, Finding{
			Rule:   RuleStaleNote,
			Entity: nt.ID,
			Detail: fmt.Sprintf("only informs concluded work (%s) — retire it or confirm it is still durable", strings.Join(terminals, ", ")),
		})
	}
	return findings
}

// untaggedLiveFindings flags a live entity — an active note, decision or runbook,
// a current reference, an actively-worked feature — carrying no tag on EITHER axis
// (facets and components both empty). Such an entity is invisible to the
// categorical `list --facet/--component` filters and reachable only by full-text
// search/ask, a retrieval gap that compounds as the project grows. It is
// advisory, never blocking: D-0002 fixed the tag vocabulary but not a mandatory
// gate, so creation stays frictionless and this rule surfaces the debt for a
// batched `cerne <kind> retag`. Superseded/obsolete entities are history and are
// never flagged. Features gained the tag axes in D-0003; only the actively-worked
// states (candidate/approved/in_development) are flagged — the implemented
// backlog is deliberately spared so the rule never dumps the whole done-history
// as untagged debt.
func untaggedLiveFindings(features []store.Feature, decisions []store.Decision, references []store.Reference, notes []store.Note, runbooks []store.Runbook) []Finding {
	untagged := func(facets, components []string) bool {
		return len(facets) == 0 && len(components) == 0
	}
	var findings []Finding
	for _, f := range features {
		live := f.State == domain.StateCandidate || f.State == domain.StateApproved || f.State == domain.StateInDevelopment
		if live && untagged(f.Facets, f.Components) {
			findings = append(findings, Finding{
				Rule:   RuleUntaggedLive,
				Entity: f.ID,
				Detail: "no tag (facet/component) — classify it with 'cerne feature retag'",
			})
		}
	}
	for _, nt := range notes {
		if nt.State == domain.NoteActive && untagged(nt.Facets, nt.Components) {
			findings = append(findings, Finding{
				Rule:   RuleUntaggedLive,
				Entity: nt.ID,
				Detail: "no tag (facet/component) — classify it with 'cerne note retag'",
			})
		}
	}
	for _, d := range decisions {
		if d.State == domain.DecisionActive && untagged(d.Facets, d.Components) {
			findings = append(findings, Finding{
				Rule:   RuleUntaggedLive,
				Entity: d.ID,
				Detail: "no tag (facet/component) — classify it with 'cerne decision retag'",
			})
		}
	}
	for _, r := range references {
		if r.State == domain.ReferenceCurrent && untagged(r.Facets, r.Components) {
			findings = append(findings, Finding{
				Rule:   RuleUntaggedLive,
				Entity: r.ID,
				Detail: "no tag (facet/component) — classify it with 'cerne reference retag'",
			})
		}
	}
	for _, rb := range runbooks {
		if rb.State == domain.RunbookActive && untagged(rb.Facets, rb.Components) {
			findings = append(findings, Finding{
				Rule:   RuleUntaggedLive,
				Entity: rb.ID,
				Detail: "no tag (facet/component) — classify it with 'cerne runbook retag'",
			})
		}
	}
	return findings
}

// gateFindings enforces the entity-state document invariants: approved and
// later states require a real spec.md, in_development and later a real
// plan.md, implemented (and done, its accepted successor — F-0131) a real
// result.md. "Real" reuses the gate stub check (doccheck.HasRealContent), so
// a missing file and an untouched stub are reported the same way.
func gateFindings(repoDir string, features []store.Feature) ([]Finding, error) {
	var findings []Finding
	for _, f := range features {
		var required []string
		switch f.State {
		case domain.StateApproved:
			required = []string{"spec.md"}
		case domain.StateInDevelopment:
			required = []string{"spec.md", "plan.md"}
		case domain.StateImplemented, domain.StateDone:
			required = []string{"spec.md", "plan.md", "result.md"}
		default:
			continue
		}
		if domain.CheckGate(f.ID, domain.ActionStart, domain.DocSet{HasPlan: false}) == nil {
			required = removeDoc(required, "plan.md")
		}
		for _, doc := range required {
			real, err := isRealFile(filepath.Join(repoDir, "content", "features", f.ID, doc))
			if err != nil {
				return nil, err
			}
			if real {
				continue
			}
			findings = append(findings, Finding{
				Rule:   RuleMissingGateDoc,
				Entity: f.ID,
				Detail: fmt.Sprintf("%s feature without a real %s", f.State, doc),
			})
		}
	}
	return findings, nil
}

// removeDoc returns docs without name, preserving order.
func removeDoc(docs []string, name string) []string {
	out := docs[:0:0]
	for _, d := range docs {
		if d != name {
			out = append(out, d)
		}
	}
	return out
}

// isRealFile reports whether the file exists and has non-stub content.
func isRealFile(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return doccheck.HasRealContent(data), nil
}
