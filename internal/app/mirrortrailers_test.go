package app

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/tracker"
	"github.com/henriquesakata/cerne/internal/tracker/trackertest"
)

// commitURLEcho is a fake whose trailer shows the commit_url the core handed
// it, so the test sees that [delivery] commit_url reaches CommitRef.
type commitURLEcho struct{ *trackertest.Fake }

func (e commitURLEcho) CommitRef(r tracker.Ref, commitURL string, closing bool) string {
	return "Refs: " + r.ID + " via " + commitURL
}

// F-0449 crit. 7 and 8: MirrorTrailers cites, for each card it is given, the
// issue of that card's own tracker — Refs, or Closes with close_on = "merge" —
// and only those cards: a mirrored card it is not handed (one a commit only
// mentions in its body) gets no trailer. Anything it cannot cite is skipped,
// never an error: the hook is fail-open.
func TestMirrorTrailers(t *testing.T) {
	setup := func(t *testing.T, c mirrorConf) (repoDir, projDir string) {
		t.Helper()
		asHuman(t)
		repoDir, projDir, _ = newFeatureBackend(t)
		pub, second := &trackertest.Fake{Name: "pub"}, &trackertest.Fake{Name: "second"}
		mirrorProject(t, projDir, c, pub, second)
		seedMirrored(t, repoDir, "F-0001", domain.StateInDevelopment, issueOn(t, pub, "F-0001", domain.StateInDevelopment))
		seedMirrored(t, repoDir, "F-0002", domain.StateInDevelopment, issueOn(t, second, "F-0002", domain.StateInDevelopment))
		seedFeatureInState(t, repoDir, "F-0003", domain.StateInDevelopment)
		seedMirrored(t, repoDir, "F-0004", domain.StateInDevelopment, tracker.Ref{Tracker: "gone", ID: "7", URL: "https://fake.test/gone/issues/7"})
		return repoDir, projDir
	}
	const pubRef, secondRef = "https://fake.test/pub/issues/1", "https://fake.test/second/issues/1"

	t.Run("close_on accept", func(t *testing.T) {
		repoDir, projDir := setup(t, mirrorConf{})
		for _, c := range []struct {
			ids  []string
			want []string
		}{
			{[]string{"F-0001"}, []string{"Refs: " + pubRef}},
			{[]string{"F-0002"}, []string{"Refs: " + secondRef}},
			{[]string{"F-0001", "F-0002"}, []string{"Refs: " + pubRef, "Refs: " + secondRef}},
			{[]string{"F-0003"}, nil}, // not mirrored; F-0001 is, but it was not handed over
			{[]string{"F-9999"}, nil}, // no such card
			{[]string{"F-0004"}, nil}, // its tracker is no longer configured
			{nil, nil},
		} {
			if got := MirrorTrailers(repoDir, projDir, c.ids); !slices.Equal(got, c.want) {
				t.Errorf("MirrorTrailers(%v) = %q, want %q", c.ids, got, c.want)
			}
		}
	})
	t.Run("close_on merge", func(t *testing.T) {
		repoDir, projDir := setup(t, mirrorConf{closeOn: "merge"})
		if got, want := MirrorTrailers(repoDir, projDir, []string{"F-0001", "F-0002"}), []string{"Closes: " + pubRef, "Closes: " + secondRef}; !slices.Equal(got, want) {
			t.Fatalf("MirrorTrailers = %q, want %q", got, want)
		}
	})
	t.Run("mirror off", func(t *testing.T) {
		repoDir, projDir := setup(t, mirrorConf{off: true})
		if got := MirrorTrailers(repoDir, projDir, []string{"F-0001"}); len(got) != 0 {
			t.Fatalf("MirrorTrailers with the mirror off = %q, want none", got)
		}
	})
	t.Run("the commit_url reaches the tracker", func(t *testing.T) {
		asHuman(t)
		repoDir, projDir, _ := newFeatureBackend(t)
		pub := &trackertest.Fake{Name: "pub"}
		mirrorProject(t, projDir, mirrorConf{}, pub)
		rc := filepath.Join(projDir, ".cernerc")
		body, err := os.ReadFile(rc)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(rc, append(body, "\n[delivery]\ncommit_url = \"https://github.com/o/r/commit/{sha}\"\n"...), 0o644); err != nil {
			t.Fatal(err)
		}
		prev, had := tracker.Kinds["fake"]
		t.Cleanup(func() {
			if had {
				tracker.Kinds["fake"] = prev
			}
		})
		tracker.Kinds["fake"] = func(c tracker.Config) (tracker.IssueTracker, error) {
			inner, err := prev(c)
			if err != nil {
				return nil, err
			}
			return commitURLEcho{inner.(*trackertest.Fake)}, nil
		}
		seedMirrored(t, repoDir, "F-0001", domain.StateInDevelopment, issueOn(t, pub, "F-0001", domain.StateInDevelopment))
		if got, want := MirrorTrailers(repoDir, projDir, []string{"F-0001"}), []string{"Refs: 1 via https://github.com/o/r/commit/{sha}"}; !slices.Equal(got, want) {
			t.Fatalf("MirrorTrailers = %q, want %q", got, want)
		}
		if got := project.MirrorSettings(projDir); !got.Enabled {
			t.Fatalf("the [delivery] append turned the mirror off: %+v", got)
		}
	})
}
