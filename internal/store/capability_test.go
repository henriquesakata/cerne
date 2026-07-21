package store

import (
	"errors"
	"testing"

	"gitlab.com/rockt/cerne/internal/domain"
)

// TestCapabilityRoundTrip pins F-0188 criterion 1 (persistence half): a
// capability saves to entities/capabilities/<ID>.json and loads back intact,
// including the parent link that forms the taxonomy tree.
func TestCapabilityRoundTrip(t *testing.T) {
	dir := t.TempDir()
	c := Capability{
		ID:     "C-0001",
		Title:  "Studio",
		State:  domain.CapabilityActive,
		Parent: "",
	}
	if err := SaveCapability(dir, c); err != nil {
		t.Fatalf("SaveCapability: %v", err)
	}
	child := Capability{ID: "C-0002", Title: "Issues", State: domain.CapabilityActive, Parent: "C-0001"}
	if err := SaveCapability(dir, child); err != nil {
		t.Fatalf("SaveCapability child: %v", err)
	}

	got, err := LoadCapability(dir, "C-0002")
	if err != nil {
		t.Fatalf("LoadCapability: %v", err)
	}
	if got.Title != "Issues" || got.Parent != "C-0001" || got.State != domain.CapabilityActive {
		t.Errorf("round-trip = %+v", got)
	}

	all, err := LoadAllCapabilities(dir)
	if err != nil {
		t.Fatalf("LoadAllCapabilities: %v", err)
	}
	if len(all) != 2 || all[0].ID != "C-0001" || all[1].ID != "C-0002" {
		t.Errorf("LoadAll = %+v, want sorted C-0001, C-0002", all)
	}

	if _, err := LoadCapability(dir, "C-9999"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing capability err = %v, want ErrNotFound", err)
	}
}

// TestCapabilityNextID: the C- prefix participates in the shared NextID
// sequence (max numeric suffix + 1; C-0001 on an empty dir).
func TestCapabilityNextID(t *testing.T) {
	dir := t.TempDir()
	id, err := NextID(dir, "C")
	if err != nil {
		t.Fatalf("NextID empty: %v", err)
	}
	if id != "C-0001" {
		t.Errorf("NextID empty = %q, want C-0001", id)
	}
	if err := SaveCapability(dir, Capability{ID: "C-0007", Title: "x", State: domain.CapabilityActive}); err != nil {
		t.Fatalf("SaveCapability: %v", err)
	}
	id, err = NextID(dir, "C")
	if err != nil {
		t.Fatalf("NextID: %v", err)
	}
	if id != "C-0008" {
		t.Errorf("NextID = %q, want C-0008", id)
	}
}

// TestFeatureCapabilitiesPersist pins F-0188 criterion 2 (persistence half):
// the capabilities list on a feature survives the save/load round-trip.
func TestFeatureCapabilitiesPersist(t *testing.T) {
	dir := t.TempDir()
	f := Feature{
		ID:           "F-0001",
		Title:        "t",
		State:        domain.StateCandidate,
		Capabilities: []string{"C-0001", "C-0002"},
	}
	if err := SaveFeature(dir, f); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}
	got, err := LoadFeature(dir, "F-0001")
	if err != nil {
		t.Fatalf("LoadFeature: %v", err)
	}
	if len(got.Capabilities) != 2 || got.Capabilities[0] != "C-0001" || got.Capabilities[1] != "C-0002" {
		t.Errorf("Capabilities = %v, want [C-0001 C-0002]", got.Capabilities)
	}
}
