package app

import (
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
)

// TestHeldRiskLifecycle: the accept, by merge or by hand, keeps the reasons
// the gate held the card with; a revert from any state clears them; a card
// that never met the risk gate never gets the field.
// F-0521 crit. 3
func TestHeldRiskLifecycle(t *testing.T) {
	step := func(t *testing.T, repoDir, projDir string, action domain.Action) {
		t.Helper()
		reason := ""
		if action == domain.ActionRevert {
			reason = "because"
		}
		if _, err := TransitionFeature(repoDir, projDir, "F-0001", action, "", reason, nil); err != nil {
			t.Fatalf("%s: %v", action, err)
		}
	}
	held := func(t *testing.T, gate string) (repoDir, projDir string) {
		t.Helper()
		asActor(t, "agent")
		repoDir, projDir, _ = newFeatureBackend(t)
		g := project.GatesConfig{Accept: gate, RiskCalibrationN: new(int64)}
		riskDeliveryUnder(t, g, repoDir, projDir, store.Feature{ID: "F-0001", Facets: []string{"auth"}, DeliveryMRURL: prURL("7")}, "vitória", cleanChange())
		step(t, repoDir, projDir, domain.ActionComplete)
		return repoDir, projDir
	}
	cases := []struct {
		name string
		run  func(t *testing.T, repoDir, projDir string)
		want domain.FeatureState
		kept bool
	}{
		{"held, then accepted by the merge", func(t *testing.T, repoDir, projDir string) {
			useFakeForge(t, &fakeForge{pr: mergedPR})
			cfg := mergeCfg()
			cfg.Gates.Accept = project.GateRisk
			if _, err := AcceptMerged(repoDir, projDir, cfg, []string{"F-0001"}); err != nil {
				t.Fatal(err)
			}
		}, domain.StateDone, true},
		{"held, then accepted by hand", func(t *testing.T, repoDir, projDir string) {
			asActor(t, "")
			step(t, repoDir, projDir, domain.ActionAccept)
		}, domain.StateDone, true},
		{"held, then reverted", func(t *testing.T, repoDir, projDir string) {
			asActor(t, "")
			step(t, repoDir, projDir, domain.ActionRevert)
		}, domain.StateInDevelopment, false},
		{"held, accepted, then reverted from done", func(t *testing.T, repoDir, projDir string) {
			asActor(t, "")
			step(t, repoDir, projDir, domain.ActionAccept)
			step(t, repoDir, projDir, domain.ActionRevert)
		}, domain.StateImplemented, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			repoDir, projDir := held(t, project.GateRisk)
			if len(reload(t, repoDir, "F-0001").HeldRisk) == 0 {
				t.Fatal("the complete left no held_risk")
			}
			c.run(t, repoDir, projDir)
			f := reload(t, repoDir, "F-0001")
			if f.State != c.want || (len(f.HeldRisk) > 0) != c.kept {
				t.Errorf("card = %s, held_risk %q; want %s, kept %v", f.State, f.HeldRisk, c.want, c.kept)
			}
		})
	}
	t.Run("under the merge gate", func(t *testing.T) {
		repoDir, _ := held(t, project.GateMerge)
		if f := reload(t, repoDir, "F-0001"); f.State != domain.StateImplemented || len(f.HeldRisk) != 0 {
			t.Errorf("card = %s, held_risk %q; want implemented, no field", f.State, f.HeldRisk)
		}
	})
}
