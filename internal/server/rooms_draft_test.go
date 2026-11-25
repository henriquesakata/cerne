package server

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// draftJSON mirrors room.MinutesDraft over HTTP (T-0022).
type draftJSON struct {
	Minutes   minutesJSON `json:"minutes"`
	Author    string      `json:"author"`
	UpdatedAt time.Time   `json:"updated_at"`
}

// draftRoomJSON is a listed room with its current minutes draft.
type draftRoomJSON struct {
	roomJSON
	MinutesDraft *draftJSON `json:"minutes_draft"`
}

// openDraftRoom opens a room and joins alice.
func openDraftRoom(t *testing.T, pid string) (h http.Handler, opened openJSON, alice sessionJSON) {
	t.Helper()
	h = newRoomServer(t, pid)
	doJSON(t, h, http.MethodPost, "/api/rooms", `{"topic":"t"}`, &opened)
	return h, opened, joinAs(t, h, opened.ID, "alice")
}

func listedRoom(t *testing.T, h http.Handler, id string) (draftRoomJSON, string) {
	t.Helper()
	var list struct {
		Rooms []draftRoomJSON `json:"rooms"`
	}
	rec := doJSON(t, h, http.MethodGet, "/api/rooms", "", &list)
	for _, r := range list.Rooms {
		if r.ID == id {
			return r, rec.Body.String()
		}
	}
	t.Fatalf("GET /api/rooms has no %s: %s", id, rec.Body.String())
	return draftRoomJSON{}, ""
}

// T-0022 crit. 1
// E-0006 crit. 10
func TestMinutesDraftEndpoint(t *testing.T) {
	h, opened, alice := openDraftRoom(t, "proj-minutes-draft")
	path := "/api/rooms/" + opened.ID + "/minutes/draft"

	var d draftJSON
	rec := doJSON(t, h, http.MethodPost, path, `{"session_id":"`+alice.SessionID+`","minutes":{"conclusion":"reverter","rationale":"rapido"}}`, &d)
	if rec.Code != http.StatusOK || d.Author != "alice" || d.Minutes.Conclusion != "reverter" || d.UpdatedAt.IsZero() {
		t.Fatalf("draft by session = %d %+v", rec.Code, d)
	}
	bodies := []string{rec.Body.String()}
	rec = doJSON(t, h, http.MethodPost, path, `{"facilitator_id":"`+opened.FacilitatorID+`","minutes":{"conclusion":"v2","rationale":"rapido"}}`, &d)
	if rec.Code != http.StatusOK || d.Author != "facilitator" || d.Minutes.Conclusion != "v2" {
		t.Fatalf("draft by facilitator = %d %+v", rec.Code, d)
	}
	bodies = append(bodies, rec.Body.String())

	r, listed := listedRoom(t, h, opened.ID)
	if r.ClosedAt != nil || r.MinutesDraft == nil || r.MinutesDraft.Author != "facilitator" || r.MinutesDraft.Minutes.Conclusion != "v2" {
		t.Fatalf("listed room = %+v, want it open with the facilitator's draft", r)
	}
	var read struct {
		Messages []messageJSON `json:"messages"`
	}
	rec = doJSON(t, h, http.MethodGet, "/api/rooms/"+opened.ID+"/messages?since=0", "", &read)
	drafts := 0
	for _, m := range read.Messages {
		if m.Type == "minutes_draft" {
			drafts++
		}
	}
	if drafts != 2 {
		t.Fatalf("transcript minutes_draft events = %d, want 2: %+v", drafts, read.Messages)
	}
	for _, body := range append(bodies, listed, rec.Body.String()) {
		for _, secret := range []string{alice.SessionID, opened.FacilitatorID, "session_id", "facilitator_id", "hash"} {
			if strings.Contains(body, secret) {
				t.Fatalf("a draft response serves %q: %s", secret, body)
			}
		}
	}
}

// T-0022 crit. 1
// E-0006 crit. 10
func TestMinutesDraftEndpointRefusals(t *testing.T) {
	h, opened, alice := openDraftRoom(t, "proj-minutes-draft-refusals")
	path := "/api/rooms/" + opened.ID + "/minutes/draft"
	ok := `"minutes":{"conclusion":"ok","rationale":"ok"}`
	for _, tc := range []struct {
		name, body string
		want       int
	}{
		{"incomplete", `{"session_id":"` + alice.SessionID + `","minutes":{"conclusion":"ok","rationale":""}}`, http.StatusUnprocessableEntity},
		{"no credential", `{` + ok + `}`, http.StatusUnauthorized},
		{"unknown session", `{"session_id":"deadbeef",` + ok + `}`, http.StatusUnauthorized},
		{"session as facilitator", `{"facilitator_id":"` + alice.SessionID + `",` + ok + `}`, http.StatusForbidden},
		{"bad json", `{`, http.StatusBadRequest},
	} {
		var got map[string]any
		if rec := doJSON(t, h, http.MethodPost, path, tc.body, &got); rec.Code != tc.want {
			t.Errorf("%s: status = %d, want %d; body: %s", tc.name, rec.Code, tc.want, rec.Body.String())
		}
	}
	var got map[string]string
	if r, _ := listedRoom(t, h, opened.ID); r.MinutesDraft != nil {
		t.Fatalf("a refused draft was saved: %+v", r.MinutesDraft)
	}
	closeMinimalRoom(t, h, opened.ID, opened.FacilitatorID)
	if rec := doJSON(t, h, http.MethodPost, path, `{"session_id":"`+alice.SessionID+`",`+ok+`}`, &got); rec.Code != http.StatusConflict {
		t.Fatalf("draft in a closed room status = %d, want 409", rec.Code)
	}
}

// T-0022 crit. 1
// E-0006 crit. 10
func TestMinutesDraftEndpointRefusesAnOversizedBody(t *testing.T) {
	h, opened, alice := openDraftRoom(t, "proj-minutes-draft-413")
	huge := strings.Repeat("x", 300<<10)
	body := `{"session_id":"` + alice.SessionID + `","minutes":{"conclusion":"` + huge + `","rationale":"r"}}`
	var got map[string]string
	if rec := doJSON(t, h, http.MethodPost, "/api/rooms/"+opened.ID+"/minutes/draft", body, &got); rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized draft status = %d, want 413", rec.Code)
	}
	if r, _ := listedRoom(t, h, opened.ID); r.MinutesDraft != nil {
		t.Fatal("an oversized draft was saved")
	}
}

// T-0022 crit. 2, 3
// E-0006 crit. 11, 12
func TestCloseUseDraftChecksTheReviewedDraft(t *testing.T) {
	h, opened, alice := openDraftRoom(t, "proj-close-reviewed-draft")
	save := func(conclusion string) draftJSON {
		var d draftJSON
		body := `{"session_id":"` + alice.SessionID + `","minutes":{"conclusion":"` + conclusion + `","rationale":"r"}}`
		if rec := doJSON(t, h, http.MethodPost, "/api/rooms/"+opened.ID+"/minutes/draft", body, &d); rec.Code != http.StatusOK {
			t.Fatalf("save draft status = %d; body: %s", rec.Code, rec.Body.String())
		}
		return d
	}
	closeAt := func(at time.Time, out any) int {
		body := `{"facilitator_id":"` + opened.FacilitatorID + `","use_draft":true,"draft_updated_at":"` + at.Format(time.RFC3339Nano) + `"}`
		return doJSON(t, h, http.MethodPost, "/api/rooms/"+opened.ID+"/close", body, out).Code
	}
	seen := save("a revisada")
	time.Sleep(2 * time.Millisecond)
	current := save("a nova")

	var refused map[string]any
	if code := closeAt(seen.UpdatedAt, &refused); code != http.StatusConflict || refused["draft_changed"] != true {
		t.Fatalf("close with a stale draft = %d %v, want 409 with draft_changed", code, refused)
	}
	var closed draftRoomJSON
	if code := closeAt(current.UpdatedAt, &closed); code != http.StatusOK || closed.Minutes == nil || closed.Minutes.Conclusion != "a nova" {
		t.Fatalf("close with the current draft = %d %+v", code, closed)
	}
}

// T-0022 crit. 2
// E-0006 crit. 11
func TestCloseUseDraft(t *testing.T) {
	h, opened, alice := openDraftRoom(t, "proj-close-use-draft")
	closePath := "/api/rooms/" + opened.ID + "/close"
	useDraft := func(cred string) string { return `{"facilitator_id":"` + cred + `","use_draft":true}` }

	var got map[string]any
	if rec := doJSON(t, h, http.MethodPost, closePath, useDraft(opened.FacilitatorID), &got); rec.Code != http.StatusConflict {
		t.Fatalf("use_draft with no draft status = %d, want 409; body: %s", rec.Code, rec.Body.String())
	}
	draft := `{"session_id":"` + alice.SessionID + `","minutes":{"conclusion":"reverter","rationale":"rapido","next_steps":["post-mortem"]}}`
	if rec := doJSON(t, h, http.MethodPost, "/api/rooms/"+opened.ID+"/minutes/draft", draft, &draftJSON{}); rec.Code != http.StatusOK {
		t.Fatalf("save draft status = %d; body: %s", rec.Code, rec.Body.String())
	}
	if rec := doJSON(t, h, http.MethodPost, closePath, useDraft(alice.SessionID), &got); rec.Code != http.StatusForbidden {
		t.Fatalf("use_draft by a participant status = %d, want 403", rec.Code)
	}
	both := `{"facilitator_id":"` + opened.FacilitatorID + `","use_draft":true,"minutes":{"conclusion":"x","rationale":"y"}}`
	if rec := doJSON(t, h, http.MethodPost, closePath, both, &got); rec.Code != http.StatusBadRequest {
		t.Fatalf("use_draft with minutes status = %d, want 400", rec.Code)
	}

	var closed draftRoomJSON
	rec := doJSON(t, h, http.MethodPost, closePath, useDraft(opened.FacilitatorID), &closed)
	if rec.Code != http.StatusOK || closed.ClosedAt == nil || closed.Minutes == nil || closed.Minutes.Conclusion != "reverter" || len(closed.Minutes.NextSteps) != 1 {
		t.Fatalf("close use_draft = %d %+v, want closed with the draft", rec.Code, closed)
	}
	if closed.MinutesDraft != nil {
		t.Fatalf("closed room still serves a draft: %+v", closed.MinutesDraft)
	}
}
