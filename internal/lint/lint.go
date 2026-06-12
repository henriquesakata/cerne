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
	"unicode/utf8"

	"gitlab.com/rockt/cerne/internal/doccheck"
	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

// Rule names, in deterministic report order.
const (
	RuleRefToSuperseded = "ref-to-superseded"
	RuleBrokenLink      = "broken-link"
	RuleOrphanContent   = "orphan-content"
	RuleMissingGateDoc  = "missing-gate-doc"
)

// RuleOrder is the deterministic report order of the lint rules.
var RuleOrder = []string{RuleRefToSuperseded, RuleBrokenLink, RuleOrphanContent, RuleMissingGateDoc}

var ruleRank = map[string]int{
	RuleRefToSuperseded: 0,
	RuleBrokenLink:      1,
	RuleOrphanContent:   2,
	RuleMissingGateDoc:  3,
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
// case-insensitive word-boundary substring on the ID and, for features with
// a title of at least minTitleNeedleLen runes, on the title. One finding per
// (doc, needle) pair: ID and title hits of the same entity collapse into a
// single finding sharing the ID-based detail.
type needle struct {
	id, title, state, use string
	rxID, rxTitle         *regexp.Regexp
}

// scannedDoc is one live-entity content document subject to the text rules.
type scannedDoc struct {
	entity string
	rel    string // rel slash path within the backend
}

// mdLinkRx captures the target of inline markdown links [x](target) and
// [x](target "title"); the optional title after the first whitespace is
// ignored.
var mdLinkRx = regexp.MustCompile(`\[[^\]]*\]\(([^)\s]+)[^)]*\)`)

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

	needles := buildNeedles(features, decisions, references)
	docs, err := scannedDocs(repoDir, features, decisions, references)
	if err != nil {
		return nil, err
	}

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

// buildNeedles collects every terminal entity scanned docs must not mention:
// superseded/rejected features, superseded decisions and obsolete references.
func buildNeedles(features []store.Feature, decisions []store.Decision, references []store.Reference) []needle {
	var needles []needle
	for _, f := range features {
		if f.State != domain.StateSuperseded && f.State != domain.StateRejected {
			continue
		}
		n := needle{id: f.ID, title: f.Title, state: string(f.State), use: f.SupersededBy, rxID: wordRx(f.ID)}
		if utf8.RuneCountInString(f.Title) >= minTitleNeedleLen {
			n.rxTitle = wordRx(f.Title)
		}
		needles = append(needles, n)
	}
	for _, d := range decisions {
		if d.State != domain.DecisionSuperseded {
			continue
		}
		needles = append(needles, needle{id: d.ID, title: d.Title, state: string(d.State), use: d.SupersededBy, rxID: wordRx(d.ID)})
	}
	for _, r := range references {
		if r.State != domain.ReferenceObsolete {
			continue
		}
		needles = append(needles, needle{id: r.ID, title: r.Title, state: string(r.State), rxID: wordRx(r.ID)})
	}
	return needles
}

// wordRx compiles a case-insensitive word-boundary matcher for the literal s.
func wordRx(s string) *regexp.Regexp {
	return regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(s) + `\b`)
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

// refFindings reports the terminal entities the document mentions.
func refFindings(d scannedDoc, text string, needles []needle) []Finding {
	var findings []Finding
	for _, n := range needles {
		if !n.rxID.MatchString(text) && (n.rxTitle == nil || !n.rxTitle.MatchString(text)) {
			continue
		}
		detail := fmt.Sprintf("menciona %s (\"%s\") que está %s", n.id, n.title, n.state)
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
	var findings []Finding
	seen := map[string]bool{}
	for _, m := range mdLinkRx.FindAllStringSubmatch(text, -1) {
		target := m[1]
		if seen[target] || skipLinkTarget(target) {
			continue
		}
		seen[target] = true
		resolved, _, _ := strings.Cut(target, "#")
		if resolved == "" {
			continue
		}
		full := filepath.Join(repoDir, filepath.FromSlash(path.Dir(d.rel)), filepath.FromSlash(resolved))
		// Any stat failure (not just fs.ErrNotExist) means the target is
		// unreachable from the doc, e.g. characters invalid on the OS.
		if _, err := os.Stat(full); err != nil {
			findings = append(findings, Finding{Rule: RuleBrokenLink, Entity: d.entity, File: d.rel, Detail: "link quebrado: " + target})
		}
	}
	return findings
}

// skipLinkTarget reports whether the link target is outside the broken-link
// rule: external schemes, pure anchors and absolute paths.
func skipLinkTarget(target string) bool {
	lower := strings.ToLower(target)
	return strings.HasPrefix(lower, "http://") ||
		strings.HasPrefix(lower, "https://") ||
		strings.HasPrefix(lower, "mailto:") ||
		strings.HasPrefix(target, "#") ||
		strings.HasPrefix(target, "/")
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
