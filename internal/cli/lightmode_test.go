package cli

import (
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
)

// setGauntlet rewrites the project's .cernerc with [gates] gauntlet = v.
func setGauntlet(t *testing.T, projDir, v string) {
	t.Helper()
	cfg, _, err := project.LoadConfig(projDir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	cfg.Gates.Gauntlet = v
	if err := project.SaveConfig(projDir, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
}

// TestApprove_LightModeCLI (F-0364 crit. 1): in light mode the check calls a
// spec with no bar approvable, without asking for a bar, and the approve
// command takes it.
func TestApprove_LightModeCLI(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	setGauntlet(t, projDir, "off")
	id := newFeature(t, projDir, "Feature sem barra", 1)
	writeDoc(t, repoDir, id, "spec.md", "---\ncerne_id: "+id+"\n---\n\n# Título\n\n## Problema\n\nProblema real descrito aqui.\n")
	commitBackend(t, repoDir)

	out, err := execCerne(t, "feature", "approve", id, "--check", "--dir", projDir)
	if err != nil || !strings.Contains(out, "is approvable") || !strings.Contains(out, "light mode") || strings.Contains(out, "Barra:") {
		t.Fatalf("check err = %v, want approvable in light mode with no bar forms\n%s", err, out)
	}
	if out, err := execCerne(t, "feature", "approve", id, "--dir", projDir); err != nil {
		t.Fatalf("approve: %v\n%s", err, out)
	}
	if f, _ := store.LoadFeature(repoDir, id); f.State != domain.StateApproved {
		t.Fatalf("state = %s, want approved", f.State)
	}
}

// TestLint_LightModeCLI (F-0364 crit. 3): 'cerne lint' in light mode does not
// charge spec-missing-bar on a candidate whose spec has no bar.
func TestLint_LightModeCLI(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	id := newFeature(t, projDir, "Feature sem barra", 1)
	writeDoc(t, repoDir, id, "spec.md", "---\ncerne_id: "+id+"\n---\n\n# Título\n\n## Problema\n\nProblema real descrito aqui.\n")
	commitBackend(t, repoDir)

	out, _ := execCerne(t, "lint", "--dir", projDir)
	if !strings.Contains(out, "SPEC-MISSING-BAR") {
		t.Fatalf("gauntlet on: lint lacks spec-missing-bar\n%s", out)
	}
	setGauntlet(t, projDir, "off")
	out, _ = execCerne(t, "lint", "--dir", projDir)
	if strings.Contains(out, "SPEC-MISSING-BAR") {
		t.Fatalf("light mode: lint still charges spec-missing-bar\n%s", out)
	}
}
