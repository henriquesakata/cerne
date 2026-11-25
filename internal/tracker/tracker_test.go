package tracker

import (
	"reflect"
	"slices"
	"testing"
)

func TestItemBody(t *testing.T) {
	it := Item{CardID: "F-0001", Title: "Public title", Summary: "Public summary"}
	if got, want := it.Body(), "Public summary\n\ncerne F-0001"; got != want {
		t.Fatalf("Body() = %q, want %q", got, want)
	}
}

// TestItemCarriesOnlyPublicFields pins what may leave the backend: a new field
// on Item is a new disclosure and must be decided, not slipped in. HoldOpen
// (B-0079) is an instruction to the adapter, never written to the tracker.
func TestItemCarriesOnlyPublicFields(t *testing.T) {
	want := []string{"CardID", "State", "Title", "Summary", "Successor", "NoReopen", "HoldOpen"}
	typ := reflect.TypeFor[Item]()
	var got []string
	for i := range typ.NumField() {
		got = append(got, typ.Field(i).Name)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("Item fields = %v, want exactly %v", got, want)
	}
}
