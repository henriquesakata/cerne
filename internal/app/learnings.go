package app

import (
	"errors"
	"fmt"
	"os"
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
	Text string `json:"text"`
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
	done, skipped := triageEvents(events)
	rep := LearningsReport{Features: []LearningFeature{}}
	for _, f := range feats {
		if (f.State != domain.StateImplemented && f.State != domain.StateDone) ||
			len(epics[f.ID]) > 0 || skippedSince(skipped, done, f.ID) || opts.Feature != "" && f.ID != opts.Feature {
			continue
		}
		at, ok := done[f.ID]
		if !ok {
			at = f.CreatedAt // no completed event (imported history): the creation date, which never moves, stands in
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
			lf.Items = append(lf.Items, LearningItem{Text: t})
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

// triageEvents reads each feature's latest completion and latest skip.
func triageEvents(events []domain.Event) (done, skipped map[string]time.Time) {
	done, skipped = map[string]time.Time{}, map[string]time.Time{}
	for _, ev := range events {
		m := map[string]map[string]time.Time{"feature.completed": done, eventLearningsTriaged: skipped}[ev.Type]
		if m != nil && !ev.At.Before(m[ev.EntityID]) {
			m[ev.EntityID] = ev.At
		}
	}
	return done, skipped
}

// skippedSince reports a skip recorded after the feature's latest completion:
// completing again reopens the triage.
func skippedSince(skipped, done map[string]time.Time, id string) bool {
	at, ok := skipped[id]
	return ok && at.After(done[id])
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

var featureIDRe = regexp.MustCompile(`^F-\d+$`)

var (
	// ErrNoSuchFeature is returned when --skip names a feature the backend lacks.
	ErrNoSuchFeature = errors.New("no such feature")
	// ErrNoLearnings is returned when --skip names a feature whose result has no learnings items.
	ErrNoLearnings = errors.New("the feature's result has no learnings items")
	// ErrAlreadyTriaged is returned when the feature already carries a --skip record.
	ErrAlreadyTriaged = errors.New("the feature's learnings were already skipped")
	// ErrNotFeatureID is returned when --skip names something that is not a feature id.
	ErrNotFeatureID = errors.New("not a feature id")
	// ErrNotSkippable is returned when --skip names an epic or a feature that is not implemented or done.
	ErrNotSkippable = errors.New("only a concluded feature that is not an epic has learnings to skip")
)

// SkipLearnings records that nothing in a feature's learnings is durable: the
// feature.learnings_triaged event takes it off Learnings' list (F-0401).
func SkipLearnings(repoDir, projDir, id, reason string, now time.Time) error {
	id = domain.CanonicalID(id)
	if !featureIDRe.MatchString(id) {
		return fmt.Errorf("%w: %q", ErrNotFeatureID, id)
	}
	return MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		f, err := store.LoadFeature(repoDir, id)
		if errors.Is(err, store.ErrNotFound) {
			return "", fmt.Errorf("%w: %s", ErrNoSuchFeature, id)
		} else if err != nil {
			return "", err
		}
		feats, err := store.LoadAllFeatures(repoDir)
		if err != nil {
			return "", err
		}
		if (f.State != domain.StateImplemented && f.State != domain.StateDone) || len(store.ChildrenByParent(feats)[id]) > 0 {
			return "", fmt.Errorf("%w: %s is %s", ErrNotSkippable, id, f.State)
		}
		raw, _ := os.ReadFile(FeatureDocPath(repoDir, id, "result.md"))
		if len(restmpl.LearningItems(string(raw))) == 0 {
			return "", fmt.Errorf("%w: %s", ErrNoLearnings, id)
		}
		events, err := store.ReadEvents(repoDir)
		if err != nil {
			return "", err
		}
		done, skipped := triageEvents(events)
		if skippedSince(skipped, done, id) {
			return "", fmt.Errorf("%w: %s", ErrAlreadyTriaged, id)
		}
		ev := domain.NewEvent(id, eventLearningsTriaged, map[string]string{"reason": reason}, EventActor())
		ev.At = now.UTC()
		if err := store.AppendEvent(repoDir, ev); err != nil {
			return "", err
		}
		return "cerne: learnings of " + id + " triaged", nil
	})
}
