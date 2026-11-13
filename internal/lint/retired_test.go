package lint

import (
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// TestRetiredDecisionIsANeedle (F-0385): a retired decision is history like a
// superseded one, so a live doc that still cites it is ref-to-superseded.
func TestRetiredDecisionIsANeedle(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	if err := store.SaveFeature(dir, store.Feature{ID: "F-0001", Title: "Núcleo", State: domain.StateCandidate, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}
	if err := store.SaveDecision(dir, store.Decision{ID: "D-0004", Title: "Usar fila", State: domain.DecisionRetired, Reason: "gone", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("SaveDecision: %v", err)
	}
	writeDoc(t, dir, "content/features/F-0001/spec.md", "---\ncerne_id: F-0001\n---\n\n# Núcleo\n\nSegue a D-0004.\n")

	findings, err := Run(dir)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, f := range findings {
		if f.Rule == RuleRefToSuperseded && f.File == "content/features/F-0001/spec.md" && strings.Contains(f.Detail, "D-0004") && strings.Contains(f.Detail, "retired") {
			return
		}
	}
	t.Fatalf("no ref-to-superseded naming retired D-0004: %+v", findings)
}
