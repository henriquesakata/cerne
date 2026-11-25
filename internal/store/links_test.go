package store

import (
	"reflect"
	"testing"
	"time"
)

// TestLinkSuggestionsRoundTrip: a missing file is an empty queue; saving
// keeps every field and orders by n.
func TestLinkSuggestionsRoundTrip(t *testing.T) { // F-0502 crit. 5
	dir := t.TempDir()
	got, err := LoadLinkSuggestions(dir)
	if err != nil || len(got) != 0 {
		t.Fatalf("empty = %v, %v; want none", got, err)
	}
	at := time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)
	in := []LinkSuggestion{
		{N: 2, From: "F-0002", To: "D-0001", Excerpt: "b", Status: LinkOpen, CreatedAt: at},
		{N: 1, From: "F-0001", To: "D-0001", Excerpt: "a", Status: LinkAccepted, CreatedAt: at, DecidedAt: at, DecidedBy: "ana"},
	}
	if err := SaveLinkSuggestions(dir, in); err != nil {
		t.Fatal(err)
	}
	got, err = LoadLinkSuggestions(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].N != 1 || got[1].N != 2 || got[0].DecidedBy != "ana" || !got[0].CreatedAt.Equal(at) || got[1].Excerpt != "b" {
		t.Errorf("round trip = %+v", got)
	}
}

// TestConfirmedLinks: only accepted suggestions are relations, keyed by the
// source and sorted without repeats.
func TestConfirmedLinks(t *testing.T) { // F-0502 crit. 6
	dir := t.TempDir()
	err := SaveLinkSuggestions(dir, []LinkSuggestion{
		{N: 1, From: "F-0001", To: "D-0002", Status: LinkAccepted},
		{N: 2, From: "F-0001", To: "D-0001", Status: LinkAccepted},
		{N: 3, From: "F-0001", To: "D-0003", Status: LinkOpen},
		{N: 4, From: "F-0002", To: "D-0001", Status: LinkRejected},
		{N: 5, From: "F-0001", To: "D-0001", Status: LinkAccepted},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := ConfirmedLinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{"F-0001": {"D-0001", "D-0002"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ConfirmedLinks = %v, want %v", got, want)
	}
}
