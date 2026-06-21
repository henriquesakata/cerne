package importer

import (
	"regexp"
	"strings"

	"gitlab.com/rockt/cerne/internal/store"
)

// reCasePrefix matches a leading "Case NNNN —|–|:|- " label some case specs
// carry in their heading, so a candidate's title compares equal to the backend
// feature registered natively without the label.
var reCasePrefix = regexp.MustCompile(`^Case\s+\d+\s*[—–:-]\s*`)

// reSpaces collapses internal whitespace runs to a single space.
var reSpaces = regexp.MustCompile(`\s+`)

// normalizeTitle canonicalizes a title for identity comparison: it strips a
// leading "Case NNNN — " label, trims, and collapses internal whitespace runs
// to single spaces. Deterministic — no fuzzy matching.
func normalizeTitle(s string) string {
	s = reCasePrefix.ReplaceAllString(s, "")
	s = reSpaces.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// ExistingFeatureTitles returns the set of normalized titles of every feature
// already registered in the backend at repoDir, so import can skip re-minting a
// feature (registered natively, hence without an imported_from source) whose
// title already exists. A backend with no features yields an empty set.
func ExistingFeatureTitles(repoDir string) (map[string]bool, error) {
	features, err := store.LoadAllFeatures(repoDir)
	if err != nil {
		return nil, err
	}
	titles := make(map[string]bool, len(features))
	for _, f := range features {
		titles[normalizeTitle(f.Title)] = true
	}
	return titles, nil
}

// DropRegisteredTitles splits cands, dropping the feature candidates whose
// normalized title already exists in titles (kept order preserved) and counting
// them as skipped. Non-feature candidates (decisions, references, runbooks) and
// features whose title is not in the set are always kept — so a genuine orphan
// (a case implemented but never registered) is still surfaced for import.
func DropRegisteredTitles(cands []Candidate, titles map[string]bool) (kept []Candidate, skipped int) {
	for _, c := range cands {
		if c.Kind == "feature" && titles[normalizeTitle(c.Title)] {
			skipped++
			continue
		}
		kept = append(kept, c)
	}
	return kept, skipped
}
