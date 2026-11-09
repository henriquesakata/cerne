package lint

import (
	"strings"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

// F-0874 r1: a malformed or step-less manual precondition is pointed at; valid
// ones and superseded runbooks are not; the rule never blocks.
func TestPreconditionFindings(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	rbs := []store.Runbook{
		{ID: "RB-0001", Title: "A", State: domain.RunbookActive},
		{ID: "RB-0002", Title: "B", State: domain.RunbookSuperseded},
	}
	for _, rb := range rbs {
		rb.CreatedAt, rb.UpdatedAt = now, now
		if err := store.SaveRunbook(dir, rb); err != nil {
			t.Fatal(err)
		}
	}
	writeDoc(t, dir, "content/runbooks/RB-0001.md", "# A\n\n- S1 x\n"+
		"Pré-condição S1: registro F-0001\n"+
		"Pré-condição S1: registro  F-1 extra\n"+
		"Pré-condição S1: mr a!--web merged\n"+
		"Pré-condição: janela de manutenção aprovada\n")
	writeDoc(t, dir, "content/runbooks/RB-0002.md", "# B\n\nPré-condição: sem passo\n")
	got, err := preconditionFindings(dir, rbs)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 findings (RB-0002 is superseded, the valid line is fine): %+v", got)
	}
	for _, f := range got {
		if f.Rule != RulePreconditionMalformed || f.Entity != "RB-0001" || !strings.Contains(f.Detail, "pré-condição") {
			t.Errorf("bad finding %+v", f)
		}
	}
	if !Advisory(RulePreconditionMalformed) {
		t.Fatal("must be advisory")
	}
}
