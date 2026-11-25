package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/forge"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/store"
	"github.com/henriquesakata/cerne/internal/tracker"
	"github.com/henriquesakata/cerne/internal/tracker/trackertest"
)

// B-0079: under mirror.close_on = "merge" a done card's issue waits for its
// delivery to reach the default branch; the merge (or the first mirror round
// after it) closes the issue, never the accept.

const (
	holdPR     = "https://github.com/o/r/pull/7"
	holdCommit = "https://github.com/o/r/commit/abc1234"
)

var (
	prOpen   = forge.PullRequest{Base: "main", DefaultBranch: "main"}
	prMerged = forge.PullRequest{Merged: true, Closed: true, Base: "main", DefaultBranch: "main"}
)

// holdCard is F-0001 in state, mirrored on the fake tracker pub with its issue
// open, carrying the delivery mr/commit given ("" = none), in a project with
// close_on as given and, when withForge, a [delivery] forge answered by f.
func holdCard(t *testing.T, closeOn string, withForge bool, state domain.FeatureState, mr, commit string, f *fakeForge) (repoDir, projDir string, pub *trackertest.Fake, r tracker.Ref) {
	t.Helper()
	asHuman(t)
	repoDir, projDir, _ = newFeatureBackend(t)
	pub = &trackertest.Fake{Name: "pub"}
	mirrorProject(t, projDir, mirrorConf{closeOn: closeOn}, pub)
	if withForge {
		rc := filepath.Join(projDir, ".cernerc")
		data, err := os.ReadFile(rc)
		if err != nil {
			t.Fatal(err)
		}
		data = append(data, "\n[delivery]\ncommit_url = \"https://github.com/o/r/commit/{sha}\"\npr_url = \"https://github.com/o/r/pull/{n}\"\nforge = \"github\"\n"...)
		if err := os.WriteFile(rc, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	useFakeForge(t, f)
	r = issueOn(t, pub, "F-0001", state)
	seedMirrored(t, repoDir, "F-0001", state, r)
	if mr != "" || commit != "" {
		card := reload(t, repoDir, "F-0001")
		card.DeliveryMRURL, card.DeliveryCommitURL = mr, commit
		if err := store.SaveFeature(repoDir, card); err != nil {
			t.Fatal(err)
		}
		if _, err := gitback.CommitAndPush(repoDir, "deliver F-0001"); err != nil {
			t.Fatal(err)
		}
	}
	return repoDir, projDir, pub, r
}

func holdAccept(t *testing.T, repoDir, projDir string) {
	t.Helper()
	out, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionAccept, "", "", nil)
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	if w := mirrorWarnings(out.Warnings); len(w) != 0 {
		t.Fatalf("mirror warnings = %q, want none", w)
	}
}

func holdSync(t *testing.T, repoDir, projDir string) {
	t.Helper()
	rep, err := SyncMirror(repoDir, projDir)
	if err != nil {
		t.Fatalf("SyncMirror: %v", err)
	}
	if len(rep.Warnings) != 0 {
		t.Fatalf("sync warnings = %q, want none", rep.Warnings)
	}
}

func issueOpen(t *testing.T, pub *trackertest.Fake, r tracker.Ref) bool {
	t.Helper()
	return pub.Issues[r.ID].Open
}

// B-0079 crit. 1: accepted while the delivery PR is open, the issue takes the
// done state and stays open.
func TestHoldOpenWhilePROpen(t *testing.T) {
	f := &fakeForge{pr: prOpen}
	repoDir, projDir, pub, r := holdCard(t, "merge", true, domain.StateImplemented, holdPR, holdCommit, f)
	holdAccept(t, repoDir, projDir)
	if c := lastUpdate(t, pub, "F-0001"); !c.Item.HoldOpen || c.Item.State != domain.StateDone {
		t.Fatalf("Update item = %+v, want done with HoldOpen", c.Item)
	}
	if !issueOpen(t, pub, r) || pub.Issues[r.ID].State != domain.StateDone {
		t.Fatalf("issue = %+v, want open in the done state", pub.Issues[r.ID])
	}
	if f.prN != 7 {
		t.Fatalf("forge asked about PR %d, want 7", f.prN)
	}
}

// B-0079 crit. 2: once the forge says the PR merged into the default branch,
// the next sync closes the issue; a merge into another branch is not it.
func TestHoldOpenSyncClosesAfterMerge(t *testing.T) {
	f := &fakeForge{pr: prOpen}
	repoDir, projDir, pub, r := holdCard(t, "merge", true, domain.StateImplemented, holdPR, holdCommit, f)
	holdAccept(t, repoDir, projDir)
	f.pr = forge.PullRequest{Merged: true, Closed: true, Base: "release", DefaultBranch: "main"}
	holdSync(t, repoDir, projDir)
	if !issueOpen(t, pub, r) {
		t.Fatal("a PR merged into release closed the issue; the default branch is main")
	}
	f.pr = prMerged
	holdSync(t, repoDir, projDir)
	if issueOpen(t, pub, r) {
		t.Fatal("the PR merged into main and the sync left the issue open")
	}
}

// B-0079 crit. 3: with only a delivery commit, the issue waits until the
// forge says the commit is on the default branch.
func TestHoldOpenCommitOnly(t *testing.T) {
	f := &fakeForge{on: false, branch: "main"}
	repoDir, projDir, pub, r := holdCard(t, "merge", true, domain.StateImplemented, "", holdCommit, f)
	holdAccept(t, repoDir, projDir)
	if !issueOpen(t, pub, r) {
		t.Fatal("the commit is not on main and the accept closed the issue")
	}
	f.on = true
	holdSync(t, repoDir, projDir)
	if issueOpen(t, pub, r) {
		t.Fatal("the commit reached main and the sync left the issue open")
	}
}

// B-0079 crit. 4: a forge that does not answer, or no [delivery] forge at all,
// holds the issue open, and the accept still succeeds.
func TestHoldOpenInDoubt(t *testing.T) {
	t.Run("forge error", func(t *testing.T) {
		f := &fakeForge{err: errors.New("boom")}
		repoDir, projDir, pub, r := holdCard(t, "merge", true, domain.StateImplemented, holdPR, holdCommit, f)
		holdAccept(t, repoDir, projDir)
		if !issueOpen(t, pub, r) {
			t.Fatal("the forge failed and the accept closed the issue")
		}
	})
	t.Run("no delivery forge", func(t *testing.T) {
		f := &fakeForge{pr: prMerged}
		repoDir, projDir, pub, r := holdCard(t, "merge", false, domain.StateImplemented, holdPR, holdCommit, f)
		holdAccept(t, repoDir, projDir)
		if !issueOpen(t, pub, r) {
			t.Fatal("no forge to ask and the accept closed the issue")
		}
		if f.calls != 0 {
			t.Fatalf("forge called %d times with no [delivery] forge", f.calls)
		}
	})
}

// B-0079 crit. 5: rejected, superseded and a done card with no delivery still
// close the issue on the transition, without asking the forge.
func TestHoldOpenOnlyForDeliveredDone(t *testing.T) {
	t.Run("rejected", func(t *testing.T) {
		f := &fakeForge{pr: prOpen}
		repoDir, projDir, pub, r := holdCard(t, "merge", true, domain.StateCandidate, holdPR, holdCommit, f)
		if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionReject, "", "not now", nil); err != nil {
			t.Fatalf("reject: %v", err)
		}
		if issueOpen(t, pub, r) || f.calls != 0 {
			t.Fatalf("rejected: open=%v, forge calls=%d; want closed, none", issueOpen(t, pub, r), f.calls)
		}
	})
	t.Run("superseded", func(t *testing.T) {
		f := &fakeForge{pr: prOpen}
		repoDir, projDir, pub, r := holdCard(t, "merge", true, domain.StateCandidate, holdPR, holdCommit, f)
		seedFeatureInState(t, repoDir, "F-0002", domain.StateCandidate)
		if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionSupersede, "F-0002", "merged into F-0002", nil); err != nil {
			t.Fatalf("supersede: %v", err)
		}
		if issueOpen(t, pub, r) || f.calls != 0 {
			t.Fatalf("superseded: open=%v, forge calls=%d; want closed, none", issueOpen(t, pub, r), f.calls)
		}
	})
	// B-0079 crit. 7: on a tracker no merge closes (Jira, Trello), done closes
	// the issue on the accept and the forge is not asked.
	t.Run("done on a tracker no merge closes", func(t *testing.T) {
		f := &fakeForge{pr: prOpen}
		repoDir, projDir, pub, r := holdCard(t, "merge", true, domain.StateImplemented, holdPR, holdCommit, f)
		pub.RefsOnly = true
		holdAccept(t, repoDir, projDir)
		if issueOpen(t, pub, r) || f.calls != 0 {
			t.Fatalf("refs-only tracker: open=%v, forge calls=%d; want closed, none", issueOpen(t, pub, r), f.calls)
		}
	})
	t.Run("done with no delivery", func(t *testing.T) {
		f := &fakeForge{pr: prOpen}
		repoDir, projDir, pub, r := holdCard(t, "merge", true, domain.StateImplemented, "", "", f)
		holdAccept(t, repoDir, projDir)
		if issueOpen(t, pub, r) || f.calls != 0 {
			t.Fatalf("done, no delivery: open=%v, forge calls=%d; want closed, none", issueOpen(t, pub, r), f.calls)
		}
	})
}

// B-0079 crit. 6: under close_on = "accept" the accept closes the issue, merged
// or not, and the mirror never asks the forge.
func TestHoldOpenNotUnderCloseOnAccept(t *testing.T) {
	f := &fakeForge{pr: prOpen}
	repoDir, projDir, pub, r := holdCard(t, "", true, domain.StateImplemented, holdPR, holdCommit, f)
	holdAccept(t, repoDir, projDir)
	if issueOpen(t, pub, r) {
		t.Fatal("close_on = accept: the accept left the issue open")
	}
	holdSync(t, repoDir, projDir)
	if f.calls != 0 {
		t.Fatalf("close_on = accept: the mirror asked the forge %d times", f.calls)
	}
}

// B-0079 crit. 1: the opt-in of a card already done, its PR still open,
// publishes the issue open.
func TestHoldOpenOnOptIn(t *testing.T) {
	f := &fakeForge{pr: prOpen}
	repoDir, projDir, pub, _ := holdCard(t, "merge", true, domain.StateImplemented, holdPR, holdCommit, f)
	seedFeatureInState(t, repoDir, "F-0002", domain.StateDone)
	card := reload(t, repoDir, "F-0002")
	card.DeliveryMRURL = holdPR
	if err := store.SaveFeature(repoDir, card); err != nil {
		t.Fatal(err)
	}
	if _, err := gitback.CommitAndPush(repoDir, "deliver F-0002"); err != nil {
		t.Fatal(err)
	}
	got, err := MirrorFeature(repoDir, projDir, "F-0002", "", "Public F-0002", "Public summary of F-0002")
	if err != nil {
		t.Fatalf("MirrorFeature: %v", err)
	}
	if !pub.Issues[got.Mirror.ID].Open {
		t.Fatal("the opt-in of a done card with an open PR published the issue closed")
	}
}

// B-0079: a sync asks the forge only for an issue it did not already see
// closed: a delivered done card whose issue the merge closed costs nothing.
func TestHoldOpenSyncSkipsClosedIssues(t *testing.T) {
	f := &fakeForge{pr: prMerged}
	repoDir, projDir, pub, r := holdCard(t, "merge", true, domain.StateImplemented, holdPR, holdCommit, f)
	holdAccept(t, repoDir, projDir)
	if issueOpen(t, pub, r) {
		t.Fatal("merged before the accept: the accept left the issue open")
	}
	f.calls = 0
	holdSync(t, repoDir, projDir)
	if f.calls != 0 {
		t.Fatalf("the sync saw the issue closed and still asked the forge %d times", f.calls)
	}
}
