package app

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

var t0 = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

const withLearnings = "## Resultado\nok\n\n## Aprendizados e desvios\n- the gate reads the clone\n- second thing\n\n## Gauntlet\nBarra: x\n"

// seedLearnFeature writes a feature with a result and a feature.completed event at done.
func seedLearnFeature(t *testing.T, repoDir, id string, st domain.FeatureState, result string, done time.Time, parent string) {
	t.Helper()
	if err := store.SaveFeature(repoDir, store.Feature{ID: id, Title: "Title " + id, State: st, Parent: parent, CreatedAt: t0, UpdatedAt: t0}); err != nil {
		t.Fatal(err)
	}
	p := FeatureDocPath(repoDir, id, "result.md")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(result), 0o644); err != nil {
		t.Fatal(err)
	}
	ev := domain.NewEvent(id, "feature.completed", nil, "t")
	ev.At = done
	if err := store.AppendEvent(repoDir, ev); err != nil {
		t.Fatal(err)
	}
}

func seedInformingNote(t *testing.T, repoDir, id string, informs []string, st domain.NoteState, created time.Time) {
	t.Helper()
	if err := store.SaveNote(repoDir, store.Note{ID: id, Title: id, State: st, Informs: informs, CreatedAt: created, UpdatedAt: created}); err != nil {
		t.Fatal(err)
	}
}

func listedIDs(r LearningsReport) []string {
	var out []string
	for _, f := range r.Features {
		out = append(out, f.ID)
	}
	return out
}

func TestLearningsTriageRules(t *testing.T) {
	repoDir, _, _ := newFeatureBackend(t)
	seedLearnFeature(t, repoDir, "F-0001", domain.StateImplemented, withLearnings, t0, "")                  // pending, oldest
	seedLearnFeature(t, repoDir, "F-0002", domain.StateDone, withLearnings, t0.Add(1*time.Hour), "")        // pending
	seedLearnFeature(t, repoDir, "F-0003", domain.StateImplemented, withLearnings, t0.Add(2*time.Hour), "") // triaged by a later note
	seedInformingNote(t, repoDir, "N-0001", []string{"F-0003"}, domain.NoteActive, t0.Add(3*time.Hour))
	seedLearnFeature(t, repoDir, "F-0004", domain.StateImplemented, withLearnings, t0.Add(4*time.Hour), "") // earlier note: provenance only
	seedInformingNote(t, repoDir, "N-0002", []string{"F-0004"}, domain.NoteActive, t0.Add(3*time.Hour))
	seedLearnFeature(t, repoDir, "F-0005", domain.StateImplemented, withLearnings, t0.Add(5*time.Hour), "") // triaged by --skip
	if err := store.AppendEvent(repoDir, domain.NewEvent("F-0005", "feature.learnings_triaged", map[string]string{"reason": "x"}, "t")); err != nil {
		t.Fatal(err)
	}
	seedLearnFeature(t, repoDir, "F-0006", domain.StateRejected, withLearnings, t0.Add(6*time.Hour), "")                         // wrong state
	seedLearnFeature(t, repoDir, "E-0007", domain.StateImplemented, withLearnings, t0.Add(7*time.Hour), "")                      // epic
	seedLearnFeature(t, repoDir, "F-0008", domain.StateImplemented, withLearnings, t0.Add(8*time.Hour), "E-0007")                // child of the epic, pending
	seedLearnFeature(t, repoDir, "F-0009", domain.StateImplemented, "## Aprendizados e desvios\nN/A\n", t0.Add(9*time.Hour), "") // no items
	seedLearnFeature(t, repoDir, "F-0010", domain.StateImplemented, withLearnings, t0.Add(10*time.Hour), "")                     // triaged only by a superseded note -> still pending
	seedInformingNote(t, repoDir, "N-0003", []string{"F-0010"}, domain.NoteSuperseded, t0.Add(11*time.Hour))

	r, err := Learnings(repoDir, LearningsOpts{All: true})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"F-0010", "F-0008", "F-0004", "F-0002", "F-0001"}
	if got := listedIDs(r); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("features = %v, want %v", got, want)
	}
	if r.TotalFeatures != 5 || r.TotalItems != 10 {
		t.Errorf("totals = %d features / %d items, want 5 / 10", r.TotalFeatures, r.TotalItems)
	}
	if n := len(r.Features[0].Items); n != 2 || r.Features[0].Items[0].Text != "the gate reads the clone" {
		t.Errorf("items = %+v", r.Features[0].Items)
	}
	if !r.Features[0].CompletedAt.Equal(t0.Add(10 * time.Hour)) {
		t.Errorf("completed_at = %v", r.Features[0].CompletedAt)
	}
}

func TestLearningsLimitSinceFeature(t *testing.T) {
	repoDir, _, _ := newFeatureBackend(t)
	for i := 1; i <= 4; i++ {
		seedLearnFeature(t, repoDir, fmt.Sprintf("F-000%d", i), domain.StateImplemented, withLearnings, t0.Add(time.Duration(i)*24*time.Hour), "")
	}
	r, _ := Learnings(repoDir, LearningsOpts{Limit: 2})
	if fmt.Sprint(listedIDs(r)) != "[F-0004 F-0003]" || r.TotalFeatures != 4 || r.TotalItems != 8 {
		t.Errorf("limit: %v totals %d/%d", listedIDs(r), r.TotalFeatures, r.TotalItems)
	}
	r, _ = Learnings(repoDir, LearningsOpts{All: true, Limit: 1})
	if len(r.Features) != 4 {
		t.Errorf("--all must ignore the limit, got %v", listedIDs(r))
	}
	r, _ = Learnings(repoDir, LearningsOpts{All: true, Since: t0.Add(72 * time.Hour)})
	if fmt.Sprint(listedIDs(r)) != "[F-0004 F-0003]" {
		t.Errorf("since: %v", listedIDs(r))
	}
	r, _ = Learnings(repoDir, LearningsOpts{Feature: "F-0002"})
	if fmt.Sprint(listedIDs(r)) != "[F-0002]" {
		t.Errorf("feature: %v", listedIDs(r))
	}
}

func TestLearningsWritesNothing(t *testing.T) {
	repoDir, _, _ := newFeatureBackend(t)
	seedLearnFeature(t, repoDir, "F-0001", domain.StateImplemented, withLearnings, t0, "")
	before := treeDigest(t, repoDir)
	if _, err := Learnings(repoDir, LearningsOpts{All: true}); err != nil {
		t.Fatal(err)
	}
	if after := treeDigest(t, repoDir); after != before {
		t.Error("Learnings changed the backend clone")
	}
}
