package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/spectmpl"
	"github.com/henriquesakata/cerne/internal/store"
)

func nextStepRepo(t *testing.T) (proj, repo string) {
	t.Helper()
	proj = t.TempDir()
	repo = filepath.Join(proj, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	return proj, repo
}

func nextStepDoc(t *testing.T, repo, id, name, body string) {
	t.Helper()
	p := FeatureDocPath(repo, id, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

const nextStepGoodSpec = "# t\n\nProblema real.\n\nBarra: referência nomeada — acesso\n"

// TestNextStep (F-0371 crit. 2-11): one row per state, each answer the
// composition of the existing gate.
func TestNextStep(t *testing.T) {
	tag := func(f *store.Feature) { f.Facets, f.Components = []string{"code"}, []string{"app"} }
	deliver := func(f *store.Feature) { f.DeliveryMRURL = "https://example.com/mr/1" }
	cases := []struct {
		name    string
		state   domain.FeatureState
		mut     func(*store.Feature)
		docs    map[string]string
		actor   string
		stepHas string
		command string
	}{
		{"candidate stub spec", domain.StateCandidate, nil, map[string]string{"spec.md": spectmpl.Stub("t")}, ActorAgent, "template stub", "cerne push"},
		{"candidate no bar", domain.StateCandidate, nil, map[string]string{"spec.md": "# t\n\nProblema real.\n"}, ActorAgent, "Barra:", "cerne push"},
		{"candidate ready", domain.StateCandidate, tag, map[string]string{"spec.md": nextStepGoodSpec + "\n"}, ActorHuman, "cerne feature approve F-0001", ""},
		{"candidate ready with clarification", domain.StateCandidate, tag, map[string]string{"spec.md": nextStepGoodSpec + "\nX [ESCLARECER: qual o limite?]\n"}, ActorHuman, "qual o limite?", ""},
		{"approved stale", domain.StateApproved, nil, map[string]string{"spec.md": "# novo\n"}, ActorHuman, "re-approve", ""},
		{"approved stub plan", domain.StateApproved, nil, map[string]string{"spec.md": nextStepGoodSpec}, ActorAgent, "content/features/F-0001/plan.md", "cerne push"},
		{"approved real plan", domain.StateApproved, nil, map[string]string{"spec.md": nextStepGoodSpec, "plan.md": "# plano\n\nPassos reais do plano.\n"}, ActorAgent, "start", "cerne feature start F-0001"},
		{"in development stub result", domain.StateInDevelopment, nil, map[string]string{"spec.md": nextStepGoodSpec, "plan.md": "# plano\n\nreal.\n", "result.md": "# r\n\n## Aceitação\n\ncritério 1 — ATENDIDO\n"}, ActorAgent, "would refuse", "cerne push"},
		{"implemented no delivery", domain.StateImplemented, nil, nil, ActorAgent, "cerne feature deliver F-0001 --mr <url>", "cerne feature deliver F-0001 --mr <url>"},
		{"implemented complete", domain.StateImplemented, deliver, nil, ActorHuman, "cerne feature accept F-0001", ""},
		{"done", domain.StateDone, nil, nil, "", "no next step", ""},
		{"rejected", domain.StateRejected, nil, nil, "", "no next step", ""},
		{"superseded", domain.StateSuperseded, nil, nil, "", "no next step", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			proj, repo := nextStepRepo(t)
			f := store.Feature{ID: "F-0001", Title: "t", State: tc.state}
			if tc.mut != nil {
				tc.mut(&f)
			}
			if err := store.SaveFeature(repo, f); err != nil {
				t.Fatal(err)
			}
			for name, body := range tc.docs {
				nextStepDoc(t, repo, "F-0001", name, body)
			}
			if tc.state == domain.StateApproved && tc.name == "approved stale" {
				if err := os.MkdirAll(filepath.Dir(store.ApprovedSpecPath(repo, "F-0001")), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(store.ApprovedSpecPath(repo, "F-0001"), []byte("# velho\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			got, err := NextStep(repo, proj, "f-0001")
			if err != nil {
				t.Fatal(err)
			}
			if got.State != string(tc.state) || got.Actor != tc.actor || got.Command != tc.command || !strings.Contains(got.Step, tc.stepHas) {
				t.Errorf("NextStep = %+v; want actor %q command %q step containing %q", got, tc.actor, tc.command, tc.stepHas)
			}
		})
	}
}

// TestNextStepUnknownID (crit. 11): the error names the id.
func TestNextStepUnknownID(t *testing.T) {
	proj, repo := nextStepRepo(t)
	if _, err := NextStep(repo, proj, "F-9999"); err == nil || !strings.Contains(err.Error(), "F-9999") {
		t.Fatalf("err = %v", err)
	}
}
