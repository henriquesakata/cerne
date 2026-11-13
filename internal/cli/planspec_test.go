package cli

import (
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/app"
)

// TestFeatureStartWarnsPlanBehindSpec (F-0377 crit. 2, 3, 4): the plan pushed,
// then the spec rewritten and re-approved — 'cerne lint' warns
// plan-older-than-spec, 'cerne feature start' prints the warning on stderr and
// still moves the feature, and the plan pushed again clears the finding.
func TestFeatureStartWarnsPlanBehindSpec(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	id := newFeature(t, projDir, "Plano velho", 1)
	writeDoc(t, repoDir, id, "spec.md", realDoc(id, "Seis critérios."))
	commitBackend(t, repoDir)
	if _, err := execCerne(t, "feature", "approve", id, "--dir", projDir); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if err := app.SaveFeatureDoc(repoDir, projDir, id, "plan", realDoc(id, "Passo 1: os seis.")); err != nil {
		t.Fatal(err)
	}
	if err := app.SaveFeatureDoc(repoDir, projDir, id, "spec", realDoc(id, "Quinze critérios.")); err != nil {
		t.Fatal(err)
	}
	if _, err := execCerne(t, "feature", "approve", id, "--dir", projDir); err != nil {
		t.Fatalf("re-approve: %v", err)
	}

	out, err := execCerne(t, "lint", "--dir", projDir)
	if !strings.Contains(out, "PLAN-OLDER-THAN-SPEC (1) (warning)") || !strings.Contains(out, id+" plan was written on ") {
		t.Fatalf("lint: err %v, output:\n%s\nwant an advisory plan-older-than-spec on %s", err, out, id)
	}

	stdout, stderr, err := execCerneSplit(t, "feature", "start", id, "--dir", projDir)
	if err != nil {
		t.Fatalf("start refused: %v\nstderr: %s", err, stderr)
	}
	if !strings.Contains(stderr, "warning: "+id+" plan predates the current spec (plan written ") || !strings.Contains(stderr, "re-read the spec and push the plan again") {
		t.Fatalf("start stderr = %q, want the plan warning", stderr)
	}
	if strings.Contains(stdout, "plan predates") || !strings.Contains(stdout, "approved → in_development") {
		t.Fatalf("start stdout = %q, want the transition and no warning", stdout)
	}

	if err := app.SaveFeatureDoc(repoDir, projDir, id, "plan", realDoc(id, "Passo 1: os quinze.")); err != nil {
		t.Fatal(err)
	}
	if out, _ := execCerne(t, "lint", "--dir", projDir); strings.Contains(out, "PLAN-OLDER-THAN-SPEC") {
		t.Fatalf("plan pushed again, lint still flags it:\n%s", out)
	}
}
