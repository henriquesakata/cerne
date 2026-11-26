package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/forge"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
)

// mergedPR is a PR the human merged into the default branch.
var mergedPR = forge.PullRequest{Merged: true, Closed: true, MergedAt: "2026-11-21T14:38:57Z", Base: "main", DefaultBranch: "main"}

// mergeCfg is a project under the merge gate with a GitHub delivery forge.
func mergeCfg() project.Config {
	c := forgeCfg("github")
	c.Gates.Accept = project.GateMerge
	return c
}

// seedCards persists the cards as given and commits them, so the transition's
// rebase onto origin keeps them.
func seedCards(t *testing.T, repoDir string, cards ...store.Feature) {
	t.Helper()
	for _, f := range cards {
		if f.Title == "" {
			f.Title = "Card " + f.ID
		}
		if err := store.SaveFeature(repoDir, f); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := gitback.CommitAndPush(repoDir, "seed cards"); err != nil {
		t.Fatal(err)
	}
}

func prURL(n string) string { return "https://github.com/o/r/pull/" + n }

func checkFor(t *testing.T, checks []MergeCheck, id string) MergeCheck {
	t.Helper()
	for _, c := range checks {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("no check for %s in %+v", id, checks)
	return MergeCheck{}
}

// lastEventOf is the last event the log holds for id.
func lastEventOf(t *testing.T, repoDir, id string) domain.Event {
	t.Helper()
	evs, err := store.ReadEvents(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i].EntityID == id {
			return evs[i]
		}
	}
	t.Fatalf("no event for %s", id)
	return domain.Event{}
}

// The sweep examines every implemented card: the merged one goes to done,
// the open one and the one without a PR stay, a done card is not examined.
// F-0488 crit. 3
func TestAcceptMergedSweep(t *testing.T) {
	t.Setenv("CERNE_ACTOR", "agent")
	repoDir, projDir, _ := newFeatureBackend(t)
	seedCards(t, repoDir,
		store.Feature{ID: "F-0001", State: domain.StateImplemented, DeliveryMRURL: prURL("1")},
		store.Feature{ID: "F-0002", State: domain.StateImplemented, DeliveryMRURL: prURL("2")},
		store.Feature{ID: "B-0003", State: domain.StateImplemented, DeliveryCommitURL: "https://github.com/o/r/commit/abc1234"},
		store.Feature{ID: "F-0004", State: domain.StateDone, DeliveryMRURL: prURL("4")},
	)
	useFakeForge(t, &fakeForge{prs: map[int]forge.PullRequest{1: mergedPR, 2: {Base: "main", DefaultBranch: "main"}}})

	checks, err := AcceptMerged(repoDir, projDir, mergeCfg(), nil)
	if err != nil {
		t.Fatalf("AcceptMerged: %v", err)
	}
	if len(checks) != 3 {
		t.Fatalf("checks = %+v, want the three implemented cards", checks)
	}
	if c := checkFor(t, checks, "F-0001"); !c.Accepted || c.Line != "F-0001: implemented → done (accepted on merge of "+prURL("1")+")" {
		t.Fatalf("F-0001 check = %+v", c)
	}
	if c := checkFor(t, checks, "F-0002"); c.Accepted || !strings.Contains(c.Line, "still open") {
		t.Fatalf("F-0002 check = %+v", c)
	}
	if c := checkFor(t, checks, "B-0003"); c.Accepted || !strings.Contains(c.Line, "cerne feature deliver B-0003 --mr <url>") {
		t.Fatalf("B-0003 check = %+v", c)
	}
	for id, want := range map[string]domain.FeatureState{"F-0001": domain.StateDone, "F-0002": domain.StateImplemented, "B-0003": domain.StateImplemented} {
		if got := reload(t, repoDir, id).State; got != want {
			t.Errorf("%s state = %s, want %s", id, got, want)
		}
	}
}

// With ids, only those cards are examined.
// F-0488 crit. 3
func TestAcceptMergedByID(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	seedCards(t, repoDir,
		store.Feature{ID: "F-0001", State: domain.StateImplemented, DeliveryMRURL: prURL("1")},
		store.Feature{ID: "F-0002", State: domain.StateImplemented, DeliveryMRURL: prURL("2")},
	)
	f := &fakeForge{pr: mergedPR}
	useFakeForge(t, f)

	checks, err := AcceptMerged(repoDir, projDir, mergeCfg(), []string{"F-0002"})
	if err != nil {
		t.Fatalf("AcceptMerged: %v", err)
	}
	if len(checks) != 1 || checks[0].ID != "F-0002" || !checks[0].Accepted || f.prN != 2 {
		t.Fatalf("checks = %+v (asked PR %d), want only F-0002, accepted on PR 2", checks, f.prN)
	}
	if got := reload(t, repoDir, "F-0001").State; got != domain.StateImplemented {
		t.Fatalf("F-0001 state = %s, want implemented: it was not asked", got)
	}
	if _, err := AcceptMerged(repoDir, projDir, mergeCfg(), []string{"F-0099"}); err == nil {
		t.Fatal("an unknown id must fail")
	}
}

// The event records the human's merge: via, PR, merge time, actor human, and
// the shell that transcribed it apart; nothing else, no forge login.
// F-0488 crit. 4
func TestAcceptMergedEventPayload(t *testing.T) {
	t.Setenv("CERNE_ACTOR", "agent")
	repoDir, projDir, _ := newFeatureBackend(t)
	seedCards(t, repoDir, store.Feature{ID: "F-0001", State: domain.StateImplemented, DeliveryMRURL: prURL("158")})
	useFakeForge(t, &fakeForge{pr: mergedPR})

	if _, err := AcceptMerged(repoDir, projDir, mergeCfg(), []string{"F-0001"}); err != nil {
		t.Fatal(err)
	}
	ev := lastEventOf(t, repoDir, "F-0001")
	if ev.Type != "feature.accepted" || ev.Actor != "human" {
		t.Fatalf("event = %+v, want feature.accepted by human", ev)
	}
	keys := make([]string, 0, len(ev.Payload))
	for k := range ev.Payload {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	// F-0516: every accept also records the Gauntlet round it saw.
	if want := []string{"gauntlet_round", "merged_at", "pr", "recorded_by", "via"}; !slices.Equal(keys, want) {
		t.Fatalf("payload keys = %v, want %v", keys, want)
	}
	if ev.Payload["via"] != "merge" || ev.Payload["pr"] != prURL("158") || ev.Payload["merged_at"] != mergedPR.MergedAt || !strings.HasPrefix(ev.Payload["recorded_by"], "agent:") {
		t.Fatalf("payload = %v", ev.Payload)
	}
	if got := reload(t, repoDir, "F-0001"); got.State != domain.StateDone || got.AcceptedVia != "merge" {
		t.Fatalf("entity = %+v, want done with accepted_via merge", got)
	}
}

// Every reason not to accept leaves the card implemented, with one line.
// F-0488 crit. 6
func TestAcceptMergedRefusals(t *testing.T) {
	cases := []struct {
		name   string
		mr     string
		pr     forge.PullRequest
		err    error
		origin string
		want   string
	}{
		{name: "open", mr: prURL("1"), pr: forge.PullRequest{Base: "main", DefaultBranch: "main"}, want: prURL("1") + " is still open"},
		{name: "closed", mr: prURL("1"), pr: forge.PullRequest{Closed: true, Base: "main", DefaultBranch: "main"}, want: "closed without merge: a human accepts with 'cerne feature accept F-0001'"},
		{name: "stacked", mr: prURL("1"), pr: forge.PullRequest{Merged: true, Closed: true, Base: "feat/a", DefaultBranch: "main"}, want: "merged into feat/a, not into main"},
		{name: "other repository", mr: "https://github.com/x/y/pull/1", pr: mergedPR, want: "not in the [delivery] commit_url repository"},
		{name: "not origin", mr: prURL("1"), pr: mergedPR, origin: "git@gitlab.com:o/r.git", want: "host github.com is not the origin remote"},
		{name: "not a PR URL", mr: "https://github.com/o/r/commit/abc1234", pr: mergedPR, want: "is not a PR URL"},
		{name: "forge timeout", mr: prURL("1"), err: context.DeadlineExceeded, want: "the forge did not answer"},
		{name: "forge error", mr: prURL("1"), err: errors.New("gh: HTTP 502"), want: "gh: HTTP 502"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repoDir, projDir, _ := newFeatureBackend(t)
			seedCards(t, repoDir, store.Feature{ID: "F-0001", State: domain.StateImplemented, DeliveryMRURL: tc.mr})
			useFakeForge(t, &fakeForge{pr: tc.pr, err: tc.err})
			if tc.origin != "" {
				useOrigin(t, tc.origin)
			}
			checks, err := AcceptMerged(repoDir, projDir, mergeCfg(), []string{"F-0001"})
			if err != nil {
				t.Fatalf("AcceptMerged: %v", err)
			}
			if len(checks) != 1 || checks[0].Accepted || !strings.Contains(checks[0].Line, tc.want) || !strings.HasPrefix(checks[0].Line, "F-0001: ") {
				t.Fatalf("check = %+v, want a refusal containing %q", checks, tc.want)
			}
			if got := reload(t, repoDir, "F-0001").State; got != domain.StateImplemented {
				t.Fatalf("state = %s, want implemented", got)
			}
		})
	}
}

// Outside the merge gate --merged is refused, naming the key.
// F-0488 crit. 5
func TestAcceptMergedRefusedOutsideGate(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	for _, gate := range []string{"", "human", "auto", "Merge"} {
		cfg := forgeCfg("github")
		cfg.Gates.Accept = gate
		_, err := AcceptMerged(repoDir, projDir, cfg, nil)
		if !errors.Is(err, ErrMergeGateOff) || !strings.Contains(err.Error(), "[gates] accept") {
			t.Errorf("gate %q: err = %v, want ErrMergeGateOff naming [gates] accept", gate, err)
		}
	}
}

// Under the merge gate with no [delivery] forge, cerne cannot ask anyone.
// F-0488 crit. 5
func TestAcceptMergedNoForge(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	cfg := mergeCfg()
	cfg.Delivery.Forge = ""
	_, err := AcceptMerged(repoDir, projDir, cfg, nil)
	if !errors.Is(err, ErrMergeGateNoForge) || !strings.Contains(err.Error(), "[delivery] forge") {
		t.Fatalf("err = %v, want ErrMergeGateNoForge naming [delivery] forge", err)
	}
}

// A parent accepted on merge takes its implemented tasks along, even in an
// agent's shell; an F- child keeps its own accept.
// F-0488 crit. 7
func TestAcceptMergedCascadesTasks(t *testing.T) {
	t.Setenv("CERNE_ACTOR", "agent")
	repoDir, projDir, _ := newFeatureBackend(t)
	seedCards(t, repoDir,
		store.Feature{ID: "E-0001", State: domain.StateImplemented, DeliveryMRURL: prURL("1")},
		store.Feature{ID: "T-0002", Parent: "E-0001", State: domain.StateImplemented},
		store.Feature{ID: "F-0003", Parent: "E-0001", State: domain.StateImplemented},
	)
	useFakeForge(t, &fakeForge{pr: mergedPR})

	checks, err := AcceptMerged(repoDir, projDir, mergeCfg(), []string{"E-0001"})
	if err != nil || !checks[0].Accepted {
		t.Fatalf("AcceptMerged = %+v, %v", checks, err)
	}
	if got := reload(t, repoDir, "T-0002"); got.State != domain.StateDone || got.AcceptedVia != "merge" {
		t.Fatalf("T-0002 = %+v, want done on merge", got)
	}
	ev := lastEventOf(t, repoDir, "T-0002")
	if ev.Actor != "human" || ev.Payload["cascade_from"] != "E-0001" || ev.Payload["via"] != "merge" {
		t.Fatalf("T-0002 event = %+v, want cascade_from E-0001, via merge, by human", ev)
	}
	if got := reload(t, repoDir, "F-0003").State; got != domain.StateImplemented {
		t.Fatalf("F-0003 state = %s, want implemented", got)
	}
	if !strings.Contains(strings.Join(checks[0].Outcome.Cascade, "\n"), "T-0002") {
		t.Fatalf("cascade lines = %q, want T-0002", checks[0].Outcome.Cascade)
	}
}

// Under the merge gate the next step of a delivered card is the agent's
// --merged, with the PR named; without a PR it is the link. No forge call.
// F-0488 crit. 8
func TestNextStepMergeGate(t *testing.T) {
	old := NewDeliveryForge
	t.Cleanup(func() { NewDeliveryForge = old })
	NewDeliveryForge = func(string, string) (forge.DeliveryForge, error) {
		t.Fatal("NextStep asked the forge")
		return nil, nil
	}
	proj, repo := nextStepRepo(t)
	if err := os.WriteFile(filepath.Join(proj, ".cernerc"), []byte("project_id = \"demo\"\nbackend = \"b\"\n\n[gates]\naccept = \"merge\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, f := range []store.Feature{
		{ID: "F-0001", Title: "t", State: domain.StateImplemented, DeliveryMRURL: prURL("12")},
		{ID: "F-0002", Title: "t", State: domain.StateImplemented, DeliveryCommitURL: "https://github.com/o/r/commit/abc1234"},
	} {
		if err := store.SaveFeature(repo, f); err != nil {
			t.Fatal(err)
		}
	}
	s, err := NextStep(repo, proj, "F-0001")
	if err != nil || s.Actor != ActorAgent || s.Command != "cerne feature accept F-0001 --merged" || !strings.Contains(s.Step, prURL("12")) {
		t.Fatalf("F-0001 step = %+v, %v", s, err)
	}
	s, err = NextStep(repo, proj, "F-0002")
	if err != nil || s.Actor != ActorAgent || s.Command != "cerne feature deliver F-0002 --mr <url>" || !strings.Contains(s.Step, "a human runs 'cerne feature accept F-0002'") {
		t.Fatalf("F-0002 step = %+v, %v", s, err)
	}
}

// A task the parent's acceptance carried along earlier in the same sweep is
// not examined again: no false "accept failed" line for a card already done.
// F-0488 crit. 3, 7
func TestAcceptMergedSweepSkipsCascaded(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	seedCards(t, repoDir,
		store.Feature{ID: "E-0001", State: domain.StateImplemented, DeliveryMRURL: prURL("1")},
		store.Feature{ID: "T-0002", Parent: "E-0001", State: domain.StateImplemented, DeliveryMRURL: prURL("2")},
	)
	useFakeForge(t, &fakeForge{pr: mergedPR})
	checks, err := AcceptMerged(repoDir, projDir, mergeCfg(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(checks) != 1 || checks[0].ID != "E-0001" || !checks[0].Accepted {
		t.Fatalf("checks = %+v, want only E-0001, accepted (T-0002 rides its cascade)", checks)
	}
	if got := reload(t, repoDir, "T-0002").State; got != domain.StateDone {
		t.Fatalf("T-0002 state = %s, want done", got)
	}
}

// An acceptance on merge the human reverted is not recorded again from the
// same merge, by id or by the sweep; a new delivery PR is accepted on its own
// merge.
// F-0499 crit. 5
func TestAcceptMergedSkipsRevertedMerge(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	seedCards(t, repoDir, store.Feature{ID: "F-0001", State: domain.StateImplemented, DeliveryMRURL: prURL("1")})
	useFakeForge(t, &fakeForge{pr: mergedPR})
	if _, err := AcceptMerged(repoDir, projDir, mergeCfg(), []string{"F-0001"}); err != nil {
		t.Fatal(err)
	}
	if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionRevert, "", "bug depois do merge", nil); err != nil {
		t.Fatalf("revert: %v", err)
	}

	for _, ids := range [][]string{{"F-0001"}, nil} {
		checks, err := AcceptMerged(repoDir, projDir, mergeCfg(), ids)
		if err != nil {
			t.Fatalf("AcceptMerged(%v): %v", ids, err)
		}
		c := checkFor(t, checks, "F-0001")
		if c.Accepted || !strings.Contains(c.Line, "already recorded") || !strings.Contains(c.Line, "cerne feature deliver F-0001 --mr <url>") {
			t.Fatalf("AcceptMerged(%v) check = %+v, want the reverted merge refused with the way out", ids, c)
		}
		if got := reload(t, repoDir, "F-0001").State; got != domain.StateImplemented {
			t.Fatalf("state = %s, want implemented", got)
		}
	}

	f := reload(t, repoDir, "F-0001")
	f.DeliveryMRURL = prURL("2")
	seedCards(t, repoDir, f)
	checks, err := AcceptMerged(repoDir, projDir, mergeCfg(), []string{"F-0001"})
	if err != nil {
		t.Fatal(err)
	}
	if c := checkFor(t, checks, "F-0001"); !c.Accepted {
		t.Fatalf("check = %+v, want the new PR's merge accepted", c)
	}
}
