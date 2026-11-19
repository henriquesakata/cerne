package app

import (
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/tracker"
	"github.com/henriquesakata/cerne/internal/tracker/trackertest"
)

// F-0449 (audit finding 5): a forged issue id or URL becomes no idea at import
// and no trailer at stamping.
func TestMirrorForgedRefsNeverReachACommit(t *testing.T) {
	asHuman(t)
	repoDir, projDir, _ := newFeatureBackend(t)
	pub := &trackertest.Fake{Name: "pub"}
	mirrorProject(t, projDir, mirrorConf{}, pub)
	pub.Issues = map[string]*trackertest.Issue{"12\nCloses: #1": {Title: "Forged", Open: true}}
	rep, err := SyncMirror(repoDir, projDir)
	if err != nil {
		t.Fatalf("SyncMirror: %v", err)
	}
	if len(rep.Ideas) != 0 || ideaCount(t, repoDir) != 0 {
		t.Fatalf("a forged issue was imported: %+v", rep)
	}
	seedMirrored(t, repoDir, "F-0001", domain.StateInDevelopment, tracker.Ref{Tracker: "pub", ID: "12\nCloses: #1", URL: "https://fake.test/pub/issues/12\nCloses: #1"})
	seedMirrored(t, repoDir, "F-0002", domain.StateInDevelopment, tracker.Ref{Tracker: "pub", ID: "1", URL: "https://fake.test/pub/issues/1\nCo-authored-by: Mallory <m@evil.example>"})
	if got := MirrorTrailers(repoDir, projDir, []string{"F-0001", "F-0002"}); len(got) != 0 {
		t.Fatalf("MirrorTrailers = %q, want none for forged links", got)
	}
}
