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
	"github.com/henriquesakata/cerne/internal/store"
)

// seedHandoffs writes two works and five handoffs, one in each closing shape,
// with timestamps chosen so ID order differs from the expected list order.
func seedHandoffs(t *testing.T, repoDir string) {
	t.Helper()
	now := time.Now().UTC()
	at := func(d time.Duration) time.Time { return now.Add(-d) }
	for _, w := range []store.Work{
		{ID: "W-0010", Title: "Investigar erro X", State: domain.WorkActive, CreatedAt: now, UpdatedAt: now},
		{ID: "W-0011", Title: "Refatorar Y", State: domain.WorkDone, CreatedAt: now, UpdatedAt: now},
	} {
		if err := store.SaveWork(repoDir, w); err != nil {
			t.Fatalf("SaveWork %s: %v", w.ID, err)
		}
	}
	taken := at(2 * time.Hour)
	for _, h := range []store.Handoff{
		{ID: "HO-0001", State: domain.HandoffPending, FromWork: "W-0010", Actor: "a@b", Next: "rodar X", CreatedAt: at(3 * time.Hour), UpdatedAt: at(3 * time.Hour)},
		{ID: "HO-0002", State: domain.HandoffTaken, FromWork: "W-0011", Actor: "a@b", Next: "rodar Y", TakenBy: "c@d", TakenWork: "W-0012", TakenNote: "entendi Y", TakenAt: &taken, CreatedAt: at(4 * time.Hour), UpdatedAt: taken},
		{ID: "HO-0003", State: domain.HandoffPending, FromWork: "W-0099", Actor: "a@b", Next: "rodar Z", CreatedAt: at(1 * time.Hour), UpdatedAt: at(1 * time.Hour)},
		{ID: "HO-0004", State: domain.HandoffCancelled, FromWork: "W-0010", Actor: "a@b", Next: "n", CancelReason: "desisti", CreatedAt: at(6 * time.Hour), UpdatedAt: at(30 * time.Minute)},
		{ID: "HO-0005", State: domain.HandoffSuperseded, FromWork: "W-0010", Actor: "a@b", Next: "n", SupersededBy: "HO-0001", CreatedAt: at(7 * time.Hour), UpdatedAt: at(5 * time.Hour)},
	} {
		if err := store.SaveHandoff(repoDir, h); err != nil {
			t.Fatalf("SaveHandoff %s: %v", h.ID, err)
		}
		if err := store.SaveHandoffContent(repoDir, h.ID, "brief de "+h.ID+"\n\nPróximo passo: "+h.Next+"\n"); err != nil {
			t.Fatalf("SaveHandoffContent %s: %v", h.ID, err)
		}
	}
}

func getHandoffs(t *testing.T, repoDir, projDir, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	New(repoDir, projDir, "proj").ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

// TestHandoffsEndpointOrder: GET /api/handoffs returns every HO, pending first
// (oldest first), then the closed ones by closing time (newest first), each
// with its origin work's title — "" when that work is gone (F-0386 crit. 1–2).
func TestHandoffsEndpointOrder(t *testing.T) {
	repoDir, projDir := newBackend(t)
	seedHandoffs(t, repoDir)

	rec := getHandoffs(t, repoDir, projDir, "/api/handoffs")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var rows []handoffRow
	if err := json.NewDecoder(rec.Body).Decode(&rows); err != nil {
		t.Fatalf("decode: %v", err)
	}
	var ids []string
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	if got, want := strings.Join(ids, ","), "HO-0001,HO-0003,HO-0004,HO-0002,HO-0005"; got != want {
		t.Fatalf("order = %s, want %s", got, want)
	}
	if rows[0].FromWorkTitle != "Investigar erro X" {
		t.Fatalf("HO-0001 from_work_title = %q, want the W-0010 title", rows[0].FromWorkTitle)
	}
	if rows[1].FromWorkTitle != "" {
		t.Fatalf("HO-0003 from_work_title = %q, want empty (W-0099 is gone)", rows[1].FromWorkTitle)
	}
	if r := rows[3]; r.TakenBy != "c@d" || r.TakenWork != "W-0012" || r.TakenAt == nil || r.TakenNote != "entendi Y" {
		t.Fatalf("HO-0002 = %+v, want the taken fields", r)
	}
}

// TestHandoffsEndpointPreF0368Backend: a backend without entities/handoffs
// answers [] (not null, not 500).
func TestHandoffsEndpointPreF0368Backend(t *testing.T) {
	repoDir, projDir := newBackend(t)
	if err := os.RemoveAll(filepath.Join(repoDir, "entities", "handoffs")); err != nil {
		t.Fatal(err)
	}
	rec := getHandoffs(t, repoDir, projDir, "/api/handoffs")
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("got %d %q, want 200 []", rec.Code, rec.Body.String())
	}
}

// TestHandoffDetailEndpoint: GET /api/handoffs/{id} returns the record plus
// the brief (F-0386 crit. 1, 4).
func TestHandoffDetailEndpoint(t *testing.T) {
	repoDir, projDir := newBackend(t)
	seedHandoffs(t, repoDir)

	rec := getHandoffs(t, repoDir, projDir, "/api/handoffs/HO-0002")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var d handoffDetail
	if err := json.NewDecoder(rec.Body).Decode(&d); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if d.ID != "HO-0002" || d.State != domain.HandoffTaken || d.FromWorkTitle != "Refatorar Y" {
		t.Fatalf("detail = %+v, want HO-0002 taken from Refatorar Y", d)
	}
	if !strings.Contains(d.Brief, "brief de HO-0002") {
		t.Fatalf("brief = %q, want the content/handoffs body", d.Brief)
	}
}

// TestHandoffDetailNotFound: a malformed or unknown id is a 404 with an error
// body — the id never reaches the filesystem unvalidated.
func TestHandoffDetailNotFound(t *testing.T) {
	repoDir, projDir := newBackend(t)
	seedHandoffs(t, repoDir)
	for _, id := range []string{"HO-9999", "HO-12", "ho-0001", "W-0010", "HO-0001x", "..%2Fx"} {
		rec := getHandoffs(t, repoDir, projDir, "/api/handoffs/"+id)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", id, rec.Code)
			continue
		}
		var body map[string]string
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil || body["error"] == "" {
			t.Errorf("%s: body = %v (%v), want an error message", id, body, err)
		}
	}
}

// TestHandoffsEndpointOrderTies: equal timestamps keep id order — two pending
// created at the same instant, two closed at the same instant (one by
// taken_at, one by updated_at) — so the list never reshuffles between polls.
func TestHandoffsEndpointOrderTies(t *testing.T) {
	repoDir, projDir := newBackend(t)
	same := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	closed := same.Add(30 * time.Minute)
	for _, h := range []store.Handoff{
		{ID: "HO-0009", State: domain.HandoffCancelled, FromWork: "W-1", CancelReason: "x", CreatedAt: same, UpdatedAt: closed},
		{ID: "HO-0010", State: domain.HandoffPending, FromWork: "W-1", CreatedAt: same, UpdatedAt: same},
		{ID: "HO-0011", State: domain.HandoffTaken, FromWork: "W-2", TakenAt: &closed, CreatedAt: same, UpdatedAt: closed.Add(time.Minute)},
		{ID: "HO-0012", State: domain.HandoffPending, FromWork: "W-2", CreatedAt: same, UpdatedAt: same},
	} {
		if err := store.SaveHandoff(repoDir, h); err != nil {
			t.Fatalf("SaveHandoff %s: %v", h.ID, err)
		}
	}
	for range 3 {
		rec := getHandoffs(t, repoDir, projDir, "/api/handoffs")
		var rows []handoffRow
		if err := json.NewDecoder(rec.Body).Decode(&rows); err != nil {
			t.Fatalf("decode: %v", err)
		}
		var ids []string
		for _, r := range rows {
			ids = append(ids, r.ID)
		}
		if got, want := strings.Join(ids, ","), "HO-0010,HO-0012,HO-0009,HO-0011"; got != want {
			t.Fatalf("order = %s, want %s (ties keep id order)", got, want)
		}
	}
}
