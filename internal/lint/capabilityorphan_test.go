package lint

import (
	"testing"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

// TestCapabilityOrphanFindings pins F-0188 criterion 7: once the project has
// at least one active capability, a live feature (candidate/approved/
// in_development) with no capability link gets the advisory; the implemented
// backlog and terminal features are spared. With no capabilities at all the
// rule is silent — a project that never opted into the taxonomy sees nothing.
func TestCapabilityOrphanFindings(t *testing.T) {
	features := []store.Feature{
		{ID: "F-0001", Title: "viva sem capacidade", State: domain.StateCandidate},                                     // flagged
		{ID: "F-0002", Title: "viva linkada", State: domain.StateInDevelopment, Capabilities: []string{"C-0001"}},      // linked → skip
		{ID: "F-0003", Title: "implemented sem capacidade", State: domain.StateImplemented},                            // backlog → skip
		{ID: "F-0004", Title: "rejeitada", State: domain.StateRejected},                                                // history → skip
		{ID: "F-0005", Title: "approved sem capacidade", State: domain.StateApproved},                                  // flagged
	}
	caps := []store.Capability{{ID: "C-0001", Title: "Studio", State: domain.CapabilityActive}}

	got := capabilityOrphanFindings(features, caps)
	if len(got) != 2 {
		t.Fatalf("findings = %+v, want 2 (F-0001, F-0005)", got)
	}
	if got[0].Entity != "F-0001" || got[1].Entity != "F-0005" {
		t.Fatalf("entities = %s, %s", got[0].Entity, got[1].Entity)
	}
	if got[0].Rule != RuleCapabilityOrphan {
		t.Fatalf("rule = %q", got[0].Rule)
	}

	// Sem capacidade ativa (nenhuma, ou só aposentadas) → regra silenciosa.
	if got := capabilityOrphanFindings(features, nil); len(got) != 0 {
		t.Fatalf("no capabilities: findings = %+v, want none", got)
	}
	retired := []store.Capability{{ID: "C-0001", Title: "Studio", State: domain.CapabilitySuperseded}}
	if got := capabilityOrphanFindings(features, retired); len(got) != 0 {
		t.Fatalf("only retired capabilities: findings = %+v, want none", got)
	}
}
