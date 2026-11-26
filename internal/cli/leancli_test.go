package cli

import (
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/store"
)

func commitsOn(t *testing.T, origin string) int {
	t.Helper()
	out, err := exec.Command("git", "-C", origin, "rev-list", "--count", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-list: %v", err)
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return n
}

// F-0514 crit. 1
func TestFeatureEpicTaskNewWithCapabilityOneCommit(t *testing.T) {
	projDir, origin, repoDir := setupProject(t)
	mustExec(t, projDir, "capability", "new", "Studio")

	before := commitsOn(t, origin)
	out, err := execCerne(t, "epic", "new", "Épico lean", "--context", "why", "--capability", "C-0001", "--dir", projDir)
	if err != nil {
		t.Fatalf("epic new: %v\n%s", err, out)
	}
	if strings.Contains(out, "capability-missing") {
		t.Fatalf("readiness still lists capability-missing:\n%s", out)
	}
	if got := commitsOn(t, origin) - before; got != 1 {
		t.Fatalf("epic new commits = %d, want 1", got)
	}
	e, _ := store.LoadFeature(repoDir, "E-0001")
	if len(e.Capabilities) != 1 || e.Capabilities[0] != "C-0001" {
		t.Fatalf("epic capabilities = %v", e.Capabilities)
	}

	seedApprovedState(t, repoDir, e)
	before = commitsOn(t, origin)
	if out, err = execCerne(t, "task", "new", "Passo", "--parent", "E-0001", "--context", "why", "--capability", "c-0001", "--dir", projDir); err != nil {
		t.Fatalf("task new: %v\n%s", err, out)
	}
	if got := commitsOn(t, origin) - before; got != 1 {
		t.Fatalf("task new commits = %d, want 1", got)
	}
	tk, _ := store.LoadFeature(repoDir, "T-0001")
	if len(tk.Capabilities) != 1 || tk.Capabilities[0] != "C-0001" {
		t.Fatalf("task capabilities = %v", tk.Capabilities)
	}

	if out, err = execCerne(t, "feature", "new", "Feature lean", "--context", "why", "--capability", "C-0001", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v\n%s", err, out)
	}
	if strings.Contains(out, "capability-missing") {
		t.Fatalf("feature readiness lists capability-missing:\n%s", out)
	}
}

// F-0514 crit. 1
func TestFeatureNewBadCapabilityCreatesNothing(t *testing.T) {
	projDir, origin, repoDir := setupProject(t)
	mustExec(t, projDir, "capability", "new", "Studio")
	before := commitsOn(t, origin)
	if out, err := execCerne(t, "feature", "new", "Nasce torta", "--context", "why", "--capability", "C-0001,C-9999", "--dir", projDir); err == nil {
		t.Fatalf("want an error, got:\n%s", out)
	}
	if got := commitsOn(t, origin); got != before {
		t.Fatalf("commits moved on a refused creation")
	}
	if all, _ := store.LoadAllFeatures(repoDir); len(all) != 0 {
		t.Fatalf("orphan card written: %d", len(all))
	}
	mustExec(t, projDir, "feature", "new", "Nasce torta", "--context", "why", "--capability", "C-0001")
	if _, err := store.LoadFeature(repoDir, "F-0001"); err != nil {
		t.Fatalf("retry did not mint F-0001: %v", err)
	}
}

// F-0514 crit. 2
func TestFeatureCapabilityBatch(t *testing.T) {
	projDir, origin, repoDir := setupProject(t)
	mustExec(t, projDir, "capability", "new", "Studio")
	mustExec(t, projDir, "feature", "new", "Primeira coisa", "--context", "why")
	mustExec(t, projDir, "feature", "new", "Segunda coisa", "--context", "why")

	before := commitsOn(t, origin)
	out, err := execCerne(t, "feature", "capability", "F-0001,F-0002", "C-0001", "--dir", projDir)
	if err != nil {
		t.Fatalf("batch: %v\n%s", err, out)
	}
	if got := commitsOn(t, origin) - before; got != 1 {
		t.Fatalf("batch commits = %d, want 1", got)
	}
	for _, line := range []string{"F-0001: capabilities set (1)", "F-0002: capabilities set (1)"} {
		if !strings.Contains(out, line) {
			t.Fatalf("output missing %q:\n%s", line, out)
		}
	}
	if out, err = execCerne(t, "feature", "capability", "F-0001,F-9999", "C-0001", "--dir", projDir); err == nil {
		t.Fatalf("unknown id in batch should fail:\n%s", out)
	}
	if out, err = execCerne(t, "feature", "capability", "F-0001,F-0002", "--clear", "--dir", projDir); err != nil {
		t.Fatalf("clear: %v\n%s", err, out)
	}
	for _, id := range []string{"F-0001", "F-0002"} {
		f, _ := store.LoadFeature(repoDir, id)
		if len(f.Capabilities) != 0 {
			t.Fatalf("%s not cleared", id)
		}
	}
	if !strings.Contains(out, "F-0002: capabilities cleared") {
		t.Fatalf("clear output:\n%s", out)
	}
}
