// Package trackertest holds the in-memory tracker the mirror core is tested
// against, and the contract suite every tracker adapter runs (F-0449). It is
// imported only by _test files.
package trackertest

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/tracker"
)

// Fake is an in-memory IssueTracker. Not safe for concurrent use.
type Fake struct {
	Name   string
	Issues map[string]*Issue // by Ref.ID
	Calls  []Call            // Create/Update/ListOpen, in order
	Err    error             // when set, every call returns it
	Delay  time.Duration     // each call waits; an expired ctx returns ctx.Err()
	// RefsOnly makes it a kind no merge closes, like Jira and Trello: CommitRef
	// writes Refs: even when closing, and HoldOpen is ignored (B-0079).
	RefsOnly bool
}

// Issue is one issue on the fake tracker; State stands for its cerne label.
type Issue struct {
	Title, Body, Label string
	State              domain.FeatureState
	Open               bool
	Comments           []string
}

// Call is one recorded tracker call; ListOpen carries neither Ref nor Item.
type Call struct {
	Op   string
	Ref  tracker.Ref
	Item tracker.Item
}

// Create publishes it as a new issue in it.State.
func (f *Fake) Create(ctx context.Context, it tracker.Item) (tracker.Ref, error) {
	if err := f.call(ctx, Call{Op: "Create", Item: it}); err != nil {
		return tracker.Ref{}, err
	}
	return f.add(&Issue{Title: it.Title, Body: it.Body(), State: it.State, Open: open(it.State) || it.HoldOpen && !f.RefsOnly}), nil
}

// Update moves the issue to it.State: an empty Title keeps the outside text,
// NoReopen keeps a closed issue closed, HoldOpen keeps an open one open (the
// merge closes it, B-0079), and the superseded comment is written only by the
// call that closes the issue.
func (f *Fake) Update(ctx context.Context, r tracker.Ref, it tracker.Item) error {
	if err := f.call(ctx, Call{Op: "Update", Ref: r, Item: it}); err != nil {
		return err
	}
	iss, ok := f.Issues[r.ID]
	if !ok {
		return fmt.Errorf("fake tracker %s: no issue %q", f.Name, r.ID)
	}
	if it.Title != "" {
		iss.Title, iss.Body = it.Title, it.Body()
	}
	iss.State = it.State
	if iss.Open && it.State == domain.StateSuperseded && it.Successor != nil {
		iss.Comments = append(iss.Comments, "Superseded by "+it.Successor.URL)
	}
	switch {
	case iss.Open && it.HoldOpen && !f.RefsOnly:
	case iss.Open || !it.NoReopen:
		iss.Open = open(it.State)
	}
	return nil
}

// ListOpen lists the open issues carrying label ("" = all), in id order.
func (f *Fake) ListOpen(ctx context.Context, label string) ([]tracker.Remote, error) {
	if err := f.call(ctx, Call{Op: "ListOpen"}); err != nil {
		return nil, err
	}
	var out []tracker.Remote
	for _, id := range slices.SortedFunc(maps.Keys(f.Issues), byNumber) {
		if iss := f.Issues[id]; iss.Open && (label == "" || iss.Label == label) {
			out = append(out, tracker.Remote{Ref: f.ref(id), Title: iss.Title, Body: iss.Body})
		}
	}
	return out, nil
}

// CommitRef cites the issue by URL; the fake knows no code repository.
func (f *Fake) CommitRef(r tracker.Ref, _ string, closing bool) string {
	if closing && !f.RefsOnly {
		return "Closes: " + r.URL
	}
	return "Refs: " + r.URL
}

// Owns reports whether r is one of this fake's issues, by its URL.
func (f *Fake) Owns(r tracker.Ref) bool {
	id, ok := strings.CutPrefix(r.URL, "https://fake.test/"+f.Name+"/issues/")
	return ok && id != "" && id == r.ID
}

// Open seeds a third-party issue, open, with no cerne state.
func (f *Fake) Open(title, body, label string) tracker.Ref {
	return f.add(&Issue{Title: title, Body: body, Label: label, Open: true})
}

// call records c, then waits Delay or until ctx ends.
func (f *Fake) call(ctx context.Context, c Call) error {
	f.Calls = append(f.Calls, c)
	select {
	case <-time.After(f.Delay):
	case <-ctx.Done():
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return f.Err
}

func (f *Fake) add(iss *Issue) tracker.Ref {
	if f.Issues == nil {
		f.Issues = map[string]*Issue{}
	}
	n := len(f.Issues) + 1
	for f.Issues[strconv.Itoa(n)] != nil { // an issue a test seeded by hand
		n++
	}
	id := strconv.Itoa(n)
	f.Issues[id] = iss
	return f.ref(id)
}

func (f *Fake) ref(id string) tracker.Ref {
	return tracker.Ref{Tracker: f.Name, ID: id, URL: "https://fake.test/" + f.Name + "/issues/" + id}
}

func open(s domain.FeatureState) bool {
	return s != domain.StateDone && s != domain.StateRejected && s != domain.StateSuperseded
}

// byNumber orders numeric ids numerically ("2" before "10").
func byNumber(a, b string) int { return cmp.Or(cmp.Compare(len(a), len(b)), strings.Compare(a, b)) }

// Register installs kind "fake": tracker.New returns the fake named c.Name.
// t.Cleanup restores Kinds.
func Register(t *testing.T, fakes ...*Fake) {
	t.Helper()
	prev, had := tracker.Kinds["fake"]
	tracker.Kinds["fake"] = func(c tracker.Config) (tracker.IssueTracker, error) {
		for _, f := range fakes {
			if f.Name == c.Name {
				return f, nil
			}
		}
		return nil, fmt.Errorf("no fake tracker named %q", c.Name)
	}
	t.Cleanup(func() {
		if had {
			tracker.Kinds["fake"] = prev
		} else {
			delete(tracker.Kinds, "fake")
		}
	})
}

// Harness is what an adapter's test hands RunContract.
type Harness struct {
	New           func(t *testing.T) tracker.IssueTracker
	Open          func(t *testing.T, title, body, label string) tracker.Ref
	ClosesOnMerge bool // the kind writes a closing word: NoReopen and HoldOpen are checked
}

// RunContract checks, through the IssueTracker interface only, the behavior
// the mirror core relies on. Each subtest calls h.New first, then h.Open.
func RunContract(t *testing.T, h Harness) {
	t.Helper()
	item := func(card string, s domain.FeatureState) tracker.Item {
		return tracker.Item{CardID: card, State: s, Title: "Public " + card, Summary: "Public summary of " + card}
	}

	t.Run("create lists the issue", func(t *testing.T) {
		tr := h.New(t)
		it := item("F-9001", domain.StateCandidate)
		r := create(t, tr, it)
		if r.ID == "" || r.URL == "" {
			t.Fatalf("Create returned %+v, want an ID and a URL", r)
		}
		// The sync's idempotency key is the URL stored at Create, matched
		// against what ListOpen reports: the two must be the same Ref.
		got, ok := listed(t, tr, "", r)
		if !ok {
			t.Fatalf("ListOpen does not list the created issue %+v", r)
		}
		if got.Ref != r || got.Title != it.Title || got.Body != it.Body() {
			t.Fatalf("listed %+v, want ref %+v, title %q, body %q", got, r, it.Title, it.Body())
		}
	})

	t.Run("rejected closes and in_development reopens", func(t *testing.T) {
		tr := h.New(t)
		r := createOpen(t, tr, item("F-9002", domain.StateApproved))
		update(t, tr, r, item("F-9002", domain.StateRejected))
		if _, ok := listed(t, tr, "", r); ok {
			t.Fatalf("a rejected card's issue %s is still open", r.ID)
		}
		update(t, tr, r, item("F-9002", domain.StateInDevelopment))
		if _, ok := listed(t, tr, "", r); !ok {
			t.Fatalf("back in_development, issue %s did not reopen", r.ID)
		}
	})

	t.Run("NoReopen keeps a closed issue closed", func(t *testing.T) {
		if !h.ClosesOnMerge {
			t.Skip("the kind writes no closing word")
		}
		tr := h.New(t)
		r := createOpen(t, tr, item("F-9003", domain.StateImplemented))
		update(t, tr, r, item("F-9003", domain.StateDone)) // what the merge's Closes: does
		it := item("F-9003", domain.StateInDevelopment)
		it.NoReopen = true
		update(t, tr, r, it)
		if _, ok := listed(t, tr, "", r); ok {
			t.Fatalf("NoReopen reopened issue %s", r.ID)
		}
	})

	// B-0079: under close_on = "merge" a done card whose delivery has not
	// reached the default branch moves only its labels; once the core stops
	// holding, the same done closes the issue. A kind that writes no closing
	// word ignores the hold: no merge closes its issues.
	t.Run("HoldOpen keeps an open issue open until the merge", func(t *testing.T) {
		tr := h.New(t)
		if !h.ClosesOnMerge {
			held := createOpen(t, tr, item("F-9008", domain.StateImplemented))
			plain := createOpen(t, tr, item("F-9009", domain.StateImplemented))
			it := item("F-9008", domain.StateDone)
			it.HoldOpen = true
			update(t, tr, held, it)
			update(t, tr, plain, item("F-9009", domain.StateDone))
			_, heldOpen := listed(t, tr, "", held)
			if _, plainOpen := listed(t, tr, "", plain); heldOpen != plainOpen {
				t.Fatalf("HoldOpen changed a done issue on a kind that writes no closing word: listed %v, without it %v", heldOpen, plainOpen)
			}
			return
		}
		r := createOpen(t, tr, item("F-9008", domain.StateImplemented))
		it := item("F-9008", domain.StateDone)
		it.HoldOpen, it.NoReopen = true, true
		update(t, tr, r, it)
		if _, ok := listed(t, tr, "", r); !ok {
			t.Fatalf("HoldOpen closed issue %s before the merge", r.ID)
		}
		it.HoldOpen = false
		update(t, tr, r, it)
		if _, ok := listed(t, tr, "", r); ok {
			t.Fatalf("done with the merge confirmed left issue %s open", r.ID)
		}
		// The opt-in of a card already done, its merge still to come.
		it = item("F-9010", domain.StateDone)
		it.HoldOpen, it.NoReopen = true, true
		if c := create(t, tr, it); !func() bool { _, ok := listed(t, tr, "", c); return ok }() {
			t.Fatalf("Create with HoldOpen closed issue %s before the merge", c.ID)
		}
	})

	t.Run("label filters the open list", func(t *testing.T) {
		tr := h.New(t)
		a := h.Open(t, "Outside A", "Body A", "contract-a")
		b := h.Open(t, "Outside B", "Body B", "contract-b")
		if _, ok := listed(t, tr, "contract-a", a); !ok {
			t.Fatalf("ListOpen(contract-a) misses issue %s labeled contract-a", a.ID)
		}
		if _, ok := listed(t, tr, "contract-a", b); ok {
			t.Fatalf("ListOpen(contract-a) lists issue %s labeled contract-b", b.ID)
		}
	})

	t.Run("empty title keeps the outside text", func(t *testing.T) {
		tr := h.New(t)
		r := h.Open(t, "Outside title", "Outside body", "")
		update(t, tr, r, tracker.Item{CardID: "F-9005", State: domain.StateApproved})
		got, ok := listed(t, tr, "", r)
		if !ok {
			t.Fatalf("issue %s is not open after an approved update", r.ID)
		}
		if got.Title != "Outside title" || got.Body != "Outside body" {
			t.Fatalf("outside issue rewritten to title %q, body %q", got.Title, got.Body)
		}
	})

	// A link is resolved by tracker name; a tracker re-pointed elsewhere must
	// not adopt the old issues (F-0449): the core asks Owns before it writes.
	t.Run("owns its own issues only", func(t *testing.T) {
		tr := h.New(t)
		r := create(t, tr, item("F-9007", domain.StateCandidate))
		outside := h.Open(t, "Outside owned", "Body", "")
		if !tr.Owns(r) || !tr.Owns(outside) {
			t.Fatalf("Owns(created %+v) = %v, Owns(outside %+v) = %v; want both true", r, tr.Owns(r), outside, tr.Owns(outside))
		}
		u, err := url.Parse(r.URL)
		if err != nil {
			t.Fatalf("issue URL %q: %v", r.URL, err)
		}
		u.Host = "elsewhere.invalid"
		if foreign := (tracker.Ref{Tracker: r.Tracker, ID: r.ID, URL: u.String()}); tr.Owns(foreign) {
			t.Fatalf("Owns(%+v) = true for an issue on another host", foreign)
		}
	})

	t.Run("commit ref names the issue", func(t *testing.T) {
		tr := h.New(t)
		r := create(t, tr, item("F-9006", domain.StateInDevelopment))
		const otherForge = "https://code.example/o/r/commit/{sha}"
		if s := tr.CommitRef(r, otherForge, false); !strings.HasPrefix(s, "Refs: ") || !strings.Contains(s, r.ID) {
			t.Fatalf("CommitRef(closing=false) = %q, want a Refs: trailer naming %s", s, r.ID)
		}
		if s := tr.CommitRef(r, otherForge, true); h.ClosesOnMerge && (!strings.HasPrefix(s, "Closes: ") || !strings.Contains(s, r.ID)) {
			t.Fatalf("CommitRef(closing=true) = %q, want a Closes: trailer naming %s", s, r.ID)
		}
	})
}

func create(t *testing.T, tr tracker.IssueTracker, it tracker.Item) tracker.Ref {
	t.Helper()
	r, err := tr.Create(context.Background(), it)
	if err != nil {
		t.Fatalf("Create(%s): %v", it.CardID, err)
	}
	return r
}

// createOpen creates it and checks the issue is open, so a later "no longer
// listed" check cannot pass on a tracker that lists nothing.
func createOpen(t *testing.T, tr tracker.IssueTracker, it tracker.Item) tracker.Ref {
	t.Helper()
	r := create(t, tr, it)
	if _, ok := listed(t, tr, "", r); !ok {
		t.Fatalf("a %s card's issue %s is not open after Create", it.State, r.ID)
	}
	return r
}

func update(t *testing.T, tr tracker.IssueTracker, r tracker.Ref, it tracker.Item) {
	t.Helper()
	if err := tr.Update(context.Background(), r, it); err != nil {
		t.Fatalf("Update(%s, %s): %v", r.ID, it.State, err)
	}
}

func listed(t *testing.T, tr tracker.IssueTracker, label string, r tracker.Ref) (tracker.Remote, bool) {
	t.Helper()
	open, err := tr.ListOpen(context.Background(), label)
	if err != nil {
		t.Fatalf("ListOpen(%q): %v", label, err)
	}
	for _, rm := range open {
		if rm.ID == r.ID {
			return rm, true
		}
	}
	return tracker.Remote{}, false
}
