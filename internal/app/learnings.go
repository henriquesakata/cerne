package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/restmpl"
	"github.com/henriquesakata/cerne/internal/store"
)

// LearningsOpts selects which pending learnings Learnings lists.
type LearningsOpts struct {
	Feature string
	Since   time.Time
	Limit   int
	All     bool
}

// LearningItem is one learning of a result.
type LearningItem struct {
	Text         string        `json:"text"`
	RelatedNotes []RelatedNote `json:"related_notes"`
}

// RelatedNote is an active note a learning may complement or replace. Via says
// why it matched: "path" (cites a file the learning cites) or "component".
type RelatedNote struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Via   string `json:"via"`
}

// LearningFeature is a concluded feature whose learnings are not triaged.
type LearningFeature struct {
	ID          string         `json:"id"`
	Title       string         `json:"title"`
	CompletedAt time.Time      `json:"completed_at"`
	Items       []LearningItem `json:"items"`
}

// LearningsReport is the pending learnings, newest feature first.
type LearningsReport struct {
	Features      []LearningFeature `json:"features"`
	TotalFeatures int               `json:"total_features"`
	TotalItems    int               `json:"total_items"`
}

// eventLearningsTriaged records that a feature's learnings were looked at and
// nothing in them was durable (F-0401).
const eventLearningsTriaged = "feature.learnings_triaged"

// Learnings lists the concluded features (implemented or done, epics left out)
// whose learnings are not yet triaged, newest first (F-0401). A feature is
// triaged by an active note informing it that is newer than its conclusion, or
// by a feature.learnings_triaged event. It only reads the backend clone.
func Learnings(repoDir string, opts LearningsOpts) (LearningsReport, error) {
	feats, err := store.LoadAllFeatures(repoDir)
	if err != nil {
		return LearningsReport{}, err
	}
	notes, err := store.LoadAllNotes(repoDir)
	if err != nil {
		return LearningsReport{}, err
	}
	events, err := store.ReadEvents(repoDir)
	if err != nil {
		return LearningsReport{}, err
	}
	epics := store.ChildrenByParent(feats)
	done := map[string]time.Time{}
	skipped := map[string]bool{}
	for _, ev := range events {
		switch ev.Type {
		case "feature.completed":
			if ev.At.After(done[ev.EntityID]) {
				done[ev.EntityID] = ev.At
			}
		case eventLearningsTriaged:
			skipped[ev.EntityID] = true
		}
	}
	rel := newRelator(repoDir, notes)
	var rep LearningsReport
	for _, f := range feats {
		if (f.State != domain.StateImplemented && f.State != domain.StateDone) ||
			len(epics[f.ID]) > 0 || skipped[f.ID] || opts.Feature != "" && f.ID != opts.Feature {
			continue
		}
		at, ok := done[f.ID]
		if !ok {
			at = f.UpdatedAt // no completed event (imported history): the last touch stands in
		}
		if at.Before(opts.Since) || noteAfter(notes, f.ID, at) {
			continue
		}
		raw, err := os.ReadFile(FeatureDocPath(repoDir, f.ID, "result.md"))
		if err != nil {
			continue
		}
		texts := restmpl.LearningItems(string(raw))
		if len(texts) == 0 {
			continue
		}
		lf := LearningFeature{ID: f.ID, Title: f.Title, CompletedAt: at}
		for _, t := range texts {
			lf.Items = append(lf.Items, LearningItem{Text: t, RelatedNotes: rel.related(t, f.Components)})
		}
		rep.Features = append(rep.Features, lf)
		rep.TotalItems += len(lf.Items)
	}
	rep.TotalFeatures = len(rep.Features)
	slices.SortFunc(rep.Features, func(a, b LearningFeature) int {
		if c := b.CompletedAt.Compare(a.CompletedAt); c != 0 {
			return c
		}
		return strings.Compare(b.ID, a.ID)
	})
	if !opts.All && opts.Limit > 0 && len(rep.Features) > opts.Limit {
		rep.Features = rep.Features[:opts.Limit]
	}
	return rep, nil
}

// noteAfter reports an active note informing id that was created after at.
func noteAfter(notes []store.Note, id string, at time.Time) bool {
	for _, n := range notes {
		if n.State == domain.NoteActive && n.CreatedAt.After(at) && slices.ContainsFunc(n.Informs, func(i string) bool { return domain.CanonicalID(i) == id }) {
			return true
		}
	}
	return false
}

var (
	// ErrNoSuchFeature is returned when --skip names a feature the backend lacks.
	ErrNoSuchFeature = errors.New("no such feature")
	// ErrNoLearnings is returned when --skip names a feature whose result has no learnings items.
	ErrNoLearnings = errors.New("the feature's result has no learnings items")
	// ErrAlreadyTriaged is returned when the feature already carries a --skip record.
	ErrAlreadyTriaged = errors.New("the feature's learnings were already skipped")
)

// SkipLearnings records that nothing in a feature's learnings is durable: the
// feature.learnings_triaged event takes it off Learnings' list (F-0401).
func SkipLearnings(repoDir, projDir, id, reason string, now time.Time) error {
	return MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		if _, err := store.LoadFeature(repoDir, id); errors.Is(err, store.ErrNotFound) {
			return "", fmt.Errorf("%w: %s", ErrNoSuchFeature, id)
		} else if err != nil {
			return "", err
		}
		raw, _ := os.ReadFile(FeatureDocPath(repoDir, id, "result.md"))
		if len(restmpl.LearningItems(string(raw))) == 0 {
			return "", fmt.Errorf("%w: %s", ErrNoLearnings, id)
		}
		events, err := store.ReadEvents(repoDir)
		if err != nil {
			return "", err
		}
		for _, ev := range events {
			if ev.EntityID == id && ev.Type == eventLearningsTriaged {
				return "", fmt.Errorf("%w: %s", ErrAlreadyTriaged, id)
			}
		}
		ev := domain.NewEvent(id, eventLearningsTriaged, map[string]string{"reason": reason}, EventActor())
		ev.At = now.UTC()
		if err := store.AppendEvent(repoDir, ev); err != nil {
			return "", err
		}
		return "cerne: learnings of " + id + " triaged", nil
	})
}

// maxRelated caps the notes shown under one learning.
const maxRelated = 2

// pathTokenRe finds file-path candidates in prose: a token with a directory and
// an extension, so a bare directory ("internal/") or a bare name never counts.
var pathTokenRe = regexp.MustCompile(`[A-Za-z0-9_.-]+(?:/[A-Za-z0-9_.-]+)+`)

// citedPaths lists the file paths text cites, in order of appearance.
func citedPaths(text string) []string {
	var out []string
	for _, loc := range pathTokenRe.FindAllStringIndex(text, -1) {
		if loc[0] >= 2 && text[loc[0]-2:loc[0]] == "//" || loc[0] >= 1 && text[loc[0]-1] == '/' {
			continue // a URL tail or the middle of a longer path
		}
		p := strings.TrimRight(text[loc[0]:loc[1]], ".")
		if ext := filepath.Ext(p); len(ext) > 1 && !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	return out
}

// relator finds the active notes a learning relates to without the search
// index: notes are listed newest first once, their bodies read on demand.
type relator struct {
	repoDir string
	notes   []store.Note
	bodies  map[string]string
}

func newRelator(repoDir string, all []store.Note) *relator {
	var active []store.Note
	for _, n := range all {
		if n.State == domain.NoteActive {
			active = append(active, n)
		}
	}
	slices.SortFunc(active, func(a, b store.Note) int {
		if c := b.CreatedAt.Compare(a.CreatedAt); c != 0 {
			return c
		}
		return strings.Compare(b.ID, a.ID)
	})
	return &relator{repoDir: repoDir, notes: active, bodies: map[string]string{}}
}

func (r *relator) text(n store.Note) string {
	b, ok := r.bodies[n.ID]
	if !ok {
		b, _ = store.LoadNoteContent(r.repoDir, n.ID)
		b = n.Title + "\n" + b
		r.bodies[n.ID] = b
	}
	return b
}

// related returns up to maxRelated active notes: first those citing a file path
// the item cites, then those sharing a component with the feature, newest first.
func (r *relator) related(item string, comps []string) []RelatedNote {
	out := []RelatedNote{}
	add := func(n store.Note, via string) {
		if len(out) < maxRelated && !slices.ContainsFunc(out, func(o RelatedNote) bool { return o.ID == n.ID }) {
			out = append(out, RelatedNote{ID: n.ID, Title: n.Title, Via: via})
		}
	}
	if paths := citedPaths(item); len(paths) > 0 {
		for _, n := range r.notes {
			body := r.text(n)
			if slices.ContainsFunc(paths, func(p string) bool { return citesPath(body, p) }) {
				add(n, "path")
			}
		}
	}
	for _, n := range r.notes {
		if slices.ContainsFunc(n.Components, func(c string) bool { return slices.Contains(comps, c) }) {
			add(n, "component")
		}
	}
	return out
}
