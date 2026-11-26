package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
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

// T-0022 crit. 2
// E-0006 crit. 11
func TestRoomCloseUseDraftChangedPrintsNoOpenItemsHint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/close"):
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"error":"the minutes draft changed after it was reviewed","draft_changed":true}`))
		case strings.HasSuffix(r.URL.Path, "/items/pending"):
			_, _ = w.Write([]byte(`{"items":[{"id":"I2","kind":"proposal","author":"ana","title":"usar postgres","source_seq":2,"status":"open"}]}`))
		default:
			_, _ = w.Write([]byte(`{"rooms":[{"id":"R1","topic":"t","participants":[],"minutes_draft":{"minutes":{"conclusion":"c","rationale":"r"},"author":"ana","updated_at":"2026-11-25T14:02:03Z"}}]}`))
		}
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".cernerc"), []byte("project_id = \"proj-draft-changed\"\nbackend = \"/dev/null\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := runRoom(t, "close", "R1", "--use-draft", "--facilitator", "fac", "--dir", dir, "--port", u.Port())
	if err == nil || !strings.Contains(err.Error(), "draft changed") {
		t.Fatalf("close --use-draft err = %v, want the draft-changed refusal", err)
	}
	if strings.Contains(out, "close refused") {
		t.Fatalf("close --use-draft output = %q, want no open-items hint for a changed draft", out)
	}
}

// T-0022 crit. 2
// E-0006 crit. 11
func TestRoomCloseUseDraftWithNoDraftNeverCallsClose(t *testing.T) {
	var closes atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/close") {
			closes.Add(1) // a draft saved after the list would close here
			_, _ = w.Write([]byte(`{"id":"R1"}`))
			return
		}
		_, _ = w.Write([]byte(`{"rooms":[{"id":"R1","topic":"t","participants":[],"active_participants":[]}]}`))
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".cernerc"), []byte("project_id = \"proj-no-draft\"\nbackend = \"/dev/null\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := runRoom(t, "close", "R1", "--use-draft", "--facilitator", "fac", "--dir", dir, "--port", u.Port())
	if err == nil || !strings.Contains(err.Error(), "no minutes draft") {
		t.Fatalf("close --use-draft with no draft err = %v, want the no-draft refusal", err)
	}
	if n := closes.Load(); n != 0 {
		t.Fatalf("close was called %d time(s), want none: the room had no draft when it was read", n)
	}
}
