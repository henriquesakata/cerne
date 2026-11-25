package app

import (
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// F-0506 crit. 4: --merged with the id of a done card says it was already
// done, as a success, and never asks the forge.
func TestAcceptMergedByIDAlreadyDone(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	seedCards(t, repoDir, store.Feature{ID: "F-0001", State: domain.StateDone, DeliveryMRURL: prURL("1")})
	f := &fakeForge{pr: mergedPR}
	useFakeForge(t, f)

	checks, err := AcceptMerged(repoDir, projDir, mergeCfg(), []string{"F-0001"})
	if err != nil {
		t.Fatalf("AcceptMerged: %v", err)
	}
	if len(checks) != 1 || !checks[0].Accepted || checks[0].Line != "F-0001 was already done; nothing changed" {
		t.Fatalf("checks = %+v, want one success with the already-done line", checks)
	}
	if f.prN != 0 {
		t.Fatalf("the forge was asked about PR %d", f.prN)
	}
}
