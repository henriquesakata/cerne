package cli

import (
	"path/filepath"
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
	if !strings.Contains(stderr, "warning: "+id+" plan predates the current spec (plan written ") || !strings.Contains(stderr, "re-read the spec, update the plan (a line recording the re-check is enough) and push it") {
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

// TestPushPlanClearsPlanOlderThanSpec (F-0377 crit. 2, 4; critic round 1,
// item 3) through the real 'cerne push': the spec pushed after the plan raises
// the finding; pushing the plan unchanged sends nothing, so the finding stays
// and says what clears it; the plan edited and pushed clears it.
func TestPushPlanClearsPlanOlderThanSpec(t *testing.T) {
	projDir, _, _, specPath := pulledProject(t)
	planPath := filepath.Join(filepath.Dir(specPath), "plan.md")
	if out, err := execCerne(t, "feature", "approve", "F-0001", "--dir", projDir); err != nil {
		t.Fatalf("approve: %v\n%s", err, out)
	}
	if out, err := execCerne(t, "pull", "--dir", projDir); err != nil {
		t.Fatalf("pull: %v\n%s", err, out)
	}
	push := func(want string) {
		t.Helper()
		out, err := execCerne(t, "push", "--dir", projDir)
		if err != nil || !strings.Contains(out, want) {
			t.Fatalf("push: %v, output:\n%s\nwant %q", err, out, want)
		}
	}
	lintOut := func() string {
		t.Helper()
		out, _ := execCerne(t, "lint", "--dir", projDir)
		return out
	}
	plan := realDoc("F-0001", "Passo 1: os seis critérios.")
	mustWrite(t, planPath, plan)
	push("Sent: 1 document(s)")
	if out := lintOut(); strings.Contains(out, "PLAN-OLDER-THAN-SPEC") {
		t.Fatalf("plan pushed after the spec, lint flags it:\n%s", out)
	}

	mustWrite(t, specPath, realDoc("F-0001", "Dividir compras em parcelas, agora com quinze critérios."))
	push("Sent: 1 document(s)")
	const detail = "F-0001 plan was written on "
	const clears = "re-read the spec, update the plan (a line recording the re-check is enough) and push it"
	if out := lintOut(); !strings.Contains(out, "PLAN-OLDER-THAN-SPEC (1) (warning)") || !strings.Contains(out, detail) || !strings.Contains(out, clears) {
		t.Fatalf("spec pushed after the plan: lint output\n%s\nwant plan-older-than-spec saying %q", out, clears)
	}

	push("Nothing to send.") // the plan unchanged: no write, no event
	if out := lintOut(); !strings.Contains(out, "PLAN-OLDER-THAN-SPEC (1) (warning)") || !strings.Contains(out, clears) {
		t.Fatalf("unchanged plan push cleared the finding:\n%s", out)
	}

	mustWrite(t, planPath, plan+"\nRevisto contra a spec de quinze critérios.\n")
	push("Sent: 1 document(s)")
	if out := lintOut(); strings.Contains(out, "PLAN-OLDER-THAN-SPEC") {
		t.Fatalf("plan edited and pushed, lint still flags it:\n%s", out)
	}
}
