package app

import (
	"errors"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/store"
)

// TestParseLinkSuggestions: the reply is a JSON array, bare or fenced or
// surrounded by prose; anything else is ErrLinkReply.
func TestParseLinkSuggestions(t *testing.T) { // F-0502 crit. 4
	arr := `[{"from":"F-0001","to":"D-0001","excerpt":"the cache ADR"}]`
	for name, reply := range map[string]string{
		"bare":   arr,
		"fenced": "```json\n" + arr + "\n```",
		"prose":  "Here you go:\n" + arr + "\nHope it helps.",
	} {
		got, err := ParseLinkSuggestions(reply)
		if err != nil || len(got) != 1 || got[0] != (RawLinkSuggestion{"F-0001", "D-0001", "the cache ADR"}) {
			t.Errorf("%s: %+v, %v", name, got, err)
		}
	}
	if got, err := ParseLinkSuggestions("[]"); err != nil || len(got) != 0 {
		t.Errorf("empty = %+v, %v", got, err)
	}
	for _, bad := range []string{"", "no json here", `[{"from":`} {
		if _, err := ParseLinkSuggestions(bad); !errors.Is(err, ErrLinkReply) {
			t.Errorf("%q: err = %v, want ErrLinkReply", bad, err)
		}
	}
}

// TestEnqueueAndDecideLinks: numbers are sequential and never reused, an
// accepted or rejected suggestion keeps its row, and the decision is an event
// on the source.
func TestEnqueueAndDecideLinks(t *testing.T) { // F-0502 crit. 5, 6, 7
	repo := linksRepo(t) // the queue already holds #1 (rejected)
	at := time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)
	added, err := EnqueueLinkSuggestions(repo, []RawLinkSuggestion{
		{"F-0001", "D-0001", "cache ADR"}, {"F-0002", "D-0003", "x"},
	}, at)
	if err != nil || len(added) != 2 || added[0].N != 2 || added[1].N != 3 || added[0].Status != store.LinkOpen {
		t.Fatalf("added = %+v, %v", added, err)
	}

	got, err := DecideLink(repo, 2, store.LinkAccepted, "ana")
	if err != nil || got.Status != store.LinkAccepted || got.DecidedBy != "ana" || got.DecidedAt.IsZero() {
		t.Fatalf("accept = %+v, %v", got, err)
	}
	if _, err := DecideLink(repo, 2, store.LinkAccepted, "ana"); !errors.Is(err, ErrLinkDecided) {
		t.Errorf("accept twice: %v, want ErrLinkDecided", err)
	}
	if _, err := DecideLink(repo, 1, store.LinkAccepted, "ana"); !errors.Is(err, ErrLinkDecided) {
		t.Errorf("accept a rejected one: %v, want ErrLinkDecided", err)
	}
	if _, err := DecideLink(repo, 9, store.LinkRejected, "ana"); !errors.Is(err, ErrLinkUnknown) {
		t.Errorf("unknown n: %v, want ErrLinkUnknown", err)
	}
	if got, err := DecideLink(repo, 2, store.LinkRejected, "ana"); err != nil || got.Status != store.LinkRejected {
		t.Errorf("reject an accepted one = %+v, %v", got, err)
	}
	if _, err := DecideLink(repo, 2, store.LinkRejected, "ana"); !errors.Is(err, ErrLinkDecided) {
		t.Errorf("reject twice: %v, want ErrLinkDecided", err)
	}
	all, _ := store.LoadLinkSuggestions(repo)
	if len(all) != 3 {
		t.Errorf("rows = %d, want 3 (no row is deleted)", len(all))
	}
	// a new number is never a reused one, even with the last row decided
	more, _ := EnqueueLinkSuggestions(repo, []RawLinkSuggestion{{"F-0002", "D-0001", "y"}}, at)
	if len(more) != 1 || more[0].N != 4 {
		t.Errorf("next n = %+v, want 4", more)
	}

	evs, _ := store.ReadEvents(repo)
	types := map[string]int{}
	for _, e := range evs {
		if e.EntityID == "F-0001" {
			types[e.Type]++
		}
	}
	if types["link.accepted"] != 1 || types["link.rejected"] != 1 {
		t.Errorf("events on F-0001 = %v, want one link.accepted and one link.rejected", types)
	}
}
