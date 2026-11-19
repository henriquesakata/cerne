package trackertest_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/tracker"
	"github.com/henriquesakata/cerne/internal/tracker/trackertest"
)

func TestFakeContract(t *testing.T) {
	f := &trackertest.Fake{Name: "fake-contract"}
	trackertest.RunContract(t, trackertest.Harness{
		New: func(t *testing.T) tracker.IssueTracker {
			trackertest.Register(t, f)
			tr, err := tracker.New(tracker.Config{Name: f.Name, Kind: "fake", URL: "https://fake.test/o/r"})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			return tr
		},
		Open: func(_ *testing.T, title, body, label string) tracker.Ref {
			return f.Open(title, body, label)
		},
		ClosesOnMerge: true,
	})
}

// TestFakeKnobs pins what the core tests lean on beyond the contract: every
// call is recorded even when it fails, Err and an expired context fail it, the
// superseded comment is one-shot, and Register restores Kinds.
func TestFakeKnobs(t *testing.T) {
	ctx := context.Background()
	f := &trackertest.Fake{Name: "knobs"}
	r, err := f.Create(ctx, tracker.Item{CardID: "F-0001", State: domain.StateInDevelopment, Title: "T", Summary: "S"})
	if err != nil {
		t.Fatal(err)
	}
	succ := &tracker.Ref{Tracker: "knobs", ID: "99", URL: "https://fake.test/knobs/99"}
	gone := tracker.Item{CardID: "F-0001", State: domain.StateSuperseded, Successor: succ}
	for range 2 {
		if err := f.Update(ctx, r, gone); err != nil {
			t.Fatal(err)
		}
	}
	if got := f.Issues[r.ID].Comments; len(got) != 1 || got[0] != "Superseded by "+succ.URL {
		t.Fatalf("comments = %q, want one %q", got, "Superseded by "+succ.URL)
	}

	boom := errors.New("boom")
	f.Err = boom
	if _, err := f.ListOpen(ctx, ""); !errors.Is(err, boom) {
		t.Fatalf("ListOpen with Err = %v, want %v", err, boom)
	}
	f.Err, f.Delay = nil, time.Hour
	short, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
	defer cancel()
	if err := f.Update(short, r, gone); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Update past the deadline = %v, want DeadlineExceeded", err)
	}

	var ops []string
	for _, c := range f.Calls {
		ops = append(ops, c.Op)
	}
	if want := []string{"Create", "Update", "Update", "ListOpen", "Update"}; !slices.Equal(ops, want) {
		t.Fatalf("Calls = %v, want %v", ops, want)
	}

	t.Run("register", func(t *testing.T) {
		trackertest.Register(t, f)
		if _, err := tracker.New(tracker.Config{Name: "nobody", Kind: "fake", URL: "https://fake.test/o/r"}); err == nil {
			t.Fatal("New with a name no registered fake carries succeeded")
		}
	})
	if _, ok := tracker.Kinds["fake"]; ok {
		t.Fatal("Register left kind fake behind after cleanup")
	}
}
