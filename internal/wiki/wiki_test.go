package wiki

import (
	"reflect"
	"testing"
)

func TestInputHashStableAndOrderIndependent(t *testing.T) {
	a := []Source{{ID: "F-0002", State: "implemented", ContentHash: "bb"}, {ID: "F-0001", State: "approved", ContentHash: "aa"}}
	b := []Source{{ID: "F-0001", State: "approved", ContentHash: "aa"}, {ID: "F-0002", State: "implemented", ContentHash: "bb"}}
	if InputHash(a) != InputHash(b) {
		t.Fatalf("InputHash not order-independent")
	}
	if InputHash(a) == "" {
		t.Fatalf("InputHash empty")
	}
}

func TestInputHashChangesWithState(t *testing.T) {
	base := []Source{{ID: "F-0001", State: "approved", ContentHash: "aa"}}
	changedState := []Source{{ID: "F-0001", State: "superseded", ContentHash: "aa"}}
	changedBody := []Source{{ID: "F-0001", State: "approved", ContentHash: "zz"}}
	if InputHash(base) == InputHash(changedState) {
		t.Fatalf("InputHash unchanged when state changed")
	}
	if InputHash(base) == InputHash(changedBody) {
		t.Fatalf("InputHash unchanged when content changed")
	}
}

func TestProvenanceRoundTrip(t *testing.T) {
	p := Provenance{
		Page:         "finance",
		Sources:      []string{"F-0001", "F-0007"},
		CompiledFrom: "abc123",
		InputHash:    "deadbeef",
	}
	body := RenderProvenance(p) + "\n# Finance\n\nsynthesis here.\n"
	got := ParseProvenance([]byte(body))
	if !reflect.DeepEqual(got, p) {
		t.Fatalf("round-trip mismatch:\n got %+v\nwant %+v", got, p)
	}
}

func TestParseProvenanceEmptySources(t *testing.T) {
	body := RenderProvenance(Provenance{Page: "project", CompiledFrom: "x", InputHash: "y"})
	got := ParseProvenance([]byte(body))
	if len(got.Sources) != 0 {
		t.Fatalf("Sources = %v, want empty", got.Sources)
	}
	if got.Page != "project" {
		t.Fatalf("Page = %q, want project", got.Page)
	}
}
