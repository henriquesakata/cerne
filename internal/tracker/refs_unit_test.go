package tracker_test

import (
	"context"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/tracker"
	"github.com/henriquesakata/cerne/internal/tracker/trackertest"
)

// The audit's two forged refs (finding 5): an id that smuggles a closing
// trailer, a URL that smuggles a co-author.
var forged = []tracker.Ref{
	{Tracker: "pub", ID: "12\nCloses: #1", URL: "https://fake.test/pub/issues/12\nCloses: #1"},
	{Tracker: "pub", ID: "1", URL: "https://fake.test/pub/issues/1\nCo-authored-by: Mallory <m@evil.example>"},
	{Tracker: "pub", ID: "1", URL: "http://fake.test/pub/issues/1"},
}

// badCreate answers Create with a forged ref.
type badCreate struct{ tracker.IssueTracker }

func (b badCreate) Create(context.Context, tracker.Item) (tracker.Ref, error) { return forged[0], nil }

// F-0449: every adapter's refs pass the contract package's check, whatever the
// adapter: a ref with a control character or a URL that is not https never
// leaves an adapter (Create, ListOpen) nor reaches one from the backend.
func TestNewChecksRefs(t *testing.T) {
	ctx := context.Background()
	pub := &trackertest.Fake{Name: "pub"}
	trackertest.Register(t, pub)
	pub.Open("Fine", "body", "")
	pub.Issues["12\nCloses: #1"] = &trackertest.Issue{Title: "Forged", Open: true}
	tr, err := tracker.New(tracker.Config{Name: "pub", Kind: "fake", URL: "https://fake.test/o/pub"})
	if err != nil {
		t.Fatal(err)
	}
	list, err := tr.ListOpen(ctx, "")
	if err != nil || len(list) != 1 || list[0].ID != "1" {
		t.Fatalf("ListOpen = %+v, %v; want only the well-formed issue", list, err)
	}
	for _, r := range forged {
		if s := tr.CommitRef(r, "", false); s != "" {
			t.Errorf("CommitRef(%q) = %q, want none", r.URL, s)
		}
		if tr.Owns(r) {
			t.Errorf("Owns(%q) = true, want false", r.URL)
		}
		if err := tr.Update(ctx, r, tracker.Item{CardID: "F-0001", State: domain.StateDone}); err == nil {
			t.Errorf("Update(%q) succeeded, want refused", r.URL)
		}
	}

	prev, had := tracker.Kinds["badcreate"]
	t.Cleanup(func() {
		if had {
			tracker.Kinds["badcreate"] = prev
		} else {
			delete(tracker.Kinds, "badcreate")
		}
	})
	tracker.Kinds["badcreate"] = func(tracker.Config) (tracker.IssueTracker, error) { return badCreate{pub}, nil }
	bad, err := tracker.New(tracker.Config{Name: "pub", Kind: "badcreate", URL: "https://fake.test/o/pub"})
	if err != nil {
		t.Fatal(err)
	}
	if r, err := bad.Create(ctx, tracker.Item{CardID: "F-0001", State: domain.StateCandidate, Title: "T", Summary: "S"}); err == nil || r != (tracker.Ref{}) {
		t.Fatalf("Create with a forged answer = %+v, %v; want an error and no ref", r, err)
	}
}

// F-0449: the github adapter takes only an all-digit issue number.
func TestGitHubRefusesNonDigitIDs(t *testing.T) {
	installUnitGH(t)
	tr := unitTracker(t, "https://github.com/o/r")
	r := tracker.Ref{Tracker: "gh", ID: "1/../../pulls", URL: "https://github.com/o/r/issues/1/../../pulls"}
	if err := tr.Update(context.Background(), r, tracker.Item{CardID: "F-0001", State: domain.StateDone}); err == nil {
		t.Error("Update with a non-digit id succeeded")
	}
	if s := tr.CommitRef(tracker.Ref{Tracker: "gh", ID: "x1", URL: "https://github.com/o/r/issues/x1"}, "https://github.com/o/r/commit/{sha}", false); s != "" {
		t.Errorf("CommitRef with a non-digit id = %q, want none", s)
	}
}
