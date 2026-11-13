package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

func eventsOf(t *testing.T, repoDir, typ string) []domain.Event {
	t.Helper()
	all, err := store.ReadEvents(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	var out []domain.Event
	for _, e := range all {
		if e.Type == typ {
			out = append(out, e)
		}
	}
	return out
}

func TestSkipLearnings(t *testing.T) {
	t.Setenv("CERNE_ACTOR", "tester")
	repoDir, projDir, _ := newFeatureBackend(t)
	seedLearnFeature(t, repoDir, "F-0001", domain.StateImplemented, withLearnings, t0, "")
	seedLearnFeature(t, repoDir, "F-0002", domain.StateImplemented, "## Aprendizados e desvios\nN/A\n", t0, "")
	commitBackend(t, repoDir)
	at := t0.Add(48 * time.Hour)

	if err := SkipLearnings(repoDir, projDir, "F-0001", "all provenance", at); err != nil {
		t.Fatal(err)
	}
	evs := eventsOf(t, repoDir, "feature.learnings_triaged")
	if len(evs) != 1 || evs[0].EntityID != "F-0001" || evs[0].Payload["reason"] != "all provenance" || evs[0].Actor != "tester" || !evs[0].At.Equal(at) {
		t.Fatalf("event = %+v", evs)
	}
	if r, _ := Learnings(repoDir, LearningsOpts{All: true}); len(r.Features) != 0 {
		t.Errorf("skipped feature still listed: %v", listedIDs(r))
	}

	for _, tt := range []struct {
		id  string
		err error
	}{{"F-0099", ErrNoSuchFeature}, {"F-0002", ErrNoLearnings}, {"F-0001", ErrAlreadyTriaged}} {
		before := treeDigest(t, filepath.Join(repoDir, "events"))
		if err := SkipLearnings(repoDir, projDir, tt.id, "", at); !errors.Is(err, tt.err) {
			t.Errorf("%s: err = %v, want %v", tt.id, err, tt.err)
		}
		if treeDigest(t, filepath.Join(repoDir, "events")) != before {
			t.Errorf("%s: a refused skip wrote to the backend", tt.id)
		}
	}
}

func TestLearningsNothingPendingIsAnEmptyList(t *testing.T) {
	repoDir, _, _ := newFeatureBackend(t)
	r, err := Learnings(repoDir, LearningsOpts{All: true})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(r)
	if !strings.Contains(string(b), `"features":[]`) {
		t.Errorf("json = %s, want \"features\":[]", b)
	}
}

// An imported feature has no completed event: its CreatedAt, which never
// moves, dates the conclusion, so a later edit cannot un-triage it.
func TestLearningsImportedFeatureKeepsItsTriageAfterAnEdit(t *testing.T) {
	repoDir, _, _ := newFeatureBackend(t)
	if err := store.SaveFeature(repoDir, store.Feature{ID: "F-0001", Title: "Old", State: domain.StateImplemented, CreatedAt: t0, UpdatedAt: t0}); err != nil {
		t.Fatal(err)
	}
	p := FeatureDocPath(repoDir, "F-0001", "result.md")
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte(withLearnings), 0o644)
	if r, _ := Learnings(repoDir, LearningsOpts{All: true}); len(r.Features) != 1 || !r.Features[0].CompletedAt.Equal(t0) {
		t.Fatalf("before the note: %+v", r.Features)
	}
	seedInformingNote(t, repoDir, "N-0001", []string{"F-0001"}, domain.NoteActive, t0.Add(time.Hour))
	if err := store.SaveFeature(repoDir, store.Feature{ID: "F-0001", Title: "Old, renamed", State: domain.StateImplemented, CreatedAt: t0, UpdatedAt: t0.Add(24 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if r, _ := Learnings(repoDir, LearningsOpts{All: true}); len(r.Features) != 0 {
		t.Errorf("a later edit reopened a triaged feature: %v", listedIDs(r))
	}
}

func TestLearningsRecompletionReopensTriage(t *testing.T) {
	repoDir, _, _ := newFeatureBackend(t)
	seedLearnFeature(t, repoDir, "F-0001", domain.StateImplemented, withLearnings, t0, "")
	skip := domain.NewEvent("F-0001", eventLearningsTriaged, nil, "t")
	skip.At = t0.Add(time.Hour)
	store.AppendEvent(repoDir, skip)
	if r, _ := Learnings(repoDir, LearningsOpts{All: true}); len(r.Features) != 0 {
		t.Fatal("skipped feature listed")
	}
	again := domain.NewEvent("F-0001", "feature.completed", nil, "t")
	again.At = t0.Add(2 * time.Hour)
	store.AppendEvent(repoDir, again)
	if r, _ := Learnings(repoDir, LearningsOpts{All: true}); len(r.Features) != 1 {
		t.Errorf("a skip older than the latest completion must not count: %v", listedIDs(r))
	}
}

func TestSkipLearningsRefusesWhatIsNotSkippable(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	seedLearnFeature(t, repoDir, "F-0001", domain.StateInDevelopment, withLearnings, t0, "")
	seedLearnFeature(t, repoDir, "F-0002", domain.StateImplemented, withLearnings, t0, "")
	seedLearnFeature(t, repoDir, "F-0003", domain.StateImplemented, withLearnings, t0, "F-0002")
	seedInformingNote(t, repoDir, "N-0001", nil, domain.NoteActive, t0)
	commitBackend(t, repoDir)
	for _, tt := range []struct {
		id  string
		err error
	}{{"N-0001", ErrNotFeatureID}, {"../F-0002", ErrNotFeatureID}, {"F-0001", ErrNotSkippable}, {"F-0002", ErrNotSkippable}} {
		before := treeDigest(t, filepath.Join(repoDir, "events"))
		if err := SkipLearnings(repoDir, projDir, tt.id, "", t0); !errors.Is(err, tt.err) {
			t.Errorf("%s: err = %v, want %v", tt.id, err, tt.err)
		}
		if treeDigest(t, filepath.Join(repoDir, "events")) != before {
			t.Errorf("%s: wrote to the backend", tt.id)
		}
	}
	if err := SkipLearnings(repoDir, projDir, " f-0003 ", "", t0.Add(time.Hour)); err != nil {
		t.Errorf("a lowercase id is canonicalised: %v", err)
	}
}
