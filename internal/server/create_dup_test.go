package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/store"
)

// TestCreateFeatureDup (F-0147 crit. 7): POST /api/features with a title
// similar to a live feature returns 409 with the match named in the body —
// the UI shows the same refusal the CLI prints.
func TestCreateFeatureDup(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	now := time.Now().UTC()
	f := store.Feature{ID: "F-0001", Title: "Corrigir bug do lock do push", State: domain.StateInDevelopment, CreatedAt: now, UpdatedAt: now}
	if err := store.SaveFeature(repoDir, f); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}
	if _, err := gitback.CommitAndPush(repoDir, "seed F-0001"); err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}

	h := New(repoDir, projDir, "proj")
	rec := httptest.NewRecorder()
	body := `{"title":"Corrigir o bug do lock do push urgente"}`
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/features", strings.NewReader(body)))

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "F-0001") {
		t.Fatalf("body does not name the match:\n%s", rec.Body.String())
	}
}
