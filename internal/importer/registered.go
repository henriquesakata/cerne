package importer

import (
	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

// normalizeTitle is the shared identity rule for titles (domain.NormalizeTitle),
// kept as a local alias so the import guard reads as before.
func normalizeTitle(s string) string {
	return domain.NormalizeTitle(s)
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
