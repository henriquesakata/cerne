package store

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
)

func TestHistoricalRoundTrip(t *testing.T) {
	dir := t.TempDir()
	h := Historical{
		ID:           "H-0001",
		Name:         "Bills",
		Aliases:      []string{"bills", "boletos"},
		SupersededBy: "Finance",
		Reason:       "absorvido pelo Finance",
		State:        domain.StateSuperseded,
		ImportedFrom: "docs/planning/features.md",
		CreatedAt:    time.Date(2026, 6, 13, 10, 0, 0, 0, time.UTC),
	}
	if err := SaveHistorical(dir, h); err != nil {
		t.Fatalf("SaveHistorical: %v", err)
	}
	got, err := LoadHistorical(dir, "H-0001")
	if err != nil {
		t.Fatalf("LoadHistorical: %v", err)
	}
	if !reflect.DeepEqual(got, h) {
		t.Fatalf("round-trip mismatch:\n got %+v\nwant %+v", got, h)
	}
}

func TestLoadHistoricalNotFound(t *testing.T) {
	if _, err := LoadHistorical(t.TempDir(), "H-9999"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("LoadHistorical missing = %v, want ErrNotFound", err)
	}
}

func TestLoadAllHistoricalsSorted(t *testing.T) {
	dir := t.TempDir()
	for _, id := range []string{"H-0002", "H-0001"} {
		if err := SaveHistorical(dir, Historical{ID: id, Name: id, SupersededBy: "X", Reason: "r", State: domain.StateSuperseded, CreatedAt: time.Unix(0, 0).UTC()}); err != nil {
			t.Fatalf("SaveHistorical %s: %v", id, err)
		}
	}
	all, err := LoadAllHistoricals(dir)
	if err != nil {
		t.Fatalf("LoadAllHistoricals: %v", err)
	}
	if len(all) != 2 || all[0].ID != "H-0001" || all[1].ID != "H-0002" {
		t.Fatalf("LoadAllHistoricals not sorted: %+v", all)
	}
}

func TestLoadAllHistoricalsEmpty(t *testing.T) {
	all, err := LoadAllHistoricals(t.TempDir())
	if err != nil {
		t.Fatalf("LoadAllHistoricals empty: %v", err)
	}
	if len(all) != 0 {
		t.Fatalf("want empty, got %+v", all)
	}
}

func TestNextIDHistoricalPrefix(t *testing.T) {
	dir := t.TempDir()
	first, err := NextID(dir, "H")
	if err != nil {
		t.Fatalf("NextID H (empty): %v", err)
	}
	if first != "H-0001" {
		t.Fatalf("NextID = %q, want H-0001", first)
	}
	if err := SaveHistorical(dir, Historical{ID: first, Name: "Bills", SupersededBy: "X", Reason: "r", State: domain.StateSuperseded, CreatedAt: time.Unix(0, 0).UTC()}); err != nil {
		t.Fatalf("SaveHistorical: %v", err)
	}
	second, err := NextID(dir, "H")
	if err != nil {
		t.Fatalf("NextID H (one): %v", err)
	}
	if second != "H-0002" {
		t.Fatalf("NextID = %q, want H-0002", second)
	}
}
