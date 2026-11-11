package materialize

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/henriquesakata/cerne/internal/store"
)

// statKinds is the fixed set of entity kinds reported by ComputeStats. Every
// key is always present in Stats.Kinds, zero-valued when the backend has no
// such entity, so consumers (CLI table, UI cards) render deterministically.
var statKinds = []string{"feature", "decision", "reference", "runbook", "note"}

// KindStats is the context-savings breakdown for one entity kind: the full
// corpus (every entity, every state) vs the current projection (what the
// materializer would deliver into .docs/).
type KindStats struct {
	Count        int `json:"count"`
	Bytes        int `json:"bytes"`
	CurrentCount int `json:"current_count"`
	CurrentBytes int `json:"current_bytes"`
}

// Stats is the context-savings metric (F-0127): how much documentation the
// backend holds vs how much the .docs/ projection makes an agent actually
// read. Token figures are a bytes/4 estimate (~4 chars per token), not a real
// tokenizer count.
type Stats struct {
	Kinds            map[string]KindStats `json:"kinds"`
	TotalCount       int                  `json:"total_count"`
	CurrentCount     int                  `json:"current_count"`
	TotalBytes       int                  `json:"total_bytes"`
	CurrentBytes     int                  `json:"current_bytes"`
	SavedBytes       int                  `json:"saved_bytes"`
	SavedPct         float64              `json:"saved_pct"`
	TotalTokensEst   int                  `json:"total_tokens_est"`
	CurrentTokensEst int                  `json:"current_tokens_est"`
	SavedTokensEst   int                  `json:"saved_tokens_est"`
}

// ComputeStats measures the backend at repoDir: per kind and in total, the
// count and byte size of the full corpus (all entities, all states — their
// content docs plus erratas; wiki and artifacts excluded) against the current
// projection, classified by the same *IsCurrent predicates Materialize uses.
// A fresh or empty backend yields zeros (SavedPct 0, never NaN).
func ComputeStats(repoDir string) (Stats, error) {
	features, err := store.LoadAllFeatures(repoDir)
	if err != nil {
		return Stats{}, err
	}
	decisions, err := store.LoadAllDecisions(repoDir)
	if err != nil {
		return Stats{}, err
	}
	references, err := store.LoadAllReferences(repoDir)
	if err != nil {
		return Stats{}, err
	}
	runbooks, err := store.LoadAllRunbooks(repoDir)
	if err != nil {
		return Stats{}, err
	}
	notes, err := store.LoadAllNotes(repoDir)
	if err != nil {
		return Stats{}, err
	}

	kinds := make(map[string]KindStats, len(statKinds))
	for _, kind := range statKinds {
		kinds[kind] = KindStats{}
	}

	for _, f := range features {
		size := 0
		for _, filename := range featureDocFilenames {
			n, err := docSize(filepath.Join(repoDir, "content", "features", f.ID, filename))
			if err != nil {
				return Stats{}, err
			}
			size += n
		}
		if err := tally(kinds, "feature", repoDir, f.ID, size, featureIsCurrent(f)); err != nil {
			return Stats{}, err
		}
	}
	for _, d := range decisions {
		if err := tallyEntry(kinds, "decision", repoDir, "decisions", d.ID, decisionIsCurrent(d)); err != nil {
			return Stats{}, err
		}
	}
	for _, r := range references {
		if err := tallyEntry(kinds, "reference", repoDir, "references", r.ID, referenceIsCurrent(r)); err != nil {
			return Stats{}, err
		}
	}
	for _, rb := range runbooks {
		if err := tallyEntry(kinds, "runbook", repoDir, "runbooks", rb.ID, runbookIsCurrent(rb)); err != nil {
			return Stats{}, err
		}
	}
	for _, n := range notes {
		if err := tallyEntry(kinds, "note", repoDir, "notes", n.ID, noteIsCurrent(n)); err != nil {
			return Stats{}, err
		}
	}

	s := Stats{Kinds: kinds}
	for _, ks := range kinds {
		s.TotalCount += ks.Count
		s.TotalBytes += ks.Bytes
		s.CurrentCount += ks.CurrentCount
		s.CurrentBytes += ks.CurrentBytes
	}
	s.SavedBytes = s.TotalBytes - s.CurrentBytes
	if s.TotalBytes > 0 {
		s.SavedPct = float64(s.SavedBytes) / float64(s.TotalBytes) * 100
	}
	s.TotalTokensEst = s.TotalBytes / 4
	s.CurrentTokensEst = s.CurrentBytes / 4
	s.SavedTokensEst = s.SavedBytes / 4
	return s, nil
}

// tallyEntry accumulates a single-document entity (content/<subdir>/<id>.md).
func tallyEntry(kinds map[string]KindStats, kind, repoDir, subdir, id string, current bool) error {
	size, err := docSize(filepath.Join(repoDir, "content", subdir, id+".md"))
	if err != nil {
		return err
	}
	return tally(kinds, kind, repoDir, id, size, current)
}

// tally adds one entity to its kind bucket: size plus the entity's errata
// (content/errata/<id>.md, counted with the entity's kind), on the corpus side
// always and on the current side when the materializer would project it.
func tally(kinds map[string]KindStats, kind, repoDir, id string, size int, current bool) error {
	errataBytes, err := docSize(filepath.Join(repoDir, "content", "errata", id+".md"))
	if err != nil {
		return err
	}
	size += errataBytes
	ks := kinds[kind]
	ks.Count++
	ks.Bytes += size
	if current {
		ks.CurrentCount++
		ks.CurrentBytes += size
	}
	kinds[kind] = ks
	return nil
}

// docSize returns the byte size of the document at path; 0 when it does not
// exist (a missing doc is "nothing to read", not an error).
func docSize(path string) (int, error) {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return int(info.Size()), nil
}
