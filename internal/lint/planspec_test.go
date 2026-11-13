package lint

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// planWrite is a plan's feature.doc_edited at minutes, against the spec whose
// text is spec ("" = an event from before F-0377, with no digest).
func planWrite(id string, minutes int, spec string) domain.Event {
	p := map[string]string{"doc": "plan", "state": "approved", "source": "human"}
	if spec != "" {
		sum := sha256.Sum256(SpecContent([]byte(spec)))
		p["spec_digest"] = hex.EncodeToString(sum[:])
	}
	return ev(id, "feature.doc_edited", minutes, p)
}

// TestPlanBehindSpec (F-0377 crit. 2, 4, 5): a plan is behind when its last
// write was against a spec other than the live one — by content, so a change
// of wrapping only is not one; pushed again after the spec, it is not; a last
// write with no digest (before F-0377) never is. planAt is the last plan
// write, specAt the last spec write (zero when none is recorded).
func TestPlanBehindSpec(t *testing.T) {
	const fm = "---\ncerne_id: F-0001\n---\n\n"
	at := func(minutes int) time.Time { return ev("", "", minutes, nil).At }
	cases := []struct {
		name           string
		live           string
		events         []domain.Event
		want           bool
		planAt, specAt time.Time
	}{
		{"same spec", fm + "A\n", []domain.Event{specEdit("F-0001", "approved", 5), planWrite("F-0001", 10, "A\n")}, false, at(10), at(5)},
		{"spec changed after the plan (crit. 2)", fm + "B\n", []domain.Event{planWrite("F-0001", 10, "A\n"), specEdit("F-0001", "approved", 20)}, true, at(10), at(20)},
		{"plan pushed again after the spec (crit. 4)", fm + "B\n", []domain.Event{planWrite("F-0001", 10, "A\n"), specEdit("F-0001", "approved", 20), planWrite("F-0001", 30, "B\n")}, false, at(30), at(20)},
		{"plan from before F-0377 (crit. 5)", fm + "B\n", []domain.Event{planWrite("F-0001", 10, ""), specEdit("F-0001", "approved", 20)}, false, at(10), at(20)},
		{"wrapping only", "\ufeff" + fm + "A  \r\n\r\n", []domain.Event{planWrite("F-0001", 10, "A\n"), specEdit("F-0001", "approved", 20)}, false, at(10), at(20)},
		{"no spec write recorded", fm + "B\n", []domain.Event{planWrite("F-0001", 10, "A\n")}, true, at(10), time.Time{}},
		{"no plan write", fm + "B\n", []domain.Event{specEdit("F-0001", "approved", 20)}, false, time.Time{}, at(20)},
		{"another feature's writes", fm + "B\n", []domain.Event{planWrite("F-0001", 10, "B\n"), planWrite("F-0002", 15, "A\n"), specEdit("F-0002", "approved", 20)}, false, at(10), time.Time{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repoDir := t.TempDir()
			writeSpecs(t, repoDir, "F-0001", "", tc.live)
			for _, e := range tc.events {
				if err := store.AppendEvent(repoDir, e); err != nil {
					t.Fatal(err)
				}
			}
			behind, planAt, specAt := PlanBehindSpec(repoDir, store.Feature{ID: "F-0001", State: domain.StateApproved})
			if behind != tc.want || !planAt.Equal(tc.planAt) || !specAt.Equal(tc.specAt) {
				t.Fatalf("PlanBehindSpec = %v, %v, %v; want %v, %v, %v", behind, planAt, specAt, tc.want, tc.planAt, tc.specAt)
			}
		})
	}

	// A feature with no spec has nothing to be behind.
	repoDir := t.TempDir()
	if err := store.AppendEvent(repoDir, planWrite("F-0001", 10, "A\n")); err != nil {
		t.Fatal(err)
	}
	if behind, _, _ := PlanBehindSpec(repoDir, store.Feature{ID: "F-0001", State: domain.StateApproved}); behind {
		t.Fatal("no spec read as a plan behind it")
	}
}
