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

// TestPlanOlderThanSpecFindings (F-0377 crit. 2): the lint flags a plan
// behind its spec only while the plan is the live contract (approved, in
// development), names the feature and the two writes in English — "unknown"
// when no spec write is recorded — and only warns: advisory, like the start
// (critic round 1, item 1).
func TestPlanOlderThanSpecFindings(t *testing.T) {
	repoDir := t.TempDir()
	states := map[string]domain.FeatureState{
		"F-0001": domain.StateApproved, "F-0002": domain.StateInDevelopment, "F-0003": domain.StateCandidate,
		"F-0004": domain.StateImplemented, "F-0005": domain.StateDone, "F-0006": domain.StateApproved,
	}
	for id, s := range states {
		writeSpecs(t, repoDir, id, "", "B\n")
		if err := store.SaveFeature(repoDir, store.Feature{ID: id, Title: "t " + id, State: s}); err != nil {
			t.Fatal(err)
		}
		events := []domain.Event{planWrite(id, 10, "A\n")}
		if id != "F-0006" {
			events = append(events, specEdit(id, string(s), 20))
		}
		for _, e := range events {
			if err := store.AppendEvent(repoDir, e); err != nil {
				t.Fatal(err)
			}
		}
	}
	findings, err := Run(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Finding{}
	for _, f := range findings {
		if f.Rule == RulePlanOlderThanSpec {
			got[f.Entity] = f
		}
	}
	want := map[string]string{
		"F-0001": "F-0001 plan was written on 2026-08-11 12:10:00 UTC against an older spec (spec changed 2026-08-11 12:20:00 UTC) — re-read the spec, update the plan (a line recording the re-check is enough) and push it",
		"F-0002": "F-0002 plan was written on 2026-08-11 12:10:00 UTC against an older spec (spec changed 2026-08-11 12:20:00 UTC) — re-read the spec, update the plan (a line recording the re-check is enough) and push it",
		"F-0006": "F-0006 plan was written on 2026-08-11 12:10:00 UTC against an older spec (spec changed unknown) — re-read the spec, update the plan (a line recording the re-check is enough) and push it",
	}
	if len(got) != len(want) {
		t.Fatalf("plan-older-than-spec on %v, want %v", got, want)
	}
	for id, detail := range want {
		f := got[id]
		if f.Detail != detail || f.File != "content/features/"+id+"/plan.md" || Blocking(f) {
			t.Errorf("%s: finding %+v, want detail %q on its plan.md, advisory", id, f, detail)
		}
	}
	if _, ok := ruleRank[RulePlanOlderThanSpec]; !ok {
		t.Fatal("RulePlanOlderThanSpec missing from ruleRank")
	}
}

// TestWriteTime (critic round 1, item 4): a plan and a spec written in the
// same minute print apart — UTC to the second — and an unrecorded write is
// "unknown".
func TestWriteTime(t *testing.T) {
	plan := time.Date(2026, 11, 12, 18, 44, 5, 0, time.FixedZone("BRT", -3*3600))
	spec := plan.Add(40 * time.Second)
	if got := WriteTime(plan); got != "2026-11-12 21:44:05 UTC" {
		t.Fatalf("WriteTime(plan) = %q", got)
	}
	if WriteTime(plan) == WriteTime(spec) {
		t.Fatalf("same-minute writes print alike: %q", WriteTime(spec))
	}
	if got := WriteTime(time.Time{}); got != "unknown" {
		t.Fatalf("WriteTime(zero) = %q, want unknown", got)
	}
}
