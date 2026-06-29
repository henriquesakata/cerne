package lint

import (
	"testing"

	"gitlab.com/rockt/cerne/internal/store"
)

// TestAcknowledgedOnLine pins crit. 1: the lint:ok-superseded marker is parsed
// from a single line and matched against an id (case-insensitive, comma list,
// space-tolerant).
func TestAcknowledgedOnLine(t *testing.T) {
	tests := []struct {
		name string
		line string
		id   string
		want bool
	}{
		{"single id", "veja D-0003 <!-- lint:ok-superseded D-0003 -->", "D-0003", true},
		{"id case-insensitive", "veja D-0003 <!-- lint:ok-superseded d-0003 -->", "D-0003", true},
		{"query case-insensitive", "veja <!-- lint:ok-superseded D-0003 -->", "d-0003", true},
		{"comma list first", "<!-- lint:ok-superseded D-0003, D-0005 -->", "D-0003", true},
		{"comma list second", "<!-- lint:ok-superseded D-0003, D-0005 -->", "D-0005", true},
		{"comma no space", "<!-- lint:ok-superseded D-0003,D-0005 -->", "D-0005", true},
		{"tight comment", "x<!--lint:ok-superseded D-0003-->y", "D-0003", true},
		{"no marker", "menciona D-0003 que está superseded", "D-0003", false},
		{"marker names other id", "veja D-0003 <!-- lint:ok-superseded D-0005 -->", "D-0003", false},
		{"empty line", "", "D-0003", false},
		{"feature id", "F-0003 antigo <!-- lint:ok-superseded F-0003 -->", "F-0003", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := acknowledgedOnLine(tt.line, tt.id); got != tt.want {
				t.Fatalf("acknowledgedOnLine(%q, %q) = %v, want %v", tt.line, tt.id, got, tt.want)
			}
		})
	}
}

// TestAckSuppressesSameLine pins crit. 2: an authored doc citing a superseded
// entity with a marker on the same line emits no ref-to-superseded.
func TestAckSuppressesSameLine(t *testing.T) {
	dir := t.TempDir()
	if err := store.InitLayout(dir, store.Project{Name: "Demo"}); err != nil {
		t.Fatalf("InitLayout: %v", err)
	}
	supersededDecisionPair(t, dir)
	writeLiveFeatureDoc(t, dir, "F-0001", "Antes usávamos D-0003 (trade-off X). <!-- lint:ok-superseded D-0003 -->\n")

	findings, err := Run(dir)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if refs := refToSuperseded(findings); len(refs) != 0 {
		t.Fatalf("acknowledged historical mention must not be flagged, got: %+v", refs)
	}
}

// TestAckOtherIdStillFlagged pins crit. 3: a marker naming a different id does
// not suppress the mention.
func TestAckOtherIdStillFlagged(t *testing.T) {
	dir := t.TempDir()
	if err := store.InitLayout(dir, store.Project{Name: "Demo"}); err != nil {
		t.Fatalf("InitLayout: %v", err)
	}
	supersededDecisionPair(t, dir)
	writeLiveFeatureDoc(t, dir, "F-0001", "menciona D-0003 <!-- lint:ok-superseded D-0005 -->\n")

	findings, err := Run(dir)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if refs := refToSuperseded(findings); len(refs) != 1 {
		t.Fatalf("marker for a different id must not suppress, want 1 finding, got: %+v", refs)
	}
}

// TestAckPerLineGranularity pins crit. 4: with two lines citing the same
// superseded id, only the marked one is tolerated — the unmarked one is still a
// debt (one collapsed finding for the entity).
func TestAckPerLineGranularity(t *testing.T) {
	dir := t.TempDir()
	if err := store.InitLayout(dir, store.Project{Name: "Demo"}); err != nil {
		t.Fatalf("InitLayout: %v", err)
	}
	supersededDecisionPair(t, dir)
	writeLiveFeatureDoc(t, dir, "F-0001", "Histórico: D-0003 <!-- lint:ok-superseded D-0003 -->\nVeja ainda D-0003 nesta seção.\n")

	findings, err := Run(dir)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if refs := refToSuperseded(findings); len(refs) != 1 {
		t.Fatalf("an unmarked second mention must still flag, want 1 finding, got: %+v", refs)
	}
}

// TestAckMarkerOnlyLineSelfSuppresses pins crit. 5: a line carrying only the
// marker (the id appears inside it) emits no finding.
func TestAckMarkerOnlyLineSelfSuppresses(t *testing.T) {
	dir := t.TempDir()
	if err := store.InitLayout(dir, store.Project{Name: "Demo"}); err != nil {
		t.Fatalf("InitLayout: %v", err)
	}
	supersededDecisionPair(t, dir)
	writeLiveFeatureDoc(t, dir, "F-0001", "# Notas\n\n<!-- lint:ok-superseded D-0003 -->\n")

	findings, err := Run(dir)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if refs := refToSuperseded(findings); len(refs) != 0 {
		t.Fatalf("a marker-only line must self-suppress, got: %+v", refs)
	}
}
