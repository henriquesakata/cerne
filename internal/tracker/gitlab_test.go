package tracker_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/tracker"
)

// F-0450 crit. 1: kind "gitlab" is accepted, with the host and the project
// path (subgroups included) read from the record's url, and the unknown-kind
// text lists it. The list is built from KindNames, so a sibling kind (gitea)
// does not break this test.
func TestTrackerKindGitLabAccepted(t *testing.T) {
	for _, u := range []string{
		"https://gitlab.com/g/p",
		"https://gitlab.com/g/sub/p/",
		"https://gitlab.example/g/sub/sub2/p",
	} {
		if _, err := tracker.New(tracker.Config{Name: "gl", Kind: "gitlab", URL: u}); err != nil {
			t.Errorf("New(gitlab, %q) = %v, want an adapter", u, err)
		}
	}
	for _, u := range []string{
		"http://gitlab.com/g/p",
		"https://gitlab.com",
		"https://gitlab.com/",
		"https://gitlab.com/g",
		"https://gitlab.com/g/p/-/issues", // a page of the project, not the project
		"https://gitlab.com/g/../p",
		"https://gitlab.com/g/p?private_token=x",
		"https://gitlab.com/g/p#f",
		"https://127.0.0.1/g/p",
		"https://localhost/g/p",
	} {
		if _, err := tracker.New(tracker.Config{Name: "gl", Kind: "gitlab", URL: u}); !errors.Is(err, tracker.ErrTrackerURL) {
			t.Errorf("New(gitlab, %q) = %v, want errors.Is ErrTrackerURL", u, err)
		}
	}
	kinds := tracker.KindNames()
	if !slices.Contains(kinds, "github") || !slices.Contains(kinds, "gitlab") {
		t.Fatalf("KindNames() = %v, want github and gitlab", kinds)
	}
	_, err := tracker.New(tracker.Config{Name: "j", Kind: "jira", URL: "https://jira.example/PROJ"})
	want := `unknown tracker kind "jira" (supported: ` + strings.Join(kinds, ", ") + ")"
	if !errors.Is(err, tracker.ErrUnknownKind) || err.Error() != want {
		t.Fatalf("New(jira) = %v, want errors.Is ErrUnknownKind with the text %q", err, want)
	}
	t.Logf("unknown kind: %v", err)
}
