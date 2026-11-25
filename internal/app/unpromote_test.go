package app

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/store"
	"github.com/henriquesakata/cerne/internal/tracker"
	"github.com/henriquesakata/cerne/internal/tracker/trackertest"
)

// promotedMirror imports one outside issue as an idea and promotes it: the
// fixture of B-0076, an idea whose card borrowed its issue link.
func promotedMirror(t *testing.T) (repoDir, projDir string, pub *trackertest.Fake, r tracker.Ref, idea store.Idea, f store.Feature) {
	t.Helper()
	asHuman(t)
	repoDir, projDir, _ = newFeatureBackend(t)
	pub = &trackertest.Fake{Name: "pub"}
	mirrorProject(t, projDir, mirrorConf{}, pub)
	r = pub.Open("Crash on start", "Steps: run it twice", "")
	if _, err := SyncMirror(repoDir, projDir); err != nil {
		t.Fatalf("SyncMirror: %v", err)
	}
	idea, ok := ideaByURL(t, repoDir, r.URL)
	if !ok {
		t.Fatal("the outside issue was not imported")
	}
	_, f, err := PromoteIdea(repoDir, projDir, idea.ID, PromoteOptions{})
	if err != nil {
		t.Fatalf("PromoteIdea: %v", err)
	}
	pub.Calls = nil
	return repoDir, projDir, pub, r, idea, f
}

// wantHandedBack: the idea is active again with its link, the card holds none,
// and the outside issue was neither written nor closed.
func wantHandedBack(t *testing.T, repoDir string, pub *trackertest.Fake, r tracker.Ref, idea store.Idea, cardID string) {
	t.Helper()
	got, err := store.LoadIdea(repoDir, idea.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != domain.IdeaActive || !reflect.DeepEqual(got.Mirror, idea.Mirror) {
		t.Fatalf("idea = %s mirror %+v, want active with %+v", got.State, got.Mirror, idea.Mirror)
	}
	if m := reload(t, repoDir, cardID).Mirror; m != nil {
		t.Fatalf("card %s still holds the issue link %+v", cardID, m)
	}
	for _, c := range pub.Calls {
		if c.Op != "ListOpen" {
			t.Fatalf("the tracker was written: %+v", c)
		}
	}
	if !pub.Issues[r.ID].Open {
		t.Fatal("the outside issue was closed")
	}
}

// B-0076 crit. 1: undoing the promotion rejects the minted candidate with an
// automatic reason, hands the link back to the idea, in one commit, and never
// reaches the tracker.
func TestUnpromoteMirroredIdea(t *testing.T) {
	repoDir, projDir, pub, r, idea, f := promotedMirror(t)
	before := commitCount(t, repoDir)
	i, card, err := UnpromoteIdea(repoDir, projDir, idea.ID)
	if err != nil {
		t.Fatalf("UnpromoteIdea: %v", err)
	}
	if i.State != domain.IdeaActive || card.ID != f.ID || card.State != domain.StateRejected {
		t.Fatalf("returned idea %s, card %s %s; want active and %s rejected", i.State, card.ID, card.State, f.ID)
	}
	if got := reload(t, repoDir, f.ID).State; got != domain.StateRejected {
		t.Fatalf("card state = %s, want rejected", got)
	}
	wantHandedBack(t, repoDir, pub, r, idea, f.ID)
	if n := commitCount(t, repoDir) - before; n != 1 {
		t.Fatalf("commits = %d, want 1", n)
	}
	evs, err := store.ReadEvents(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	var reason string
	for _, ev := range evs {
		if ev.EntityID == f.ID && ev.Type == "feature.rejected" {
			reason = ev.Payload["reason"]
		}
	}
	if !strings.Contains(reason, idea.ID) || !strings.Contains(reason, "undone") {
		t.Fatalf("reject reason = %q, want the undone promotion of %s", reason, idea.ID)
	}
}

// B-0076 crit. 2: a card past candidate, or an idea linked rather than
// promoted, is refused with nothing written and the command that fits.
func TestUnpromoteRefuses(t *testing.T) {
	t.Run("card past candidate", func(t *testing.T) {
		repoDir, projDir, _, _, idea, f := promotedMirror(t)
		card := reload(t, repoDir, f.ID)
		card.State = domain.StateApproved
		if err := store.SaveFeature(repoDir, card); err != nil {
			t.Fatal(err)
		}
		if _, err := gitback.CommitAndPush(repoDir, "seed approved "+f.ID); err != nil {
			t.Fatal(err)
		}
		before := commitCount(t, repoDir)
		_, _, err := UnpromoteIdea(repoDir, projDir, idea.ID)
		if !errors.Is(err, ErrPromotionNotUndoable) || !strings.Contains(err.Error(), "cerne feature reject "+f.ID) {
			t.Fatalf("err = %v, want ErrPromotionNotUndoable pointing at 'cerne feature reject %s'", err, f.ID)
		}
		if commitCount(t, repoDir) != before || reload(t, repoDir, f.ID).State != domain.StateApproved {
			t.Fatal("a refused unpromote wrote something")
		}
	})
	t.Run("idea linked, not promoted", func(t *testing.T) {
		asHuman(t)
		repoDir, projDir, _ := newFeatureBackend(t)
		seedFeatureInState(t, repoDir, "F-0001", domain.StateCandidate)
		i, err := CreateIdea(repoDir, projDir, "Export as CSV", "", nil, nil, "", false)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := LinkIdea(repoDir, projDir, i.ID, "F-0001"); err != nil {
			t.Fatal(err)
		}
		before := commitCount(t, repoDir)
		_, _, err = UnpromoteIdea(repoDir, projDir, i.ID)
		if !errors.Is(err, ErrPromotionNotUndoable) || !strings.Contains(err.Error(), "cerne idea unlink "+i.ID) {
			t.Fatalf("err = %v, want ErrPromotionNotUndoable pointing at 'cerne idea unlink %s'", err, i.ID)
		}
		if commitCount(t, repoDir) != before || reload(t, repoDir, "F-0001").State != domain.StateCandidate {
			t.Fatal("a refused unpromote wrote something")
		}
	})
}

// B-0076 crit. 3: unlink hands the issue link back to the idea, so the card
// left live no longer drives the issue, not even when rejected later.
func TestUnlinkHandsMirrorBack(t *testing.T) {
	repoDir, projDir, pub, r, idea, f := promotedMirror(t)
	if _, err := UnlinkIdea(repoDir, projDir, idea.ID); err != nil {
		t.Fatalf("UnlinkIdea: %v", err)
	}
	if _, err := TransitionFeature(repoDir, projDir, f.ID, domain.ActionReject, "", "promoted by mistake", nil); err != nil {
		t.Fatalf("reject: %v", err)
	}
	wantHandedBack(t, repoDir, pub, r, idea, f.ID)
}

// B-0076 crit. 4: rejecting the card reopens the idea with its link, and the
// outside issue stays open: the request is live again (owner, 2026-11-25).
func TestRejectHandsMirrorBack(t *testing.T) {
	repoDir, projDir, pub, r, idea, f := promotedMirror(t)
	if _, err := TransitionFeature(repoDir, projDir, f.ID, domain.ActionReject, "", "promoted by mistake", nil); err != nil {
		t.Fatalf("reject: %v", err)
	}
	wantHandedBack(t, repoDir, pub, r, idea, f.ID)
}

// B-0076 crit. 5: after any way back and a new promotion, one card holds the
// issue link, and the syncs that follow apply only it: the issue is never
// closed and reopened.
func TestRepromoteKeepsOneCardPerIssue(t *testing.T) {
	for name, back := range map[string]func(t *testing.T, repoDir, projDir, ideaID, cardID string){
		"unpromote": func(t *testing.T, repoDir, projDir, ideaID, _ string) {
			if _, _, err := UnpromoteIdea(repoDir, projDir, ideaID); err != nil {
				t.Fatal(err)
			}
		},
		"unlink then reject": func(t *testing.T, repoDir, projDir, ideaID, cardID string) {
			if _, err := UnlinkIdea(repoDir, projDir, ideaID); err != nil {
				t.Fatal(err)
			}
			if _, err := TransitionFeature(repoDir, projDir, cardID, domain.ActionReject, "", "mistake", nil); err != nil {
				t.Fatal(err)
			}
		},
		"reject": func(t *testing.T, repoDir, projDir, _, cardID string) {
			if _, err := TransitionFeature(repoDir, projDir, cardID, domain.ActionReject, "", "mistake", nil); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			repoDir, projDir, pub, r, idea, f := promotedMirror(t)
			back(t, repoDir, projDir, idea.ID, f.ID)
			_, again, err := PromoteIdea(repoDir, projDir, idea.ID, PromoteOptions{})
			if err != nil {
				t.Fatalf("second PromoteIdea: %v", err)
			}
			features, err := store.LoadAllFeatures(repoDir)
			if err != nil {
				t.Fatal(err)
			}
			var holders []string
			for _, c := range features {
				if c.Mirror != nil && c.Mirror.URL == r.URL {
					holders = append(holders, c.ID)
				}
			}
			if !reflect.DeepEqual(holders, []string{again.ID}) {
				t.Fatalf("cards holding %s = %q, want only %s", r.URL, holders, again.ID)
			}
			pub.Calls = nil
			for range 2 {
				if _, err := SyncMirror(repoDir, projDir); err != nil {
					t.Fatalf("SyncMirror: %v", err)
				}
				if !pub.Issues[r.ID].Open {
					t.Fatal("a sync closed the outside issue")
				}
			}
			for _, c := range pub.Calls {
				if c.Op == "Update" && c.Item.CardID != again.ID {
					t.Fatalf("a sync applied %s to the issue: %+v", c.Item.CardID, c)
				}
			}
		})
	}
}
