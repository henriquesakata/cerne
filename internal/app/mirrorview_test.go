package app

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
	"github.com/henriquesakata/cerne/internal/tracker/trackertest"
)

// F-0465 crit. 4: a sync leaves the time and, per tracker, what it did in the
// local cache; the backend gains nothing from the record.
func TestMirrorSyncRecordsLastRun(t *testing.T) {
	asHuman(t)
	repoDir, projDir, _ := newFeatureBackend(t)
	good, bad := &trackertest.Fake{Name: "good"}, &trackertest.Fake{Name: "bad"}
	mirrorProject(t, projDir, mirrorConf{}, good, bad)
	r := issueOn(t, good, "F-0001", domain.StateCandidate)
	seedMirrored(t, repoDir, "F-0001", domain.StateCandidate, r)
	good.Open("Crash on start", "Steps", "")
	bad.Err = errors.New("boom")

	if _, err := SyncMirror(repoDir, projDir); err != nil {
		t.Fatalf("SyncMirror: %v", err)
	}
	got := LastMirrorSync(repoDir)
	if got == nil || got.At.IsZero() || len(got.Trackers) != 2 {
		t.Fatalf("last sync = %+v, want a time and two trackers", got)
	}
	g, b := got.Trackers[0], got.Trackers[1]
	if g.Name != "good" || g.Synced != 1 || g.Imported != 1 || len(g.Errors) != 0 {
		t.Errorf("good run = %+v, want 1 synced, 1 imported, no error", g)
	}
	if b.Name != "bad" || b.Synced != 0 || b.Imported != 0 || len(b.Errors) != 1 || !strings.Contains(b.Errors[0], "boom") {
		t.Errorf("bad run = %+v, want an error mentioning boom", b)
	}
	head := backendHead(t, repoDir)
	if _, err := SyncMirror(repoDir, projDir); err != nil {
		t.Fatalf("second SyncMirror: %v", err)
	}
	if backendHead(t, repoDir) != head {
		t.Fatal("the second sync, which imported nothing, moved the backend head")
	}
}

// F-0465 crit. 4: a record that cannot be written is a warning, never a failure.
func TestMirrorSyncRecordWriteFailureWarns(t *testing.T) {
	asHuman(t)
	repoDir, projDir, _ := newFeatureBackend(t)
	mirrorProject(t, projDir, mirrorConf{}, &trackertest.Fake{Name: "pub"})
	if err := os.Mkdir(lastSyncPath(repoDir), 0o755); err != nil {
		t.Fatal(err)
	}
	rep, err := SyncMirror(repoDir, projDir)
	if err != nil {
		t.Fatalf("SyncMirror: %v", err)
	}
	if len(rep.Warnings) != 0 || len(rep.Notes) != 1 || !strings.Contains(rep.Notes[0], "last sync") {
		t.Fatalf("warnings = %q, notes = %q; want no warning and one note about the last-sync record", rep.Warnings, rep.Notes)
	}
}

// F-0465 crit. 4: with the mirror off nothing is recorded.
func TestMirrorSyncOffRecordsNothing(t *testing.T) {
	asHuman(t)
	repoDir, projDir, _ := newFeatureBackend(t)
	mirrorProject(t, projDir, mirrorConf{off: true}, &trackertest.Fake{Name: "pub"})
	if _, err := SyncMirror(repoDir, projDir); !errors.Is(err, ErrMirrorOff) {
		t.Fatalf("err = %v, want ErrMirrorOff", err)
	}
	if _, err := os.Stat(lastSyncPath(repoDir)); !os.IsNotExist(err) {
		t.Fatalf("stat = %v, want no record", err)
	}
}

func TestLastMirrorSyncMissing(t *testing.T) {
	repoDir := filepath.Join(t.TempDir(), "repo")
	if got := LastMirrorSync(repoDir); got != nil {
		t.Fatalf("missing file: %+v, want nil", got)
	}
	if err := os.WriteFile(lastSyncPath(repoDir), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := LastMirrorSync(repoDir); got != nil {
		t.Fatalf("corrupt file: %+v, want nil", got)
	}
}

// ---- F-0465 T2: overview and check ----

// checkCase is one row of TestCheckMirrorTable: a card in state, its issue
// open or not on the tracker, and what the check must say.
type checkCase struct {
	name       string
	state      domain.FeatureState
	issueOpen  bool
	closeOn    string
	label      string // the tracker's sync label; cerne's own issues do not carry it
	failTrk    bool
	delay      bool
	unknownTrk bool
	capped     bool
	delivery   bool // the card carries a delivery PR (B-0079)
	refsOnly   bool // the tracker is a kind no merge closes (B-0079)
	status     string
	reason     string
}

func TestCheckMirrorTable(t *testing.T) {
	cases := []checkCase{
		{name: "open card, open issue", state: domain.StateInDevelopment, issueOpen: true, status: MirrorOK},
		{name: "terminal card, closed issue", state: domain.StateDone, status: MirrorOK},
		{name: "terminal card, issue still open", state: domain.StateRejected, issueOpen: true, status: MirrorDrift, reason: ReasonOpenCardClosed},
		{name: "superseded card, issue still open", state: domain.StateSuperseded, issueOpen: true, status: MirrorDrift, reason: ReasonOpenCardClosed},
		{name: "open card, issue gone from the list", state: domain.StateCandidate, status: MirrorDrift, reason: ReasonClosedCardOpen},
		{name: "labeled tracker: own open issue is up to date", state: domain.StateInDevelopment, issueOpen: true, label: "cerne", status: MirrorOK},
		{name: "labeled tracker: own closed issue of an open card is drift", state: domain.StateInDevelopment, label: "cerne", status: MirrorDrift, reason: ReasonClosedCardOpen},
		{name: "close_on merge: closed issue of an in_development card is the merge", state: domain.StateInDevelopment, closeOn: "merge", status: MirrorOK},
		{name: "close_on merge: closed issue of an implemented card is the merge", state: domain.StateImplemented, closeOn: "merge", status: MirrorOK},
		{name: "close_on merge: closed issue of an approved card is drift", state: domain.StateApproved, closeOn: "merge", status: MirrorDrift, reason: ReasonClosedCardOpen},
		{name: "close_on merge: closed issue of a candidate card is drift", state: domain.StateCandidate, closeOn: "merge", status: MirrorDrift, reason: ReasonClosedCardOpen},
		// B-0079 crit. 8: a done card with a delivery waits for the merge to close its issue.
		{name: "close_on merge: open issue of a delivered done card waits for the merge", state: domain.StateDone, issueOpen: true, closeOn: "merge", delivery: true, status: MirrorOK},
		{name: "close_on merge: open issue of a done card with no delivery is drift", state: domain.StateDone, issueOpen: true, closeOn: "merge", status: MirrorDrift, reason: ReasonOpenCardClosed},
		{name: "close_on accept: open issue of a delivered done card is drift", state: domain.StateDone, issueOpen: true, delivery: true, status: MirrorDrift, reason: ReasonOpenCardClosed},
		{name: "close_on merge: open issue of a delivered done card on a tracker no merge closes is drift", state: domain.StateDone, issueOpen: true, closeOn: "merge", delivery: true, refsOnly: true, status: MirrorDrift, reason: ReasonOpenCardClosed},
		{name: "tracker fails", state: domain.StateCandidate, issueOpen: true, failTrk: true, status: MirrorUnchecked, reason: ReasonTrackerFailed},
		{name: "tracker times out", state: domain.StateCandidate, issueOpen: true, delay: true, status: MirrorUnchecked, reason: ReasonTrackerFailed},
		{name: "tracker left the config", state: domain.StateCandidate, issueOpen: true, unknownTrk: true, status: MirrorUnchecked, reason: ReasonTrackerUnknown},
		{name: "list at the cap, issue not in it", state: domain.StateCandidate, capped: true, status: MirrorUnchecked, reason: ReasonListCapped},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			asHuman(t)
			if c.delay {
				prev := mirrorTimeout
				mirrorTimeout = 30 * time.Millisecond
				t.Cleanup(func() { mirrorTimeout = prev })
			}
			repoDir, projDir, _ := newFeatureBackend(t)
			pub := &trackertest.Fake{Name: "pub", RefsOnly: c.refsOnly}
			mirrorProject(t, projDir, mirrorConf{closeOn: c.closeOn, labels: map[string]string{"pub": c.label}}, pub)
			r := issueOn(t, pub, "F-0001", c.state)
			pub.Issues[r.ID].Open = c.issueOpen
			seedMirrored(t, repoDir, "F-0001", c.state, r)
			if c.delivery {
				f := reload(t, repoDir, "F-0001")
				f.DeliveryMRURL = "https://github.com/o/r/pull/7"
				if err := store.SaveFeature(repoDir, f); err != nil {
					t.Fatal(err)
				}
			}
			if c.unknownTrk {
				f := reload(t, repoDir, "F-0001")
				f.Mirror.Tracker = "gone"
				if err := store.SaveFeature(repoDir, f); err != nil {
					t.Fatal(err)
				}
			}
			if c.capped {
				for i := 0; i < mirrorListCap; i++ {
					pub.Open("other", "", "")
				}
			}
			if c.failTrk {
				pub.Err = errors.New("boom")
			}
			if c.delay {
				pub.Delay = time.Second
			}
			got, err := CheckMirror(repoDir, projDir)
			if err != nil {
				t.Fatalf("CheckMirror: %v", err)
			}
			if len(got) != 1 || got[0].ID != "F-0001" || got[0].Status != c.status || got[0].Reason != c.reason {
				t.Fatalf("check = %+v, want F-0001 %s/%s", got, c.status, c.reason)
			}
			if c.failTrk && !strings.Contains(got[0].Detail, "boom") {
				t.Errorf("detail = %q, want the tracker's error", got[0].Detail)
			}
		})
	}
}

// One tracker failing leaves the others checked; ErrMirrorOff when off.
func TestCheckMirrorTrackerIsolationAndOff(t *testing.T) {
	asHuman(t)
	repoDir, projDir, _ := newFeatureBackend(t)
	good, bad := &trackertest.Fake{Name: "good"}, &trackertest.Fake{Name: "bad"}
	mirrorProject(t, projDir, mirrorConf{}, good, bad)
	seedMirrored(t, repoDir, "F-0001", domain.StateCandidate, issueOn(t, bad, "F-0001", domain.StateCandidate))
	seedMirrored(t, repoDir, "F-0002", domain.StateCandidate, issueOn(t, good, "F-0002", domain.StateCandidate))
	bad.Err = errors.New("boom")
	got, err := CheckMirror(repoDir, projDir)
	if err != nil || len(got) != 2 {
		t.Fatalf("CheckMirror = %+v, %v", got, err)
	}
	if got[0].Status != MirrorUnchecked || got[1].Status != MirrorOK {
		t.Fatalf("checks = %+v, want F-0001 unchecked and F-0002 ok", got)
	}

	repoDir, projDir, _ = newFeatureBackend(t)
	mirrorProject(t, projDir, mirrorConf{off: true}, good)
	if _, err := CheckMirror(repoDir, projDir); !errors.Is(err, ErrMirrorOff) {
		t.Fatalf("off: err = %v, want ErrMirrorOff", err)
	}
}

// Opening the tab calls no tracker.
func TestMirrorOverviewNoNetwork(t *testing.T) {
	asHuman(t)
	repoDir, projDir, _ := newFeatureBackend(t)
	a, b := &trackertest.Fake{Name: "a"}, &trackertest.Fake{Name: "b"}
	mirrorProject(t, projDir, mirrorConf{}, a, b)
	seedMirrored(t, repoDir, "F-0001", domain.StateCandidate, issueOn(t, a, "F-0001", domain.StateCandidate))
	seedMirrored(t, repoDir, "F-0002", domain.StateDone, issueOn(t, b, "F-0002", domain.StateDone))
	outside := b.Open("Crash on start", "Steps", "")
	if _, err := SyncMirror(repoDir, projDir); err != nil {
		t.Fatal(err)
	}
	a.Calls, b.Calls = nil, nil

	v, err := MirrorOverview(repoDir, projDir)
	if err != nil {
		t.Fatalf("MirrorOverview: %v", err)
	}
	if len(a.Calls)+len(b.Calls) != 0 {
		t.Fatalf("tracker calls = %v %v, want none", a.Calls, b.Calls)
	}
	if !v.Enabled || len(v.Trackers) != 2 || len(v.Cards) != 2 || len(v.Ideas) != 1 || v.LastSync == nil {
		t.Fatalf("view = %+v", v)
	}
	if c := v.Cards[1]; c.ID != "F-0002" || c.Tracker != "b" || c.State != domain.StateDone || c.Title == "" || c.Ref.URL == "" {
		t.Errorf("card = %+v", c)
	}
	if i := v.Ideas[0]; i.Tracker != "b" || i.URL != outside.URL || i.Title != "Crash on start" || i.IssueID != outside.ID {
		t.Errorf("idea = %+v", i)
	}
}

func TestMirrorOverviewOff(t *testing.T) {
	asHuman(t)
	repoDir, projDir, _ := newFeatureBackend(t)
	mirrorProject(t, projDir, mirrorConf{off: true})
	v, err := MirrorOverview(repoDir, projDir)
	if err != nil || v.Enabled || len(v.Cards) != 0 || len(v.Ideas) != 0 || len(v.Trackers) != 0 {
		t.Fatalf("view = %+v, %v; want disabled and empty", v, err)
	}
}

// The settings registry already drops a tracker whose URL carries a credential
// (the .cernerc is committed); the overview still strips userinfo, so a hand
// edit the registry does not catch never reaches the screen.
func TestMirrorOverviewRedactsTrackerURL(t *testing.T) {
	for in, want := range map[string]string{
		"https://user:s3cret@github.com/o/r": "https://github.com/o/r",
		"https://tok@github.com/o/r":         "https://github.com/o/r",
		"https://github.com/o/r":             "https://github.com/o/r",
		"https://a b@github.com/o/r\x7f":     "",
	} {
		if got := stripUserinfo(in); got != want {
			t.Errorf("stripUserinfo(%q) = %q, want %q", in, got, want)
		}
	}
}

// The check writes neither to the backend nor to a tracker.
func TestCheckMirrorWritesNothing(t *testing.T) {
	asHuman(t)
	repoDir, projDir, _ := newFeatureBackend(t)
	pub := &trackertest.Fake{Name: "pub"}
	mirrorProject(t, projDir, mirrorConf{}, pub)
	seedMirrored(t, repoDir, "F-0001", domain.StateDone, issueOn(t, pub, "F-0001", domain.StateCandidate))
	head := backendHead(t, repoDir)
	if _, err := CheckMirror(repoDir, projDir); err != nil {
		t.Fatal(err)
	}
	if backendHead(t, repoDir) != head {
		t.Fatal("the backend head moved")
	}
	for _, c := range pub.Calls {
		if c.Op != "ListOpen" {
			t.Fatalf("tracker got %s, want only ListOpen", c.Op)
		}
	}
}

// ---- F-0465 T3: commits that cite a card ----

// gitProject is a git repository in a fresh dir with an identity set.
func gitProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, a := range [][]string{{"init", "-q"}, {"config", "user.email", "t@t"}, {"config", "user.name", "t"}, {"config", "commit.gpgsign", "false"}} {
		gitOut(t, dir, a...)
	}
	return dir
}

func commitMsg(t *testing.T, dir, msg string) {
	t.Helper()
	gitOut(t, dir, "commit", "-q", "--allow-empty", "-m", msg)
}

func TestMirrorCommits(t *testing.T) {
	dir := gitProject(t)
	commitMsg(t, dir, "feat: other card\n\nCerne: F-0001")
	commitMsg(t, dir, "feat: longer id\n\nCerne: F-04650")
	commitMsg(t, dir, "docs: mentions F-0465 only in the subject")
	commitMsg(t, dir, "feat: prose\n\nSee Cerne: F-0465 is nice")
	for i := 0; i < 25; i++ {
		commitMsg(t, dir, "feat: step "+strconv.Itoa(i)+"\n\nCerne: F-0465\nRefs: #12")
	}
	got, err := MirrorCommits(dir, "F-0465")
	if err != nil {
		t.Fatalf("MirrorCommits: %v", err)
	}
	if len(got) != 20 {
		t.Fatalf("got %d commits, want 20", len(got))
	}
	if got[0].Subject != "feat: step 24" || got[19].Subject != "feat: step 5" {
		t.Errorf("order: first %q, last %q; want newest first", got[0].Subject, got[19].Subject)
	}
	if want := gitOut(t, dir, "rev-parse", "--short", "HEAD"); !strings.HasPrefix(want, got[0].SHA) && !strings.HasPrefix(got[0].SHA, want) {
		t.Errorf("sha = %q, want the short sha %q", got[0].SHA, want)
	}
	// Two commits cite two cards: each is found by its own id only.
	if o, err := MirrorCommits(dir, "F-0001"); err != nil || len(o) != 1 {
		t.Errorf("F-0001: %d commits, %v; want 1", len(o), err)
	}
}

func TestMirrorCommitsNoRepo(t *testing.T) {
	if _, err := MirrorCommits(t.TempDir(), "F-0465"); !errors.Is(err, ErrNoGitRepo) {
		t.Fatalf("err = %v, want ErrNoGitRepo", err)
	}
}

func TestMirrorCommitsBadID(t *testing.T) {
	dir := gitProject(t)
	for _, id := range []string{"", "f-0465", "F-1", "F-0465$", "F-0465\nX", "--all", ".*", "F-0465|F-0001"} {
		if _, err := MirrorCommits(dir, id); !errors.Is(err, ErrBadCardID) {
			t.Errorf("id %q: err = %v, want ErrBadCardID", id, err)
		}
	}
}

func TestMirrorCommitsURL(t *testing.T) {
	asHuman(t)
	_, projDir, _ := newFeatureBackend(t)
	mirrorProject(t, projDir, mirrorConf{off: true}) // writes the .cernerc the setting needs
	gitOut(t, projDir, "init", "-q")
	gitOut(t, projDir, "config", "user.email", "t@t")
	gitOut(t, projDir, "config", "user.name", "t")
	gitOut(t, projDir, "config", "commit.gpgsign", "false")
	commitMsg(t, projDir, "feat: x\n\nCerne: F-0465")
	sha := gitOut(t, projDir, "rev-parse", "HEAD")

	got, err := MirrorCommits(projDir, "F-0465")
	if err != nil || len(got) != 1 || got[0].URL != "" {
		t.Fatalf("without commit_url: %+v, %v; want one commit with no link", got, err)
	}
	for tmpl, prefix := range map[string]string{
		"https://github.com/o/r/commit/{sha}":      "https://github.com/o/r/commit/",
		"https://gitlab.com/g/s/r/-/commit/{sha}":  "https://gitlab.com/g/s/r/-/commit/",
		"https://gitea.example/o/r/commit/abc1234": "https://gitea.example/o/r/commit/",
	} {
		if err := project.SetSettingValue(projDir, "delivery.commit_url", project.ScopeProject, tmpl); err != nil {
			t.Fatalf("set commit_url: %v", err)
		}
		got, err := MirrorCommits(projDir, "F-0465")
		if err != nil || len(got) != 1 || !strings.HasPrefix(got[0].URL, prefix) || !strings.HasPrefix(sha, strings.TrimPrefix(got[0].URL, prefix)) || strings.HasSuffix(got[0].URL, "}") {
			t.Fatalf("template %s: %+v, %v; want %s<sha>", tmpl, got, err, prefix)
		}
	}
	if err := project.SetSettingValue(projDir, "delivery.commit_url", project.ScopeProject, "not a url"); err == nil {
		if got, _ := MirrorCommits(projDir, "F-0465"); len(got) != 1 || got[0].URL != "" {
			t.Fatalf("unparseable template: %+v, want a commit with no link", got)
		}
	}
}

// F-0465 critic: the list follows the grammar the commit hook writes and reads
// (internal/app CitedCardIDs): any case, several ids on a line, CRLF.
func TestMirrorCommitsTrailerGrammar(t *testing.T) {
	dir := gitProject(t)
	commitMsg(t, dir, "feat: lower\n\ncerne: F-0001")
	commitMsg(t, dir, "feat: several\n\nCerne: F-0001, F-0002")
	commitMsg(t, dir, "feat: spaces\n\nCERNE: F-0002 F-0001 B-0003")
	gitOut(t, dir, "commit", "-q", "--allow-empty", "--cleanup=verbatim", "-m", "feat: crlf\r\n\r\nCerne: F-0001\r\nRefs: #1\r\n")
	commitMsg(t, dir, "feat: longer\n\nCerne: F-00010")
	commitMsg(t, dir, "docs: only the subject, F-0001\n\nCerne: F-0009")
	commitMsg(t, dir, "docs: prose\n\nThis Cerne: F-0001 is prose")

	subjects := func(id string) []string {
		t.Helper()
		got, err := MirrorCommits(dir, id)
		if err != nil {
			t.Fatalf("MirrorCommits(%s): %v", id, err)
		}
		var out []string
		for _, c := range got {
			out = append(out, c.Subject)
		}
		return out
	}
	if got, want := subjects("F-0001"), []string{"feat: crlf", "feat: spaces", "feat: several", "feat: lower"}; !slices.Equal(got, want) {
		t.Errorf("F-0001 = %q, want %q", got, want)
	}
	if got, want := subjects("F-0002"), []string{"feat: spaces", "feat: several"}; !slices.Equal(got, want) {
		t.Errorf("F-0002 = %q, want %q", got, want)
	}
	if got := subjects("F-0010"); len(got) != 0 {
		t.Errorf("F-0010 = %q, want none (F-00010 is another card)", got)
	}
}

func TestCitedCardIDs(t *testing.T) {
	for _, tc := range []struct {
		msg  string
		want []string
	}{
		{"feat: x\n\nCerne: F-0001", []string{"F-0001"}},
		{"fix: y\n\nCerne:\tF-0001\n", []string{"F-0001"}},
		{"x\n\ncerne: F-0001, F-0002", []string{"F-0001", "F-0002"}},
		{"x\n\nCERNE: F-0002 F-0001\nCerne: B-0003\r\n", []string{"F-0002", "F-0001", "B-0003"}},
		{"x\n\nCerne:\nF-0001", nil},
		{"x F-0001\n\nno trailer", nil},
	} {
		if got := CitedCardIDs(tc.msg); !slices.Equal(got, tc.want) {
			t.Errorf("CitedCardIDs(%q) = %q, want %q", tc.msg, got, tc.want)
		}
	}
}

// F-0465 critic: only git's own "not a git repository" reads as no repository;
// a git that fails otherwise (dubious ownership, missing from PATH) is an error
// that carries git's message.
func TestMirrorCommitsGitFailures(t *testing.T) {
	t.Run("dubious ownership", func(t *testing.T) {
		bin := t.TempDir()
		script := "#!/bin/sh\necho \"fatal: detected dubious ownership in repository at '/x'\" >&2\nexit 128\n"
		if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PATH", bin)
		_, err := MirrorCommits(t.TempDir(), "F-0465")
		if err == nil || errors.Is(err, ErrNoGitRepo) || !strings.Contains(err.Error(), "dubious ownership") {
			t.Fatalf("err = %v, want an error carrying git's dubious-ownership text, not ErrNoGitRepo", err)
		}
	})
	t.Run("git missing", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		if _, err := MirrorCommits(t.TempDir(), "F-0465"); err == nil || errors.Is(err, ErrNoGitRepo) {
			t.Fatalf("err = %v, want a plain error, not ErrNoGitRepo", err)
		}
	})
}
