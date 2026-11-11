package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/materialize"
	"github.com/henriquesakata/cerne/internal/store"
)

// seedStatsProject writes entities and content docs straight into the backend
// clone (stats is read-only, so uncommitted working-tree state is fine) with
// exact byte sizes:
//
//	F-0001 candidate    spec 150               (current)
//	F-0002 implemented  spec 300 + result 450
//	D-0001 active       100                    (current)
//
// Corpus: 3 entities / 1000 bytes. Current: 2 / 250. Saved: 750 (75.0%).
func seedStatsProject(t *testing.T, repoDir string) {
	t.Helper()
	now := time.Now().UTC()
	docs := map[string]string{
		"content/features/F-0001/spec.md":   strings.Repeat("a", 150),
		"content/features/F-0002/spec.md":   strings.Repeat("a", 300),
		"content/features/F-0002/result.md": strings.Repeat("a", 450),
		"content/decisions/D-0001.md":       strings.Repeat("a", 100),
	}
	for rel, body := range docs {
		path := filepath.Join(repoDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll %s: %v", rel, err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", rel, err)
		}
	}
	features := []store.Feature{
		{ID: "F-0001", Title: "Login social", State: domain.StateCandidate, CreatedAt: now, UpdatedAt: now},
		{ID: "F-0002", Title: "Tema escuro", State: domain.StateImplemented, CreatedAt: now, UpdatedAt: now},
	}
	for _, f := range features {
		if err := store.SaveFeature(repoDir, f); err != nil {
			t.Fatalf("SaveFeature %s: %v", f.ID, err)
		}
	}
	d := store.Decision{ID: "D-0001", Title: "Usar Postgres", State: domain.DecisionActive, CreatedAt: now, UpdatedAt: now}
	if err := store.SaveDecision(repoDir, d); err != nil {
		t.Fatalf("SaveDecision: %v", err)
	}
}

// TestStats: the human table (English, per F-0124) shows per-kind and total
// corpus vs current projection plus the saved percentage.
func TestSavings(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	seedStatsProject(t, repoDir)

	out, err := execCerne(t, "savings", "--dir", projDir)
	if err != nil {
		t.Fatalf("stats: %v\noutput: %s", err, out)
	}
	for _, want := range []string{
		"KIND", "CORPUS", "CURRENT",
		"feature", "decision",
		"TOTAL", "1000", "250",
		"Saved: 750 bytes", "~187 tokens", "75.0%",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("stats output missing %q:\n%s", want, out)
		}
	}
}

// TestStatsJSON: --json emits the materialize.Stats object with the exact
// fixture numbers.
func TestSavingsJSON(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	seedStatsProject(t, repoDir)

	out, err := execCerne(t, "savings", "--json", "--dir", projDir)
	if err != nil {
		t.Fatalf("stats --json: %v\noutput: %s", err, out)
	}
	var s materialize.Stats
	if err := json.Unmarshal([]byte(out), &s); err != nil {
		t.Fatalf("stats --json is not valid JSON: %v\n%s", err, out)
	}
	if s.TotalCount != 3 || s.CurrentCount != 2 {
		t.Errorf("counts = %d/%d, want 3/2", s.TotalCount, s.CurrentCount)
	}
	if s.TotalBytes != 1000 || s.CurrentBytes != 250 || s.SavedBytes != 750 {
		t.Errorf("bytes = %d/%d/%d, want 1000/250/750", s.TotalBytes, s.CurrentBytes, s.SavedBytes)
	}
	if s.SavedPct != 75 {
		t.Errorf("SavedPct = %v, want 75", s.SavedPct)
	}
	if s.TotalTokensEst != 250 || s.CurrentTokensEst != 62 || s.SavedTokensEst != 187 {
		t.Errorf("tokens = %d/%d/%d, want 250/62/187", s.TotalTokensEst, s.CurrentTokensEst, s.SavedTokensEst)
	}
	if got := s.Kinds["feature"]; got != (materialize.KindStats{Count: 2, Bytes: 900, CurrentCount: 1, CurrentBytes: 150}) {
		t.Errorf("feature kind = %+v", got)
	}
	if got := s.Kinds["decision"]; got != (materialize.KindStats{Count: 1, Bytes: 100, CurrentCount: 1, CurrentBytes: 100}) {
		t.Errorf("decision kind = %+v", got)
	}
}
