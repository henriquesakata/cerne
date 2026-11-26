package app

import (
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// TestRiskHeldTaskStoresReasons: a task the risk gate holds keeps its own
// Held() lines, both when its parent's complete holds it and when the cascade
// of an accept finds it held.
// F-0521 crit. 2
func TestRiskHeldTaskStoresReasons(t *testing.T) {
	t.Run("the parent's complete", func(t *testing.T) {
		asActor(t, "agent")
		repoDir, projDir, _ := newFeatureBackend(t)
		riskDelivery(t, repoDir, projDir, store.Feature{ID: "F-0001", Facets: []string{"code"}}, "vitória", cleanChange())
		seedRiskTasks(t, repoDir, projDir, store.Feature{ID: "T-0002", Facets: []string{"code"}}, store.Feature{ID: "T-0003", Facets: []string{"auth"}})
		out, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionComplete, "", "", nil)
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Join(reload(t, repoDir, "T-0003").HeldRisk, "\n"); !strings.Contains(got, "auth") {
			t.Errorf("T-0003 held_risk = %q, want its own sensitive facet", got)
		}
		if got := reload(t, repoDir, "T-0002").HeldRisk; len(got) != 0 {
			t.Errorf("T-0002 held_risk = %q, want empty", got)
		}
		if got := reload(t, repoDir, "F-0001").HeldRisk; strings.Join(got, "\n") != strings.Join(out.Risk.Held(), "\n") || len(got) == 0 {
			t.Errorf("F-0001 held_risk = %q, want %q", got, out.Risk.Held())
		}
	})
	t.Run("the cascade", func(t *testing.T) {
		asActor(t, "agent")
		repoDir, projDir, _ := newFeatureBackend(t)
		riskDelivery(t, repoDir, projDir, store.Feature{ID: "F-0001", Facets: []string{"code"}}, "vitória", cleanChange())
		seedRiskTasks(t, repoDir, projDir, store.Feature{ID: "T-0002", Facets: []string{"auth"}})
		parent := reload(t, repoDir, "F-0001")
		parent.AcceptedVia = "risk"
		if _, err := cascadeAccept(repoDir, projDir, parent, nil, nil); err != nil {
			t.Fatal(err)
		}
		task := reload(t, repoDir, "T-0002")
		if task.State != domain.StateImplemented || !strings.Contains(strings.Join(task.HeldRisk, "\n"), "auth") {
			t.Errorf("T-0002 = %s, held_risk %q; want implemented with its own sensitive facet", task.State, task.HeldRisk)
		}
	})
}
