package app

import (
	"errors"
	"testing"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

func TestSaveHistoricalEntity(t *testing.T) {
	dir := t.TempDir()

	h, created, err := SaveHistoricalEntity(dir, "Bills", "Finance", "absorvido pelo Finance", []string{"bills", "boletos"}, "docs/planning/features.md", "import")
	if err != nil {
		t.Fatalf("SaveHistoricalEntity: %v", err)
	}
	if !created {
		t.Fatalf("created = false, want true on first save")
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

func TestSaveHistoricalEntityUpsert(t *testing.T) {
	dir := t.TempDir()

	first, created, err := SaveHistoricalEntity(dir, "Bills", "Finance", "absorvido", []string{"bills", "boletos"}, "docs/old.md", "cli")
	if err != nil {
		t.Fatalf("first SaveHistoricalEntity: %v", err)
	}
	if !created || first.ID != "H-0001" {
		t.Fatalf("first save = (%+v, created=%v), want H-0001 created", first, created)
	}

	// Re-record the SAME entity (different case + accent in the name, new
	// reason/by, overlapping + new aliases). Must update H-0001 in place.
	updated, created, err := SaveHistoricalEntity(dir, "BÍLLS", "Finance App", "absorvido pelo Finance", []string{"BILLS", "cobranca"}, "docs/new.md", "cli")
	if err != nil {
		t.Fatalf("second SaveHistoricalEntity: %v", err)
	}
	if created {
		t.Fatalf("created = true on re-record, want false (update)")
	}
	if updated.ID != "H-0001" {
		t.Fatalf("update ID = %q, want H-0001 (no new id allocated)", updated.ID)
	}
	if !updated.CreatedAt.Equal(first.CreatedAt) {
		t.Fatalf("CreatedAt changed on update: %v -> %v", first.CreatedAt, updated.CreatedAt)
	}
	if updated.SupersededBy != "Finance App" || updated.Reason != "absorvido pelo Finance" || updated.ImportedFrom != "docs/new.md" {
		t.Fatalf("update did not overwrite mutable fields: %+v", updated)
	}
	// Aliases unioned, order-stable (existing first), deduped case-insensitively
	// ("BILLS" already present as "bills" — kept once, original casing).
	want := []string{"bills", "boletos", "cobranca"}
	if len(updated.Aliases) != len(want) {
		t.Fatalf("aliases = %v, want %v", updated.Aliases, want)
	}
	for i, a := range want {
		if updated.Aliases[i] != a {
			t.Fatalf("aliases = %v, want %v", updated.Aliases, want)
		}
	}

	// Only one entity exists; no H-0002 leaked.
	all, err := store.LoadAllHistoricals(dir)
	if err != nil {
		t.Fatalf("LoadAllHistoricals: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("got %d historicals after re-record, want 1: %+v", len(all), all)
	}

	// Events: one recorded + one updated, both on H-0001.
	evs, err := store.ReadEvents(dir)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	if len(evs) != 2 {
		t.Fatalf("got %d events, want 2: %+v", len(evs), evs)
	}
	if evs[0].Type != "historical.recorded" || evs[0].EntityID != "H-0001" {
		t.Fatalf("event[0] = %+v, want historical.recorded for H-0001", evs[0])
	}
	if evs[1].Type != "historical.updated" || evs[1].EntityID != "H-0001" {
		t.Fatalf("event[1] = %+v, want historical.updated for H-0001", evs[1])
	}
	if evs[1].Payload["superseded_by"] != "Finance App" || evs[1].Payload["reason"] != "absorvido pelo Finance" ||
		evs[1].Payload["aliases"] != "bills,boletos,cobranca" || evs[1].Payload["source"] != "cli" {
		t.Fatalf("update event payload wrong: %+v", evs[1].Payload)
	}

	// A genuinely different name allocates the next id.
	other, created, err := SaveHistoricalEntity(dir, "Investments", "Finance", "absorvido", []string{"investments"}, "", "cli")
	if err != nil {
		t.Fatalf("third SaveHistoricalEntity: %v", err)
	}
	if !created || other.ID != "H-0002" {
		t.Fatalf("different name = (%+v, created=%v), want H-0002 created", other, created)
	}
}

func TestSaveHistoricalEntityValidationLeavesNothing(t *testing.T) {
	dir := t.TempDir()

	_, _, err := SaveHistoricalEntity(dir, "Bills", "Finance", "x", []string{"abc"}, "", "cli") // alias too short
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
