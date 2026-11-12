package app

import (
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/store"
)

// F-0355 A2: the credential goes only to the host of the checkout's origin
// remote; a commit_url naming another host is the crit. 3 path, forge untouched.
func TestDiscoverPRRefusesHostThatIsNotOrigin(t *testing.T) {
	for name, origin := range map[string]string{
		"other host": "https://evil.example/o/r.git",
		"no origin":  "",
		"unparsable": "not a remote",
	} {
		t.Run(name, func(t *testing.T) {
			f := &fakeForge{url: "https://github.com/o/r/pull/12"}
			useFakeForge(t, f)
			useOrigin(t, origin)
			d := Delivery{CommitURL: "https://github.com/o/r/commit/abc1234"}
			got := DiscoverPR(forgeCfg("github"), "p", "F-1", &d)
			want := "cerne: no single PR found for abc1234 (host github.com is not the origin remote); link it with 'cerne feature deliver F-1 --mr <url>'"
			if got != want || d.MRURL != "" || f.calls != 0 {
				t.Fatalf("got %q (calls %d, d %+v)", got, f.calls, d)
			}
		})
	}
}

// F-0355 A2: https, ssh and scp forms of the same origin all match.
func TestDiscoverPRAcceptsOriginForms(t *testing.T) {
	for _, origin := range []string{
		"https://github.com/o/r.git", "https://user@github.com/o/r", "ssh://git@github.com/o/r.git", "git@github.com:o/r.git", "git@GitHub.com:o/r.git",
	} {
		f := &fakeForge{url: "https://github.com/o/r/pull/12"}
		useFakeForge(t, f)
		useOrigin(t, origin)
		d := Delivery{CommitURL: "https://github.com/o/r/commit/abc1234"}
		if line := DiscoverPR(forgeCfg("github"), "p", "F-1", &d); line != "" || d.MRURL == "" {
			t.Errorf("%s: line %q, d %+v", origin, line, d)
		}
	}
}

// F-0355 A2: the accept check stays silent, and never calls the forge, when the
// host is not the origin.
func TestDeliveryBranchWarningNeedsOrigin(t *testing.T) {
	f := &fakeForge{on: false, branch: "main"}
	useFakeForge(t, f)
	useOrigin(t, "https://evil.example/o/r.git")
	feat := store.Feature{ID: "F-1", DeliveryCommitURL: "https://github.com/o/r/commit/abc1234"}
	if got := DeliveryBranchWarning(forgeCfg("github"), "p", feat); got != "" || f.calls != 0 {
		t.Fatalf("got %q, calls %d", got, f.calls)
	}
	if !strings.Contains(remoteHost("git@github.com:o/r.git"), "github.com") {
		t.Fatal("remoteHost scp form")
	}
}
