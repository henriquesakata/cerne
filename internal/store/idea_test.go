package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
)

func testIdea(id string) Idea {
	at := time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)
	return Idea{ID: id, Title: "talvez um dia X", State: domain.IdeaActive, CreatedAt: at, UpdatedAt: at}
}

func TestIdeaRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := testIdea("I-0001")
	want.State = domain.IdeaPromoted
	want.Facets = []string{"code"}
	want.Components = []string{"lint"}
	closed := time.Date(2026, 11, 7, 9, 0, 0, 0, time.UTC)
	want.ClosedAt = &closed
	want.Reason = "r"
	want.PromotedTo = "F-0042"
	if err := SaveIdea(dir, want); err != nil {
		t.Fatalf("SaveIdea: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "entities", "ideas", "I-0001.json")); err != nil {
		t.Fatalf("expected entities/ideas/I-0001.json: %v", err)
	}
	got, err := LoadIdea(dir, "I-0001")
	if err != nil {
		t.Fatalf("LoadIdea: %v", err)
	}
	if got.ID != want.ID || got.Title != want.Title || got.State != want.State ||
		got.Reason != want.Reason || got.PromotedTo != want.PromotedTo {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if len(got.Facets) != 1 || got.Facets[0] != "code" || len(got.Components) != 1 || got.Components[0] != "lint" {
		t.Fatalf("tags = %v / %v, want [code] / [lint]", got.Facets, got.Components)
	}
	if !got.CreatedAt.Equal(want.CreatedAt) || !got.UpdatedAt.Equal(want.UpdatedAt) || got.ClosedAt == nil || !got.ClosedAt.Equal(closed) {
		t.Fatalf("times: %v %v %v, want %v / %v", got.CreatedAt, got.UpdatedAt, got.ClosedAt, want.CreatedAt, closed)
	}
}

func TestLoadAllIdeasSortedAndLegacyEmpty(t *testing.T) {
	dir := t.TempDir()
	got, err := LoadAllIdeas(dir)
	if err != nil || len(got) != 0 {
		t.Fatalf("LoadAllIdeas on a backend without ideas/ = %v, %v; want empty, nil", got, err)
	}
	for _, id := range []string{"I-0002", "I-0001"} {
		if err := SaveIdea(dir, testIdea(id)); err != nil {
			t.Fatalf("SaveIdea(%s): %v", id, err)
		}
	}
	got, err = LoadAllIdeas(dir)
	if err != nil {
		t.Fatalf("LoadAllIdeas: %v", err)
	}
	if len(got) != 2 || got[0].ID != "I-0001" || got[1].ID != "I-0002" {
		t.Fatalf("got %+v, want [I-0001 I-0002]", got)
	}
}

func TestNextIDIdeaPrefix(t *testing.T) {
	dir := t.TempDir()
	if err := SavePending(dir, testPending("P-0007")); err != nil {
		t.Fatal(err)
	}
	if err := SaveFeature(dir, Feature{ID: "F-0009"}); err != nil {
		t.Fatal(err)
	}
	if got, err := NextID(dir, "I"); err != nil || got != "I-0001" {
		t.Fatalf("NextID(I) = %q, %v; want I-0001", got, err)
	}
	if err := SaveIdea(dir, testIdea("I-0001")); err != nil {
		t.Fatal(err)
	}
	if got, err := NextID(dir, "I"); err != nil || got != "I-0002" {
		t.Fatalf("NextID(I) = %q, %v; want I-0002", got, err)
	}
}

func TestInitLayoutCreatesIdeasDir(t *testing.T) {
	dir := t.TempDir()
	if err := InitLayout(dir, testProject()); err != nil {
		t.Fatalf("InitLayout: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "entities", "ideas", ".gitkeep")); err != nil {
		t.Fatalf("missing entities/ideas/.gitkeep: %v", err)
	}
}

// TestReadEventsOmitsIdeaEvents (F-0312 crit. 3): the funnel's log never
// carries idea.*; ReadAllEvents is the raw log, in order.
func TestReadEventsOmitsIdeaEvents(t *testing.T) {
	dir := t.TempDir()
	for _, ev := range []struct{ id, typ string }{{"F-0001", "feature.created"}, {"I-0001", "idea.created"}, {"I-0001", "idea.dropped"}} {
		if err := AppendEvent(dir, domain.NewEvent(ev.id, ev.typ, nil, "human")); err != nil {
			t.Fatal(err)
		}
	}
	funnel, err := ReadEvents(dir)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	if len(funnel) != 1 || funnel[0].Type != "feature.created" {
		t.Fatalf("ReadEvents = %+v, want only feature.created", funnel)
	}
	all, err := ReadAllEvents(dir)
	if err != nil {
		t.Fatalf("ReadAllEvents: %v", err)
	}
	if len(all) != 3 || all[0].Type != "feature.created" || all[1].Type != "idea.created" || all[2].Type != "idea.dropped" {
		t.Fatalf("ReadAllEvents = %+v, want the three in order", all)
	}
}
