package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
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
	if len(rep.Warnings) != 1 || !strings.Contains(rep.Warnings[0], "last sync") {
		t.Fatalf("warnings = %q, want one about the last-sync record", rep.Warnings)
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
	failTrk    bool
	delay      bool
	unknownTrk bool
	capped     bool
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
		{name: "close_on merge: closed issue of an open card is the merge", state: domain.StateInDevelopment, closeOn: "merge", status: MirrorOK},
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
			pub := &trackertest.Fake{Name: "pub"}
			mirrorProject(t, projDir, mirrorConf{closeOn: c.closeOn}, pub)
			r := issueOn(t, pub, "F-0001", c.state)
			pub.Issues[r.ID].Open = c.issueOpen
			seedMirrored(t, repoDir, "F-0001", c.state, r)
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
	if i := v.Ideas[0]; i.Tracker != "b" || i.URL != outside.URL || i.Title != "Crash on start" {
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
