package app

import (
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/tracker/trackertest"
)

// F-0449 (audit finding 2): the superseded comment cites the successor's issue
// only when it lives on the same tracker; one on another tracker never leaks
// into this tracker's calls.
func TestMirrorSuccessorOnAnotherTrackerIsNotCited(t *testing.T) {
	asHuman(t)
	repoDir, projDir, _ := newFeatureBackend(t)
	pub, internal := &trackertest.Fake{Name: "pub"}, &trackertest.Fake{Name: "internal"}
	mirrorProject(t, projDir, mirrorConf{}, pub, internal)
	seedMirrored(t, repoDir, "F-0001", domain.StateCandidate, issueOn(t, pub, "F-0001", domain.StateCandidate))
	succ := seedMirrored(t, repoDir, "F-0002", domain.StateCandidate, issueOn(t, internal, "F-0002", domain.StateCandidate))

	if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionSupersede, "F-0002", "replaced", nil); err != nil {
		t.Fatalf("supersede: %v", err)
	}
	if got := lastUpdate(t, pub, "F-0001").Item.Successor; got != nil {
		t.Errorf("Successor = %+v, want nil for a successor on another tracker", got)
	}
	for _, iss := range pub.Issues {
		if len(iss.Comments) > 0 {
			t.Errorf("pub issue got comments %q, want none", iss.Comments)
		}
	}
	for _, c := range pub.Calls {
		if c.Item.Successor != nil || strings.Contains(c.Item.Body(), succ.URL) {
			t.Fatalf("a pub call carries the internal successor: %+v", c)
		}
	}
}
