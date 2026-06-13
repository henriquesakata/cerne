package app

import (
	"errors"
	"testing"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

func TestSaveHistoricalEntity(t *testing.T) {
	dir := t.TempDir()

	h, err := SaveHistoricalEntity(dir, "Bills", "Finance", "absorvido pelo Finance", []string{"bills", "boletos"}, "docs/planning/features.md", "import")
	if err != nil {
		t.Fatalf("SaveHistoricalEntity: %v", err)
	}
	if h.ID != "H-0001" {
		t.Fatalf("ID = %q, want H-0001", h.ID)
	}
	if h.State != domain.StateSuperseded {
		t.Fatalf("State = %q, want superseded", h.State)
	}

	got, err := store.LoadHistorical(dir, "H-0001")
	if err != nil {
		t.Fatalf("LoadHistorical: %v", err)
	}
	if got.Name != "Bills" || got.SupersededBy != "Finance" || got.ImportedFrom != "docs/planning/features.md" {
		t.Fatalf("persisted entity wrong: %+v", got)
	}

	evs, err := store.ReadEvents(dir)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("got %d events, want 1", len(evs))
	}
	ev := evs[0]
	if ev.Type != "historical.recorded" || ev.EntityID != "H-0001" {
		t.Fatalf("event = %+v, want historical.recorded for H-0001", ev)
	}
	if ev.Payload["name"] != "Bills" || ev.Payload["aliases"] != "bills,boletos" ||
		ev.Payload["superseded_by"] != "Finance" || ev.Payload["source"] != "import" {
		t.Fatalf("event payload wrong: %+v", ev.Payload)
	}
}

func TestSaveHistoricalEntityValidationLeavesNothing(t *testing.T) {
	dir := t.TempDir()

	_, err := SaveHistoricalEntity(dir, "Bills", "Finance", "x", []string{"abc"}, "", "cli") // alias too short
	if !errors.Is(err, domain.ErrAliasTooShort) {
		t.Fatalf("err = %v, want errors.Is ErrAliasTooShort", err)
	}

	all, err := store.LoadAllHistoricals(dir)
	if err != nil {
		t.Fatalf("LoadAllHistoricals: %v", err)
	}
	if len(all) != 0 {
		t.Fatalf("validation failure persisted entities: %+v", all)
	}
	evs, err := store.ReadEvents(dir)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	if len(evs) != 0 {
		t.Fatalf("validation failure appended events: %+v", evs)
	}
}
