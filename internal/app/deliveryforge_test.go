package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/forge"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
)

// fakeForge replaces NewDeliveryForge for one test and counts the calls.
type fakeForge struct {
	url    string
	err    error
	on     bool
	branch string
	calls  int
}

func (f *fakeForge) PRForCommit(context.Context, string) (string, int, error) {
	f.calls++
	return f.url, 1, f.err
}

func (f *fakeForge) OnDefaultBranch(context.Context, string) (bool, string, error) {
	f.calls++
	return f.on, f.branch, f.err
}

func useFakeForge(t *testing.T, f *fakeForge) {
	t.Helper()
	old := NewDeliveryForge
	t.Cleanup(func() { NewDeliveryForge = old })
	NewDeliveryForge = func(string, string) (forge.DeliveryForge, error) { return f, nil }
}

func forgeCfg(name string) project.Config {
	return project.Config{Delivery: project.DeliveryConfig{
		CommitURL: "https://github.com/o/r/commit/{sha}", Forge: name, PRURL: "https://github.com/o/r/pull/{n}",
	}}
}

// F-0355 crit. 6: #<n> and !<n> resolve through pr_url; without it, the
// refusal carries the spec's text.
func TestResolveDeliveryShortMRRef(t *testing.T) {
	for _, ref := range []string{"#12", "!12"} {
		d, err := ResolveDelivery(forgeCfg("github"), "", ref)
		if err != nil || d.MRURL != "https://github.com/o/r/pull/12" {
			t.Fatalf("%s: got %+v %v", ref, d, err)
		}
	}
	for _, cfg := range []project.Config{forgeCfg(""), {Delivery: project.DeliveryConfig{PRURL: "https://x/no-placeholder"}}} {
		cfg.Delivery.PRURL = strings.Replace(cfg.Delivery.PRURL, "{n}", "", 1)
		_, err := ResolveDelivery(cfg, "", "#12")
		if !errors.Is(err, ErrDeliveryNoPRTemplate) || err.Error() != "--mr #12 needs [delivery] pr_url in .cernerc" {
			t.Fatalf("err = %v", err)
		}
	}
	// A URL still passes through untouched.
	d, err := ResolveDelivery(forgeCfg("github"), "", "https://github.com/o/r/pull/3")
	if err != nil || d.MRURL != "https://github.com/o/r/pull/3" {
		t.Fatalf("url: %+v %v", d, err)
	}
}

// F-0355 crit. 2: exactly one PR is recorded as the delivery MR.
func TestDiscoverPRFillsMR(t *testing.T) {
	f := &fakeForge{url: "https://github.com/o/r/pull/12"}
	useFakeForge(t, f)
	d := Delivery{CommitURL: "https://github.com/o/r/commit/abc1234"}
	if line := DiscoverPR(forgeCfg("github"), "F-1", &d); line != "" || d.MRURL != f.url {
		t.Fatalf("line %q, d %+v", line, d)
	}
}

// F-0355 crit. 3: error, deadline, zero and several PRs all yield the literal
// stderr line and leave d without an MR.
func TestDiscoverPRFailureLine(t *testing.T) {
	for name, tc := range map[string]struct {
		err    error
		reason string
	}{
		"error":    {errors.New("gh: Not Found (HTTP 404)"), "gh: Not Found (HTTP 404)"},
		"deadline": {fmt.Errorf("gh: %w", context.DeadlineExceeded), "timed out after 5s"},
		"zero":     {fmt.Errorf("%w: no PR contains the commit", forge.ErrNoSinglePR), "no PR contains the commit"},
		"two":      {fmt.Errorf("%w: 2 PRs contain the commit", forge.ErrNoSinglePR), "2 PRs contain the commit"},
		"nocreds":  {errors.New("no credentials for git.rockt.com.br"), "no credentials for git.rockt.com.br"},
	} {
		t.Run(name, func(t *testing.T) {
			useFakeForge(t, &fakeForge{err: tc.err})
			d := Delivery{CommitURL: "https://github.com/o/r/commit/abc1234"}
			want := "cerne: no single PR found for abc1234 (" + tc.reason + "); link it with 'cerne feature deliver F-1 --mr <url>'"
			if got := DiscoverPR(forgeCfg("github"), "F-1", &d); got != want || d.MRURL != "" {
				t.Fatalf("got %q (d %+v), want %q", got, d, want)
			}
		})
	}
}

// F-0355 crit. 8: without [delivery] forge — or with --mr already given, or no
// commit — the forge is never built, let alone called.
func TestDiscoverPRSkips(t *testing.T) {
	f := &fakeForge{url: "https://github.com/o/r/pull/12"}
	useFakeForge(t, f)
	old := NewDeliveryForge
	NewDeliveryForge = func(string, string) (forge.DeliveryForge, error) {
		t.Fatal("forge built when it must not be")
		return nil, nil
	}
	t.Cleanup(func() { NewDeliveryForge = old })
	commit := "https://github.com/o/r/commit/abc1234"
	for name, tc := range map[string]struct {
		cfg project.Config
		d   Delivery
	}{
		"no forge":  {forgeCfg(""), Delivery{CommitURL: commit}},
		"has mr":    {forgeCfg("github"), Delivery{CommitURL: commit, MRURL: "https://x/pull/1"}},
		"no commit": {forgeCfg("github"), Delivery{}},
	} {
		d := tc.d
		if line := DiscoverPR(tc.cfg, "F-1", &d); line != "" || d != tc.d {
			t.Fatalf("%s: line %q, d %+v", name, line, d)
		}
	}
	if f.calls != 0 {
		t.Fatalf("forge called %d times", f.calls)
	}
}

// F-0355 crit. 7: a commit outside the default branch warns; inside, none.
func TestDeliveryBranchWarning(t *testing.T) {
	feat := store.Feature{ID: "F-1", DeliveryCommitURL: "https://github.com/o/r/commit/abc1234"}
	useFakeForge(t, &fakeForge{on: false, branch: "main"})
	want := "cerne: delivery commit abc1234 is not on main; the PR may have merged into another branch"
	if got := DeliveryBranchWarning(forgeCfg("github"), feat); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	useFakeForge(t, &fakeForge{on: true, branch: "main"})
	if got := DeliveryBranchWarning(forgeCfg("github"), feat); got != "" {
		t.Fatalf("inside the default branch: got %q", got)
	}
	// A forge that cannot answer, no forge and no commit stay silent.
	useFakeForge(t, &fakeForge{err: errors.New("boom")})
	if got := DeliveryBranchWarning(forgeCfg("github"), feat); got != "" {
		t.Fatalf("forge error: got %q", got)
	}
	if got := DeliveryBranchWarning(forgeCfg(""), feat); got != "" {
		t.Fatalf("no forge: got %q", got)
	}
	if got := DeliveryBranchWarning(forgeCfg("github"), store.Feature{ID: "F-1"}); got != "" {
		t.Fatalf("no commit: got %q", got)
	}
}
