package cli

import (
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// TestCompleteRiskHeldStoresReasons: a held complete stores on the card the
// lines it prints, in order; a low-risk accept leaves the field empty.
// F-0521 crit. 1
func TestCompleteRiskHeldStoresReasons(t *testing.T) {
	projDir, repoDir := riskProject(t, []string{"code", "auth"}, "empate", true, map[string]string{
		"go.mod": "module x\n", "internal/x/x_test.go": "package x\n\n// F-0001 crit. 1\n"})
	t.Setenv("CERNE_ACTOR", "agent")
	out, err := execCerne(t, "feature", "complete", "F-0001", "--no-stamp", "--dir", projDir)
	if err != nil {
		t.Fatalf("complete: %v\n%s", err, out)
	}
	f, err := store.LoadFeature(repoDir, "F-0001")
	if err != nil {
		t.Fatal(err)
	}
	if len(f.HeldRisk) != 3 {
		t.Fatalf("held_risk = %q, want 3 lines", f.HeldRisk)
	}
	var printed []string
	_, after, _ := strings.Cut(out, "what held it:\n")
	for _, l := range strings.Split(after, "\n") {
		rest, ok := strings.CutPrefix(l, "  - ")
		if !ok {
			break
		}
		printed = append(printed, rest)
	}
	if strings.Join(printed, "\n") != strings.Join(f.HeldRisk, "\n") {
		t.Errorf("held_risk = %q, printed %q", f.HeldRisk, printed)
	}
}

// F-0521 crit. 1
func TestCompleteRiskLowLeavesHeldRiskEmpty(t *testing.T) {
	projDir, repoDir := riskProject(t, []string{"code"}, "vitória", false, map[string]string{
		"internal/x/x.go": "package x\n", "internal/x/x_test.go": "package x\n\n// F-0001 crit. 1, 2\n"})
	t.Setenv("CERNE_ACTOR", "agent")
	if out, err := execCerne(t, "feature", "complete", "F-0001", "--no-stamp", "--dir", projDir); err != nil {
		t.Fatalf("complete: %v\n%s", err, out)
	}
	if f, _ := store.LoadFeature(repoDir, "F-0001"); f.State != domain.StateDone || len(f.HeldRisk) != 0 {
		t.Errorf("low-risk card = %s, held_risk %q; want done and empty", f.State, f.HeldRisk)
	}
}
