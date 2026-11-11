package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/materialize"
	"github.com/henriquesakata/cerne/internal/store"
)

// TestSavingsEndpoint: GET /api/savings returns the context-savings metric with
// the exact fixture numbers — corpus (all states) vs current projection.
func TestSavingsEndpoint(t *testing.T) {
	repoDir, projDir := newBackend(t)
	now := time.Now().UTC()
	for _, f := range []store.Feature{
		{ID: "F-0001", Title: "Login social", State: domain.StateCandidate, CreatedAt: now, UpdatedAt: now},
		{ID: "F-0002", Title: "Tema escuro", State: domain.StateImplemented, CreatedAt: now, UpdatedAt: now},
	} {
		if err := store.SaveFeature(repoDir, f); err != nil {
			t.Fatalf("SaveFeature %s: %v", f.ID, err)
		}
	}
	d := store.Decision{ID: "D-0001", Title: "Usar Postgres", State: domain.DecisionActive, CreatedAt: now, UpdatedAt: now}
	if err := store.SaveDecision(repoDir, d); err != nil {
		t.Fatalf("SaveDecision: %v", err)
	}
	// F-0001 candidate: spec 150 (current). F-0002 implemented: spec 300 +
	// result 450. D-0001 active: 100 (current). Corpus 1000 B, current 250 B.
	for rel, size := range map[string]int{
		"content/features/F-0001/spec.md":   150,
		"content/features/F-0002/spec.md":   300,
		"content/features/F-0002/result.md": 450,
		"content/decisions/D-0001.md":       100,
	} {
		path := filepath.Join(repoDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll %s: %v", rel, err)
		}
		if err := os.WriteFile(path, []byte(strings.Repeat("a", size)), 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", rel, err)
		}
	}

	h := New(repoDir, projDir, "proj")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/savings", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var s materialize.Stats
	if err := json.NewDecoder(rec.Body).Decode(&s); err != nil {
		t.Fatalf("decode: %v", err)
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
}
