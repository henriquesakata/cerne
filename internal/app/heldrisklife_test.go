package app

import (
	"path/filepath"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
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

	// A task's field follows its latest reading: the next complete of the
	// parent rewrites it, empty when the task now rides. F-0521 crit. 3, 7
	reread := func(t *testing.T, round2 bool, staleTask store.Feature) (repoDir string, out TransitionOutcome) {
		t.Helper()
		asActor(t, "agent")
		repoDir, projDir, _ := newFeatureBackend(t)
		riskDelivery(t, repoDir, projDir, store.Feature{ID: "F-0001", Facets: []string{"code"}}, "vitória", cleanChange())
		seedRiskTasks(t, repoDir, projDir, staleTask)
		if staleTask.HeldRisk == nil {
			step(t, repoDir, projDir, domain.ActionComplete)
			if len(reload(t, repoDir, "T-0002").HeldRisk) == 0 {
				t.Fatal("the first complete left T-0002 no held_risk")
			}
			asActor(t, "")
			step(t, repoDir, projDir, domain.ActionRevert)
			asActor(t, "agent")
			task := reload(t, repoDir, "T-0002")
			task.Facets = []string{"code"} // the fix
			if err := store.SaveFeature(repoDir, task); err != nil {
				t.Fatal(err)
			}
		}
		if round2 {
			riskWrite(t, repoDir, filepath.Join("content", "features", "F-0001", "result.md"), riskResult("vitória")+"Rodada 2 — vitória: \"de novo\"\n")
		}
		if staleTask.HeldRisk == nil {
			if _, err := gitback.CommitAndPush(repoDir, "fix"); err != nil {
				t.Fatal(err)
			}
		}
		out, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionComplete, "", "", nil)
		if err != nil {
			t.Fatal(err)
		}
		return repoDir, out
	}
	t.Run("a task that now rides loses the old reasons", func(t *testing.T) {
		repoDir, _ := reread(t, true, store.Feature{ID: "T-0002", Facets: []string{"auth"}})
		if task := reload(t, repoDir, "T-0002"); task.State != domain.StateDone || task.AcceptedVia != "risk" || len(task.HeldRisk) != 0 {
			t.Errorf("T-0002 = %s via %q, held_risk %q; want done by risk, empty", task.State, task.AcceptedVia, task.HeldRisk)
		}
	})
	t.Run("a task kept implemented while the parent is held for another reason", func(t *testing.T) {
		repoDir, out := reread(t, false, store.Feature{ID: "T-0002", Facets: []string{"auth"}})
		if out.Risk == nil || len(out.Risk.HeldTasks) != 0 {
			t.Fatalf("outcome = %+v, want the parent held with no held task", out.Risk)
		}
		if task := reload(t, repoDir, "T-0002"); task.State != domain.StateImplemented || len(task.HeldRisk) != 0 {
			t.Errorf("T-0002 = %s, held_risk %q; want implemented, empty", task.State, task.HeldRisk)
		}
	})
	t.Run("a task held on its own, fixed later, rides the parent", func(t *testing.T) {
		repoDir, _ := reread(t, false, store.Feature{ID: "T-0002", Facets: []string{"code"}, HeldRisk: []string{"criterion 2 is verified by hand, not by a test"}})
		if task := reload(t, repoDir, "T-0002"); task.State != domain.StateDone || len(task.HeldRisk) != 0 {
			t.Errorf("T-0002 = %s, held_risk %q; want done, empty", task.State, task.HeldRisk)
		}
	})
}
