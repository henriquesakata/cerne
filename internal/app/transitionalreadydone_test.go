package app

import (
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
)

// F-0506 crit. 1, 2: a repeated accept on a done card is a no-op: AlreadyDone,
// no error, no commit, no event, same entity.
func TestAcceptDoneIsNoOp(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "F-0001", domain.StateImplemented)
	if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionAccept, "", "", nil); err != nil {
		t.Fatalf("first accept: %v", err)
	}
	head := func() string {
		out, err := exec.Command("git", "-C", repoDir, "rev-parse", "HEAD").Output()
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(string(out))
	}
	headBefore, entBefore := head(), reload(t, repoDir, "F-0001")

	out, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionAccept, "", "again", nil)
	if err != nil {
		t.Fatalf("repeated accept: %v", err)
	}
	if !out.AlreadyDone || out.From != domain.StateDone || out.To != domain.StateDone {
		t.Fatalf("outcome = %+v, want AlreadyDone done → done", out)
	}
	if head() != headBefore {
		t.Fatal("backend HEAD moved: the repeated accept wrote a commit")
	}
	if got := reload(t, repoDir, "F-0001"); !reflect.DeepEqual(got, entBefore) {
		t.Fatalf("entity changed: %+v → %+v", entBefore, got)
	}
}
