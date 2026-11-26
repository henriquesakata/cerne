package cli

import (
	"path/filepath"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
)

// demoFeatures reads every feature the demo's bare backend holds, through a
// fresh clone.
func demoFeatures(t *testing.T, backend string) []store.Feature {
	t.Helper()
	clone := filepath.Join(t.TempDir(), "clone")
	if err := gitback.Clone(backend, clone); err != nil {
		t.Fatal(err)
	}
	all, err := store.LoadAllFeatures(clone)
	if err != nil {
		t.Fatal(err)
	}
	return all
}

// TestDemo_SeededCardsCarryNoAutoMark: the demo is a script, not an agent
// crossing gates. Run from an agent's shell, under the auto gates it opens
// while seeding, it leaves no seeded card marked; a real agent approve in
// the demo project, under the auto gate, still marks.
// F-0515 crit. 6
func TestDemo_SeededCardsCarryNoAutoMark(t *testing.T) {
	demoEnv(t)
	t.Setenv("CERNE_ACTOR", "agent")
	t.Setenv("CLAUDECODE", "1")
	if out, err := runDemoCmd(t); err != nil {
		t.Fatalf("cerne demo: %v\n%s", err, out)
	}
	abs, _ := filepath.Abs("cerne-demo")
	backend, proj := filepath.Join(abs, "taskflow-docs.git"), filepath.Join(abs, "taskflow")
	var candidate string
	for _, f := range demoFeatures(t, backend) {
		if f.ApprovedVia != "" || f.AcceptedVia != "" {
			t.Errorf("seeded %s (%s) = approved_via %q, accepted_via %q; want both empty", f.ID, f.State, f.ApprovedVia, f.AcceptedVia)
		}
		if f.State == domain.StateCandidate {
			candidate = f.ID
		}
	}
	if candidate == "" {
		t.Fatal("the demo seeded no candidate to approve")
	}

	if err := project.SetSetting(proj, "gates.approve", project.ScopeProject, "auto"); err != nil {
		t.Fatal(err)
	}
	if out, err := execCerne(t, "feature", "approve", candidate, "--dir", proj); err != nil {
		t.Fatalf("agent approve of %s: %v\n%s", candidate, err, out)
	}
	for _, f := range demoFeatures(t, backend) {
		if f.ID == candidate && f.ApprovedVia != "auto" {
			t.Errorf("%s after a real agent approve = approved_via %q, want auto", f.ID, f.ApprovedVia)
		}
	}
}
