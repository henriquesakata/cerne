package tracker_test

import (
	"context"
	"errors"
	"strings"
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

// F-0449 (audit finding 6): a credential in a tracker URL is refused for every
// kind, and no error message repeats the user or the password.
func TestNewRefusesCredentialsInURL(t *testing.T) {
	trackertest.Register(t, &trackertest.Fake{Name: "pub"})
	for _, c := range []tracker.Config{
		{Name: "gh", Kind: "github", URL: "https://token@github.com/o/r"},
		{Name: "gh", Kind: "github", URL: "https://u:p@github.com/o/r"},
		{Name: "pub", Kind: "fake", URL: "https://u:p@fake.test/o/pub"},
		{Name: "gh", Kind: "github", URL: "http://token@github.com/o/r"},
		{Name: "gh", Kind: "github", URL: "https://u:p@github.com/o/r/extra"},
	} {
		_, err := tracker.New(c)
		if !errors.Is(err, tracker.ErrTrackerURL) {
			t.Errorf("New(%s %s) = %v, want ErrTrackerURL", c.Kind, c.URL, err)
			continue
		}
		if msg := err.Error(); strings.Contains(msg, "token") || strings.Contains(msg, "p@") {
			t.Errorf("New(%s %s) error repeats the credential: %s", c.Kind, c.URL, msg)
		}
	}
}

// F-0449 fix round 2 (R3): Owns takes the plain issue URL only: a query, a
// fragment or an escaped separator that decodes into the issue's path is
// another page, not this repository's issue.
func TestGitHubOwnsRefusesQueryFragmentAndEscapedSeparators(t *testing.T) {
	tr := unitTracker(t, "https://github.com/o/r")
	for _, u := range []string{
		"https://github.com/o/r/issues/7?x",
		"https://github.com/o/r/issues/7?",
		"https://github.com/o/r/issues/7#y",
		"https://github.com/o%2Fr/issues/7",
		"https://github.com/o/r/issues%2F7",
		"https://github.com/o/r%2fissues/7",
	} {
		if tr.Owns(tracker.Ref{Tracker: "gh", ID: "7", URL: u}) {
			t.Errorf("Owns(%s) = true, want false", u)
		}
	}
	if u := "https://github.com/o/r/issues/7"; !tr.Owns(tracker.Ref{Tracker: "gh", ID: "7", URL: u}) {
		t.Errorf("Owns(%s) = false, want true", u)
	}
}

// foreignAnswers is the fake tracker answering issue URLs on another host:
// well-formed refs its own Owns rejects.
type foreignAnswers struct{ *trackertest.Fake }

func foreign(r tracker.Ref) tracker.Ref {
	r.URL = strings.Replace(r.URL, "https://fake.test/", "https://elsewhere.test/", 1)
	return r
}

func (f foreignAnswers) Create(ctx context.Context, it tracker.Item) (tracker.Ref, error) {
	r, err := f.Fake.Create(ctx, it)
	return foreign(r), err
}

func (f foreignAnswers) ListOpen(ctx context.Context, label string) ([]tracker.Remote, error) {
	list, err := f.Fake.ListOpen(ctx, label)
	for i := range list {
		if list[i].ID == "1" {
			list[i].Ref = foreign(list[i].Ref)
		}
	}
	return list, err
}

// F-0449 fix round 2 (R5): a link the tracker answers outside its own
// repository never reaches the backend: Create refuses it, naming only the
// host, and ListOpen drops it.
func TestNewRefusesRefsOutsideTheRepository(t *testing.T) {
	ctx := context.Background()
	pub := &trackertest.Fake{Name: "pub"}
	prev, had := tracker.Kinds["foreign"]
	t.Cleanup(func() {
		if had {
			tracker.Kinds["foreign"] = prev
		} else {
			delete(tracker.Kinds, "foreign")
		}
	})
	tracker.Kinds["foreign"] = func(tracker.Config) (tracker.IssueTracker, error) { return foreignAnswers{pub}, nil }
	tr, err := tracker.New(tracker.Config{Name: "pub", Kind: "foreign", URL: "https://fake.test/o/pub"})
	if err != nil {
		t.Fatal(err)
	}
	r, err := tr.Create(ctx, tracker.Item{CardID: "F-0001", State: domain.StateCandidate, Title: "T", Summary: "S"})
	if !errors.Is(err, tracker.ErrTrackerURL) || r != (tracker.Ref{}) {
		t.Fatalf("Create answered on another host = %+v, %v; want no ref and ErrTrackerURL", r, err)
	}
	if msg := err.Error(); !strings.Contains(msg, "elsewhere.test") || strings.Contains(msg, "/issues/") {
		t.Errorf("Create error %q, want it to name the host and nothing more of the URL", msg)
	}
	pub.Open("Owned", "body", "")
	list, err := tr.ListOpen(ctx, "")
	if err != nil || len(list) != 1 || list[0].ID != "2" {
		t.Fatalf("ListOpen = %+v, %v; want only issue 2, the one on the tracker's own host", list, err)
	}
}

// F-0449 round 3 (A2): EqualFold folds the Kelvin sign U+212A to k, so a host
// with a non-ASCII byte is refused in a tracker URL and in a ref (punycode is
// required): Owns never sees it.
func TestTrackerRefusesNonASCIIHosts(t *testing.T) {
	_, err := tracker.New(tracker.Config{Name: "gh", Kind: "github", URL: "https://ghe.Kcorp.example/o/r"})
	if !errors.Is(err, tracker.ErrTrackerURL) {
		t.Errorf("New with a Kelvin-sign host = %v, want ErrTrackerURL", err)
	}
	tr := unitTracker(t, "https://ghe.kcorp.example/o/r")
	if tr.Owns(tracker.Ref{Tracker: "gh", ID: "7", URL: "https://ghe.Kcorp.example/o/r/issues/7"}) {
		t.Error("Owns accepted a Kelvin-sign host")
	}
	if !tr.Owns(tracker.Ref{Tracker: "gh", ID: "7", URL: "https://ghe.kcorp.example/o/r/issues/7"}) {
		t.Error("Owns refused the ASCII host")
	}
}

// F-0449 round 3 (A3): an empty host name is local, not a GitHub.
func TestNewRefusesAnEmptyHost(t *testing.T) {
	_, err := tracker.New(tracker.Config{Name: "gh", Kind: "github", URL: "https://:443/o/r"})
	if !errors.Is(err, tracker.ErrTrackerURL) {
		t.Errorf("New(https://:443/o/r) = %v, want ErrTrackerURL", err)
	}
}
