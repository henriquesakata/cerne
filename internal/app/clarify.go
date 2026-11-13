package app

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/henriquesakata/cerne/internal/clarify"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// ErrClarificationGone is the sentinel for an answer aimed at a marker that is
// no longer there: the anchor (offset+raw) does not match an open marker in
// the current document — the doc advanced between list and answer, or the
// index never existed. Nothing is written; re-list and retry.
var ErrClarificationGone = errors.New("clarification not found at the given anchor")

// ErrClarificationMoved is the --expect guard's refusal (F-0330): the n-th open
// marker is no longer the one the command was printed for — an answer
// elsewhere renumbered the open markers — so nothing is written.
var ErrClarificationMoved = errors.New("clarification numbering moved")

// ErrEntityNotLive is the sentinel for an answer aimed at a terminal entity:
// its docs are immutable history (errata is the correction path), so open
// markers there are not answerable.
var ErrEntityNotLive = errors.New("entity is not live")

// OpenClarification is one open [ESCLARECER]/[CLARIFY] marker in a live
// entity's document, with the anchor (Doc+Offset+Raw) needed to answer it and
// the 1-based Index that `cerne clarify answer <id> <n>` addresses.
type OpenClarification struct {
	Entity   string   `json:"entity"`
	Doc      string   `json:"doc"` // backend-relative path, e.g. content/features/F-0001/spec.md
	Index    int      `json:"index"`
	Question string   `json:"question"`
	Options  []string `json:"options,omitempty"`
	Offset   int      `json:"offset"`
	Raw      string   `json:"raw"`
}

// Answered reports one resolved clarification back to the caller, for the
// CLI/endpoint echo.
type Answered struct {
	Entity   string `json:"entity"`
	Doc      string `json:"doc"`
	Question string `json:"question"`
	Answer   string `json:"answer"`
}

// entityDoc is one live-entity document eligible for clarification listing —
// the same population the lint text rules scan (scannedDocs), rebuilt here
// over the store so lint internals stay unexported.
type entityDoc struct {
	entity string
	rel    string
}

// liveClarifyFeatureStates mirrors lint's liveFeatureStates: implemented and
// terminal features are settled history, not an answerable work queue.
var liveClarifyFeatureStates = map[domain.FeatureState]bool{
	domain.StateCandidate:     true,
	domain.StateApproved:      true,
	domain.StateInDevelopment: true,
}

// featureDocs lists a live feature's content docs in lifecycle order
// (spec, plan, result, then any extra *.md alphabetically) so clarification
// indexes are stable and follow reading order.
func featureDocs(repoDir, id string) ([]string, error) {
	dir := filepath.Join(repoDir, "content", "features", id)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	present := map[string]bool{}
	var extra []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		present[e.Name()] = true
	}
	lifecycle := []string{"spec.md", "plan.md", "result.md"}
	var names []string
	for _, n := range lifecycle {
		if present[n] {
			names = append(names, n)
			delete(present, n)
		}
	}
	for n := range present {
		extra = append(extra, n)
	}
	sort.Strings(extra)
	return append(names, extra...), nil
}

// clarifyDocs returns the documents eligible for clarification listing. With
// id == "" it walks every live entity (features → decisions → runbooks →
// notes → references, each by id); with an id it returns only that entity's
// docs — empty when the entity is terminal, store.ErrNotFound (wrapped) when
// it does not exist.
func clarifyDocs(repoDir, id string) ([]entityDoc, error) {
	var docs []entityDoc
	addFeature := func(f store.Feature) error {
		if !liveClarifyFeatureStates[f.State] {
			return nil
		}
		names, err := featureDocs(repoDir, f.ID)
		if err != nil {
			return err
		}
		for _, n := range names {
			docs = append(docs, entityDoc{entity: f.ID, rel: "content/features/" + f.ID + "/" + n})
		}
		return nil
	}
	addIfPresent := func(entity, rel string) error {
		_, err := os.Stat(filepath.Join(repoDir, filepath.FromSlash(rel)))
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		docs = append(docs, entityDoc{entity: entity, rel: rel})
		return nil
	}

	if id != "" {
		switch {
		case domain.IsFeatureID(id):
			f, err := store.LoadFeature(repoDir, id)
			if err != nil {
				return nil, err
			}
			return docs, addFeature(f)
		case strings.HasPrefix(id, "D-"):
			d, err := store.LoadDecision(repoDir, id)
			if err != nil {
				return nil, err
			}
			if d.State != domain.DecisionActive {
				return nil, nil
			}
			return docs, addIfPresent(id, "content/decisions/"+id+".md")
		case strings.HasPrefix(id, "RB-"):
			rb, err := store.LoadRunbook(repoDir, id)
			if err != nil {
				return nil, err
			}
			if rb.State != domain.RunbookActive {
				return nil, nil
			}
			return docs, addIfPresent(id, "content/runbooks/"+id+".md")
		case strings.HasPrefix(id, "N-"):
			n, err := store.LoadNote(repoDir, id)
			if err != nil {
				return nil, err
			}
			if n.State != domain.NoteActive {
				return nil, nil
			}
			return docs, addIfPresent(id, "content/notes/"+id+".md")
		case strings.HasPrefix(id, "R-"):
			r, err := store.LoadReference(repoDir, id)
			if err != nil {
				return nil, err
			}
			if r.State != domain.ReferenceCurrent {
				return nil, nil
			}
			return docs, addIfPresent(id, "content/references/"+id+".md")
		default:
			return nil, fmt.Errorf("%w: %s", store.ErrNotFound, id)
		}
	}

	features, err := store.LoadAllFeatures(repoDir)
	if err != nil {
		return nil, err
	}
	sort.Slice(features, func(i, j int) bool { return features[i].ID < features[j].ID })
	for _, f := range features {
		if err := addFeature(f); err != nil {
			return nil, err
		}
	}
	decisions, err := store.LoadAllDecisions(repoDir)
	if err != nil {
		return nil, err
	}
	sort.Slice(decisions, func(i, j int) bool { return decisions[i].ID < decisions[j].ID })
	for _, d := range decisions {
		if d.State != domain.DecisionActive {
			continue
		}
		if err := addIfPresent(d.ID, "content/decisions/"+d.ID+".md"); err != nil {
			return nil, err
		}
	}
	runbooks, err := store.LoadAllRunbooks(repoDir)
	if err != nil {
		return nil, err
	}
	sort.Slice(runbooks, func(i, j int) bool { return runbooks[i].ID < runbooks[j].ID })
	for _, rb := range runbooks {
		if rb.State != domain.RunbookActive {
			continue
		}
		if err := addIfPresent(rb.ID, "content/runbooks/"+rb.ID+".md"); err != nil {
			return nil, err
		}
	}
	notes, err := store.LoadAllNotes(repoDir)
	if err != nil {
		return nil, err
	}
	sort.Slice(notes, func(i, j int) bool { return notes[i].ID < notes[j].ID })
	for _, n := range notes {
		if n.State != domain.NoteActive {
			continue
		}
		if err := addIfPresent(n.ID, "content/notes/"+n.ID+".md"); err != nil {
			return nil, err
		}
	}
	references, err := store.LoadAllReferences(repoDir)
	if err != nil {
		return nil, err
	}
	sort.Slice(references, func(i, j int) bool { return references[i].ID < references[j].ID })
	for _, r := range references {
		if r.State != domain.ReferenceCurrent {
			continue
		}
		if err := addIfPresent(r.ID, "content/references/"+r.ID+".md"); err != nil {
			return nil, err
		}
	}
	return docs, nil
}

// ListClarifications enumerates the open clarification markers across live
// entities (or one entity when id is given), in reading order, with the
// per-entity 1-based index the answer command addresses.
func ListClarifications(repoDir, id string) ([]OpenClarification, error) {
	return listClarifications(repoDir, id, false)
}

// ListResolvedClarifications is the correction queue (F-0197): the same walk,
// but over the markers already answered — the ones a correction can target.
// Same shape and same 1-based per-entity index as the open listing, so the CLI
// addresses both the same way.
func ListResolvedClarifications(repoDir, id string) ([]OpenClarification, error) {
	return listClarifications(repoDir, id, true)
}

func listClarifications(repoDir, id string, resolved bool) ([]OpenClarification, error) {
	docs, err := clarifyDocs(repoDir, id)
	if err != nil {
		return nil, err
	}
	var out []OpenClarification
	indexes := map[string]int{}
	for _, d := range docs {
		data, err := os.ReadFile(filepath.Join(repoDir, filepath.FromSlash(d.rel)))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, c := range clarify.Parse(string(data)) {
			if c.Resolved != resolved {
				continue
			}
			indexes[d.entity]++
			out = append(out, OpenClarification{
				Entity:   d.entity,
				Doc:      d.rel,
				Index:    indexes[d.entity],
				Question: c.Question,
				Options:  c.Options,
				Offset:   c.Offset,
				Raw:      c.Raw,
			})
		}
	}
	return out, nil
}

// AnswerClarificationAt resolves the open marker anchored at (doc, offset,
// raw) in one of id's documents, rewriting exactly that occurrence to its
// resolved vocabulary form and persisting through the standard pipeline —
// SaveFeatureDoc for feature lifecycle docs (the same path as the UI's PUT),
// MutateAndSnapshot for the other entity kinds. ErrClarificationGone when the
// anchor no longer matches an open marker; ErrEntityNotLive for terminal
// entities. It is the page's form: the answer is a human's (F-0375).
func AnswerClarificationAt(repoDir, projDir, id, doc string, offset int, raw, answer string) (Answered, error) {
	return mutateClarificationAt(repoDir, projDir, id, doc, offset, raw, answer, false, HumanAuthor())
}

// CorrectClarificationAt rewrites the RESOLVED marker anchored at (doc, offset,
// raw), appending answer to its trail instead of replacing it (F-0197): the
// document keeps the old answer next to the new one. Same eligibility checks
// and same persistence pipeline as AnswerClarificationAt — ErrClarificationGone
// when the anchor no longer matches a resolved marker (an open one is answered,
// not corrected), ErrEntityNotLive for terminal entities. The page's form: a
// human's correction.
func CorrectClarificationAt(repoDir, projDir, id, doc string, offset int, raw, answer string) (Answered, error) {
	return mutateClarificationAt(repoDir, projDir, id, doc, offset, raw, answer, true, HumanAuthor())
}

// mutateClarificationAt is the shared body of answering and correcting: the two
// differ only in which marker state they target and which rewriter they call.
// by is the writer a feature document's event records (F-0375).
func mutateClarificationAt(repoDir, projDir, id, doc string, offset int, raw, answer string, correcting bool, by DocAuthor) (Answered, error) {
	docs, err := clarifyDocs(repoDir, id)
	if err != nil {
		return Answered{}, err
	}
	eligible := false
	for _, d := range docs {
		if d.rel == doc {
			eligible = true
			break
		}
	}
	if !eligible {
		// The entity exists (clarifyDocs would have failed otherwise) but the
		// doc is not in its answerable set: terminal entity or foreign path.
		if _, statErr := os.Stat(filepath.Join(repoDir, filepath.FromSlash(doc))); statErr == nil {
			return Answered{}, fmt.Errorf("%w: %s", ErrEntityNotLive, id)
		}
		return Answered{}, fmt.Errorf("%w: %s has no doc %s", ErrClarificationGone, id, doc)
	}

	data, err := os.ReadFile(filepath.Join(repoDir, filepath.FromSlash(doc)))
	if err != nil {
		return Answered{}, err
	}
	content := string(data)
	var target *clarify.Clarification
	for _, c := range clarify.Parse(content) {
		if c.Resolved == correcting && c.Offset == offset && c.Raw == raw {
			target = &c
			break
		}
	}
	if target == nil {
		return Answered{}, fmt.Errorf("%w: %s@%d in %s", ErrClarificationGone, raw, offset, doc)
	}
	rewrite := clarify.Resolve
	if correcting {
		rewrite = clarify.Correct
	}
	marker, ok := rewrite(*target, answer)
	if !ok {
		return Answered{}, fmt.Errorf("%w: %s@%d in %s", ErrClarificationGone, raw, offset, doc)
	}
	next := content[:target.Offset] + marker + content[target.Offset+len(target.Raw):]

	if domain.IsFeatureID(id) {
		name := strings.TrimSuffix(filepath.Base(doc), ".md")
		if _, ok := featureDocFilenames[name]; ok {
			if err := SaveFeatureDocBy(repoDir, projDir, id, name, next, by); err != nil {
				return Answered{}, err
			}
			return Answered{Entity: id, Doc: doc, Question: target.Question, Answer: answer}, nil
		}
	}
	err = MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		if err := os.WriteFile(filepath.Join(repoDir, filepath.FromSlash(doc)), []byte(next), 0o644); err != nil {
			return "", err
		}
		return fmt.Sprintf("docs(%s): answer clarification", id), nil
	})
	if err != nil {
		return Answered{}, err
	}
	return Answered{Entity: id, Doc: doc, Question: target.Question, Answer: answer}, nil
}

// markerKey is a short, stable fingerprint of one marker's text — what an
// --expect guard compares (F-0330). Shell-safe and copy-pasteable, unlike the
// question itself.
func markerKey(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:3])
}

// ExpectedClarification refuses when expect is set and c — id's n-th open
// clarification — is not the marker it fingerprints (ErrClarificationMoved).
func ExpectedClarification(id string, n int, c OpenClarification, expect string) error {
	if expect == "" || markerKey(c.Raw) == expect {
		return nil
	}
	q := c.Question
	if q == "" {
		q = c.Raw
	}
	return fmt.Errorf("%w: %s #%d is now %q, not the question this command was printed for — nothing was written; for fresh numbers re-run 'cerne feature approve %s' (or 'cerne clarify list %s')", ErrClarificationMoved, id, n, q, id, id)
}

// AnswerClarification is the index-addressed wrapper the CLI uses: it resolves
// id's n-th open clarification (1-based, ListClarifications order) to its
// anchor and delegates to AnswerClarificationAt — after the expect guard, when
// one is given (ExpectedClarification).
func AnswerClarification(repoDir, projDir, id string, n int, expect, answer string) (Answered, error) {
	items, err := ListClarifications(repoDir, id)
	if err != nil {
		return Answered{}, err
	}
	for _, c := range items {
		if c.Index == n {
			if err := ExpectedClarification(id, n, c, expect); err != nil {
				return Answered{}, err
			}
			return mutateClarificationAt(repoDir, projDir, id, c.Doc, c.Offset, c.Raw, answer, false, docAuthor(projDir))
		}
	}
	return Answered{}, fmt.Errorf("%w: %s has no open clarification #%d", ErrClarificationGone, id, n)
}

// CorrectClarification is the index-addressed wrapper for corrections (F-0197),
// mirroring AnswerClarification over the resolved queue: it resolves id's n-th
// answered clarification (1-based, ListResolvedClarifications order) to its
// anchor and delegates to CorrectClarificationAt.
func CorrectClarification(repoDir, projDir, id string, n int, answer string) (Answered, error) {
	items, err := ListResolvedClarifications(repoDir, id)
	if err != nil {
		return Answered{}, err
	}
	for _, c := range items {
		if c.Index == n {
			return mutateClarificationAt(repoDir, projDir, id, c.Doc, c.Offset, c.Raw, answer, true, docAuthor(projDir))
		}
	}
	return Answered{}, fmt.Errorf("%w: %s has no answered clarification #%d", ErrClarificationGone, id, n)
}
