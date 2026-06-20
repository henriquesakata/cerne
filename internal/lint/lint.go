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
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"gitlab.com/rockt/cerne/internal/doccheck"
	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/mdlink"
	"gitlab.com/rockt/cerne/internal/store"
	"gitlab.com/rockt/cerne/internal/wiki"
)

// Rule names, in deterministic report order.
const (
	RuleRefToSuperseded   = "ref-to-superseded"
	RuleBrokenLink        = "broken-link"
	RuleOrphanContent     = "orphan-content"
	RuleMissingGateDoc    = "missing-gate-doc"
	RuleWikiSourceUnknown = "wiki-source-unknown"
	RuleParentNotFound    = "parent-not-found"
	RuleParentTerminal    = "parent-terminal"
	RuleEpicOpenChildren  = "epic-open-children"
	RuleStaleCandidate    = "stale-candidate"
)

// RuleOrder is the deterministic report order of the lint rules.
var RuleOrder = []string{RuleRefToSuperseded, RuleBrokenLink, RuleOrphanContent, RuleMissingGateDoc, RuleWikiSourceUnknown, RuleParentNotFound, RuleParentTerminal, RuleEpicOpenChildren, RuleStaleCandidate}

var ruleRank = map[string]int{
	RuleRefToSuperseded:   0,
	RuleBrokenLink:        1,
	RuleOrphanContent:     2,
	RuleMissingGateDoc:    3,
	RuleWikiSourceUnknown: 4,
	RuleParentNotFound:    5,
	RuleParentTerminal:    6,
	RuleEpicOpenChildren:  7,
	RuleStaleCandidate:    8,
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

// scannedDoc is one live-entity content document subject to the text rules.
type scannedDoc struct {
	entity string
	rel    string // rel slash path within the backend
}

// Run lints the backend at repoDir and returns the findings in
// deterministic order: by Rule (RuleOrder), then File, then Detail
// (Entity as final tie-breaker).
func Run(repoDir string) ([]Finding, error) {
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
	historicals, err := store.LoadAllHistoricals(repoDir)
	if err != nil {
		return nil, err
	}

	needles, err := buildNeedles(repoDir, features, decisions, references, historicals)
	if err != nil {
		return nil, err
	}
	docs, err := scannedDocs(repoDir, features, decisions, references)
	if err != nil {
		return nil, err
	}
	wikiPages, err := wikiDocs(repoDir)
	if err != nil {
		return nil, err
	}
	docs = append(docs, wikiPages...)

	var findings []Finding
	for _, d := range docs {
		data, err := os.ReadFile(filepath.Join(repoDir, filepath.FromSlash(d.rel)))
		if err != nil {
			return nil, err
		}
		text := string(data)
		findings = append(findings, refFindings(d, text, needles)...)
		findings = append(findings, linkFindings(repoDir, d, text)...)
	}

	orphans, err := orphanFindings(repoDir, features, decisions, references)
	if err != nil {
		return nil, err
	}
	findings = append(findings, orphans...)

	gates, err := gateFindings(repoDir, features)
	if err != nil {
		return nil, err
	}
	findings = append(findings, gates...)

	wikiSources, err := wikiSourceFindings(repoDir, features, decisions, references)
	if err != nil {
		return nil, err
	}
	findings = append(findings, wikiSources...)

	findings = append(findings, parentFindings(features)...)

	stale, err := staleCandidateFindings(repoDir, features, time.Now())
	if err != nil {
		return nil, err
	}
	findings = append(findings, stale...)

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
	return findings, nil
}

// parentFindings checks the epic (parent/child) invariants over the features,
// independent of any content doc:
//   - parent-not-found: a feature points at a parent that does not exist.
//   - parent-terminal: a live feature sits under an epic that is terminal
//     (superseded/rejected) — the children were orphaned.
//   - epic-open-children: an implemented feature still has open (non-terminal)
//     children — the epic was marked done but its decomposition is not.
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
				Detail: fmt.Sprintf("%s aponta para o épico %s, que não existe", f.ID, f.Parent),
			})
			continue
		}
		if isLive(f.State) && isTerminal(parent.State) {
			findings = append(findings, Finding{
				Rule:   RuleParentTerminal,
				Entity: f.ID,
				Detail: fmt.Sprintf("%s está sob o épico %s, que está %s", f.ID, parent.ID, parent.State),
			})
		}
	}

	for epicID, children := range store.ChildrenByParent(features) {
		epic, ok := byID[epicID]
		if !ok || epic.State != domain.StateImplemented {
			continue
		}
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
				Detail: fmt.Sprintf("o épico %s está implementado, mas tem %d filho(s) ainda em aberto", epicID, open),
			})
		}
	}
	return findings
}

// buildNeedles collects every terminal entity scanned docs must not mention:
// superseded/rejected features, superseded decisions, obsolete references and
// historical entities. Each needle also carries the original identifiers
// derived from the imported_from frontmatter of the entity's content doc (the
// spec for features), when present; for historical entities the declared
// aliases play that role.
func buildNeedles(repoDir string, features []store.Feature, decisions []store.Decision, references []store.Reference, historicals []store.Historical) ([]needle, error) {
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
	for _, h := range historicals {
		// A historical entity is born terminal; no content doc, so its
		// aliases (validated >= MinAliasLen at creation) are the imported-style
		// identifiers live docs must not mention.
		n := needle{id: h.ID, title: h.Name, state: string(h.State), use: h.SupersededBy, rxID: wordRx(h.ID)}
		if utf8.RuneCountInString(h.Name) >= minTitleNeedleLen {
			n.rxTitle = wordRx(h.Name)
		}
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

// wikiDocs lists the compiled wiki pages as scanned documents, so the text
// rules (ref-to-superseded, broken-link) run over them too.
func wikiDocs(repoDir string) ([]scannedDoc, error) {
	rels, err := wikiPagePaths(repoDir)
	if err != nil {
		return nil, err
	}
	docs := make([]scannedDoc, 0, len(rels))
	for _, rel := range rels {
		docs = append(docs, scannedDoc{entity: "", rel: rel})
	}
	return docs, nil
}

// wikiSourceFindings reports wiki pages whose provenance lists a source that
// resolves to no existing entity (a dangling synthesis input).
func wikiSourceFindings(repoDir string, features []store.Feature, decisions []store.Decision, references []store.Reference) ([]Finding, error) {
	known := make(map[string]bool, len(features)+len(decisions)+len(references))
	for _, f := range features {
		known[f.ID] = true
	}
	for _, d := range decisions {
		known[d.ID] = true
	}
	for _, r := range references {
		known[r.ID] = true
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
				findings = append(findings, Finding{Rule: RuleWikiSourceUnknown, File: rel, Detail: "fonte desconhecida: " + src})
			}
		}
	}
	return findings, nil
}

// scannedDocs lists the content documents of live entities: every *.md under
// content/features/<ID>/ for candidate/approved/in_development features,
// plus content/decisions/<ID>.md for active decisions and
// content/references/<ID>.md for current references, when present.
func scannedDocs(repoDir string, features []store.Feature, decisions []store.Decision, references []store.Reference) ([]scannedDoc, error) {
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
	return docs, nil
}

// stateLabelPT maps an English lifecycle state to the pt-BR adjective used in
// user-facing ref-to-superseded findings. Feminine, to agree with "entidade"
// (the rule's UI/CLI label is "menção a entidade substituída").
func stateLabelPT(state string) string {
	switch state {
	case "superseded":
		return "substituída"
	case "rejected":
		return "rejeitada"
	case "obsolete":
		return "obsoleta"
	}
	return state
}

// refFindings reports the terminal entities the document mentions. All hits
// of the same entity collapse into one finding: an ID or title hit keeps the
// ID-based detail; otherwise the first matching imported identifier names
// the mention.
func refFindings(d scannedDoc, text string, needles []needle) []Finding {
	var findings []Finding
	for _, n := range needles {
		var detail string
		switch {
		case n.rxID.MatchString(text) || (n.rxTitle != nil && n.rxTitle.MatchString(text)):
			detail = fmt.Sprintf("menciona %s (\"%s\") que está %s", n.id, n.title, stateLabelPT(n.state))
		default:
			for _, in := range n.imported {
				if in.rx.MatchString(text) {
					detail = fmt.Sprintf("menciona %s (%s \"%s\") que está %s", in.text, n.id, n.title, stateLabelPT(n.state))
					break
				}
			}
		}
		if detail == "" {
			continue
		}
		if n.use != "" {
			detail += " — use " + n.use
		}
		findings = append(findings, Finding{Rule: RuleRefToSuperseded, Entity: d.entity, File: d.rel, Detail: detail})
	}
	return findings
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
		findings = append(findings, Finding{Rule: RuleBrokenLink, Entity: d.entity, File: d.rel, Detail: "link quebrado: " + target})
	}
	return findings
}

// orphanFindings reports content files whose entity JSON does not exist:
// every file under content/features/<ID>/ for an unknown <ID>, and every
// content/decisions/<ID>.md / content/references/<ID>.md (extension
// stripped) for an unknown <ID>.
func orphanFindings(repoDir string, features []store.Feature, decisions []store.Decision, references []store.Reference) ([]Finding, error) {
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
			findings = append(findings, Finding{Rule: RuleOrphanContent, File: relSlash, Detail: "conteúdo sem entidade: " + relSlash})
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
	for _, kind := range []struct {
		subdir string
		ids    map[string]bool
	}{
		{"content/decisions", decisionIDs},
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
			findings = append(findings, Finding{Rule: RuleOrphanContent, File: rel, Detail: "conteúdo sem entidade: " + rel})
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
			Detail: fmt.Sprintf("candidata há %d dias sem spec — rejeite ou avance com uma spec", int(age.Hours()/24)),
		})
	}
	return findings, nil
}

// gateFindings enforces the entity-state document invariants: approved and
// later states require a real spec.md, in_development and later a real
// plan.md, implemented a real result.md. "Real" reuses the gate stub check
// (doccheck.HasRealContent), so a missing file and an untouched stub are
// reported the same way.
func gateFindings(repoDir string, features []store.Feature) ([]Finding, error) {
	var findings []Finding
	for _, f := range features {
		var required []string
		switch f.State {
		case domain.StateApproved:
			required = []string{"spec.md"}
		case domain.StateInDevelopment:
			required = []string{"spec.md", "plan.md"}
		case domain.StateImplemented:
			required = []string{"spec.md", "plan.md", "result.md"}
		default:
			continue
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
				Detail: fmt.Sprintf("feature %s sem %s real", f.State, doc),
			})
		}
	}
	return findings, nil
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
