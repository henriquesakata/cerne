package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
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
