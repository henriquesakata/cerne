package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// T-0022 crit. 2, 3
// E-0006 crit. 11, 12
func TestRoomCloseWithDraftSendsTheReviewedDraft(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = nil
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"R1"}`))
	}))
	defer srv.Close()
	c := roomClient{base: srv.URL}

	reviewed := time.Date(2026, 11, 25, 14, 2, 3, 456789000, time.UTC)
	if _, err := c.closeWithDraft(context.Background(), "R1", "fac", reviewed, false); err != nil {
		t.Fatalf("closeWithDraft: %v", err)
	}
	if got["use_draft"] != true || got["draft_updated_at"] != reviewed.Format(time.RFC3339Nano) || got["facilitator_id"] != "fac" {
		t.Fatalf("close body = %v, want use_draft with the reviewed draft's updated_at", got)
	}
	if _, has := got["minutes"]; has {
		t.Fatalf("close body = %v, want no minutes beside use_draft", got)
	}
	if _, err := c.closeWithDraft(context.Background(), "R1", "fac", time.Time{}, false); err != nil {
		t.Fatalf("closeWithDraft without a reviewed draft: %v", err)
	}
	if _, has := got["draft_updated_at"]; has {
		t.Fatalf("close body = %v, want no draft_updated_at for a zero time", got)
	}
}

