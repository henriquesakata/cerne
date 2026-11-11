package materialize

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// addRunbook saves a runbook entity with no content document at all: stats must
// still count the entity (count semantics are per entity, bytes per doc).
func addRunbook(t *testing.T, backendDir, id, title string, state domain.RunbookState) {
	t.Helper()
	rb := store.Runbook{ID: id, Title: title, State: state, CreatedAt: seedTime, UpdatedAt: seedTime}
	if err := store.SaveRunbook(backendDir, rb); err != nil {
		t.Fatalf("SaveRunbook %s: %v", id, err)
	}
}

// body returns a deterministic markdown-ish body of exactly n bytes.
func body(n int) string { return strings.Repeat("a", n) }

// seedStatsBackend builds the mixed-state fixture the stats tests assert on.
//
// Corpus (all states):
//
//	feature   F-0001 candidate      spec 100 + errata 40 = 140  (current)
//	          F-0002 implemented    spec 100 + plan 200 + result 300 = 600
//	          F-0003 rejected       spec 50
//	decision  D-0001 active         80                            (current)
//	          D-0002 superseded     120
//	note      N-0001 active         60                            (current)
//	reference R-0001 obsolete       90
//	runbook   RB-0001 active        no doc, 0 bytes               (current)
//
// Totals: count 8 (current 4), bytes 1140 (current 280), saved 860.
func seedStatsBackend(t *testing.T) string {
	t.Helper()
	backend := seedBackend(t)
	addFeature(t, backend, "F-0001", "Login social", "", domain.StateCandidate,
		map[string]string{"spec.md": body(100)})
	if err := os.MkdirAll(filepath.Join(backend, "content", "errata"), 0o755); err != nil {
		t.Fatalf("mkdir errata: %v", err)
	}
	if err := os.WriteFile(filepath.Join(backend, "content", "errata", "F-0001.md"), []byte(body(40)), 0o644); err != nil {
		t.Fatalf("write errata: %v", err)
	}
	addFeature(t, backend, "F-0002", "Tema escuro", "", domain.StateImplemented,
		map[string]string{"spec.md": body(100), "plan.md": body(200), "result.md": body(300)})
	addFeature(t, backend, "F-0003", "Upload FTP", "", domain.StateRejected,
		map[string]string{"spec.md": body(50)})
	addDecision(t, backend, "D-0001", "Usar Postgres", domain.DecisionActive, body(80))
	addDecision(t, backend, "D-0002", "Usar MySQL", domain.DecisionSuperseded, body(120))
	addNote(t, backend, "N-0001", "SSH gotcha", domain.NoteActive, body(60))
	addReference(t, backend, "R-0001", "Arquitetura antiga", domain.ReferenceObsolete, body(90))
	addRunbook(t, backend, "RB-0001", "Deploy", domain.RunbookActive)

	// Wiki and artifacts are excluded from both sides of the metric.
	if err := os.WriteFile(filepath.Join(backend, "content", "wiki", "index.md"), []byte(body(500)), 0o644); err != nil {
		t.Fatalf("write wiki: %v", err)
	}
	if err := os.WriteFile(filepath.Join(backend, "content", "artifacts", "diagram.excalidraw"), []byte(body(700)), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	return backend
}

// TestComputeStats: full corpus vs current projection, per kind and total,
// following exactly the materializer's current-state rules.
func TestComputeStats(t *testing.T) {
	backend := seedStatsBackend(t)

	s, err := ComputeStats(backend)
	if err != nil {
		t.Fatalf("ComputeStats: %v", err)
	}

	wantKinds := map[string]KindStats{
		"feature":   {Count: 3, Bytes: 790, CurrentCount: 1, CurrentBytes: 140},
		"decision":  {Count: 2, Bytes: 200, CurrentCount: 1, CurrentBytes: 80},
		"note":      {Count: 1, Bytes: 60, CurrentCount: 1, CurrentBytes: 60},
		"reference": {Count: 1, Bytes: 90, CurrentCount: 0, CurrentBytes: 0},
		"runbook":   {Count: 1, Bytes: 0, CurrentCount: 1, CurrentBytes: 0},
	}
	if len(s.Kinds) != len(wantKinds) {
		t.Fatalf("kinds = %v, want %d entries", s.Kinds, len(wantKinds))
	}
	for kind, want := range wantKinds {
		if got := s.Kinds[kind]; got != want {
			t.Errorf("Kinds[%q] = %+v, want %+v", kind, got, want)
		}
	}

	if s.TotalCount != 8 || s.CurrentCount != 4 {
		t.Errorf("counts = %d/%d, want total 8 current 4", s.TotalCount, s.CurrentCount)
	}
	if s.TotalBytes != 1140 || s.CurrentBytes != 280 {
		t.Errorf("bytes = %d/%d, want total 1140 current 280", s.TotalBytes, s.CurrentBytes)
	}
	if s.SavedBytes != 860 {
		t.Errorf("SavedBytes = %d, want 860", s.SavedBytes)
	}
	wantPct := float64(860) / float64(1140) * 100
	if s.SavedPct != wantPct {
		t.Errorf("SavedPct = %v, want %v", s.SavedPct, wantPct)
	}
}

// TestComputeStatsTokens: the token estimate is bytes/4 (integer division) on
// every axis, saved included.
func TestComputeStatsTokens(t *testing.T) {
	backend := seedStatsBackend(t)

	s, err := ComputeStats(backend)
	if err != nil {
		t.Fatalf("ComputeStats: %v", err)
	}
	cases := []struct {
		name      string
		got, want int
	}{
		{"total", s.TotalTokensEst, s.TotalBytes / 4},
		{"current", s.CurrentTokensEst, s.CurrentBytes / 4},
		{"saved", s.SavedTokensEst, s.SavedBytes / 4},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s tokens = %d, want %d", c.name, c.got, c.want)
		}
	}
	if s.TotalTokensEst != 285 || s.CurrentTokensEst != 70 || s.SavedTokensEst != 215 {
		t.Errorf("tokens = %d/%d/%d, want 285/70/215", s.TotalTokensEst, s.CurrentTokensEst, s.SavedTokensEst)
	}
}

// TestComputeStatsEmptyBackend: a fresh backend yields zeros everywhere and a
// SavedPct of 0 (never NaN), with every kind key present.
func TestComputeStatsEmptyBackend(t *testing.T) {
	backend := seedBackend(t)

	s, err := ComputeStats(backend)
	if err != nil {
		t.Fatalf("ComputeStats: %v", err)
	}
	if s.TotalCount != 0 || s.CurrentCount != 0 || s.TotalBytes != 0 || s.CurrentBytes != 0 || s.SavedBytes != 0 {
		t.Fatalf("empty backend stats = %+v, want zeros", s)
	}
	if s.SavedPct != 0 {
		t.Fatalf("SavedPct = %v, want 0 (never NaN)", s.SavedPct)
	}
	for _, kind := range []string{"feature", "decision", "reference", "runbook", "note"} {
		if got, ok := s.Kinds[kind]; !ok || got != (KindStats{}) {
			t.Fatalf("Kinds[%q] = %+v (present %v), want zero-value entry", kind, got, ok)
		}
	}
}
