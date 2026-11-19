package app

import (
	"slices"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/tracker"
	"github.com/henriquesakata/cerne/internal/tracker/trackertest"
)

// F-0449 (audit finding 1): a link is resolved by tracker name, so a tracker
// re-pointed to another repository would adopt issue #N there. The core asks
// the adapter whether it owns the link and skips the card when not, on the
// transition, the sync and the trailer.
func TestMirrorLinkOnAnotherRepoIsSkipped(t *testing.T) {
	asHuman(t)
	repoDir, projDir, _ := newFeatureBackend(t)
	pub := &trackertest.Fake{Name: "pub"}
	mirrorProject(t, projDir, mirrorConf{}, pub)
	seedMirrored(t, repoDir, "F-0001", domain.StateCandidate, tracker.Ref{Tracker: "pub", ID: "1", URL: "https://fake.test/elsewhere/issues/1"})
	want := "cerne: mirror F-0001 -> pub: link https://fake.test/elsewhere/issues/1 is not on https://fake.test/o/pub; re-link or restore the tracker"

	out, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionReject, "", "not now", nil)
	if err != nil {
		t.Fatalf("reject: %v", err)
	}
	if !slices.Contains(out.Warnings, want) {
		t.Errorf("transition warnings = %q, want %q", out.Warnings, want)
	}
	rep, err := SyncMirror(repoDir, projDir)
	if err != nil {
		t.Fatalf("SyncMirror: %v", err)
	}
	if !slices.Contains(rep.Warnings, want) || slices.Contains(rep.Applied, "F-0001") {
		t.Errorf("sync warnings = %q, applied = %q; want the warning and F-0001 not applied", rep.Warnings, rep.Applied)
	}
	if got := MirrorTrailers(repoDir, projDir, []string{"F-0001"}); len(got) != 0 {
		t.Errorf("MirrorTrailers = %q, want none for a link the tracker does not own", got)
	}
	for _, c := range pub.Calls {
		if c.Op == "Update" {
			t.Fatalf("the re-pointed tracker got an Update: %+v", c)
		}
	}
}
