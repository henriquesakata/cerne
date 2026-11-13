package app

import (
	"os"
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

// noteAfter reports an active note informing id that was created after at.
func noteAfter(notes []store.Note, id string, at time.Time) bool {
	for _, n := range notes {
		if n.State == domain.NoteActive && n.CreatedAt.After(at) && slices.ContainsFunc(n.Informs, func(i string) bool { return domain.CanonicalID(i) == id }) {
			return true
		}
	}
	return false
}
