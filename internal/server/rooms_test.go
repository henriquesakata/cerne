package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/project"
	"gitlab.com/rockt/cerne/internal/room"
	"gitlab.com/rockt/cerne/internal/store"
)

// roomJSON/messageJSON mirror the HTTP contract of the war room endpoints
// (T-0001) — a black-box view, independent of internal/room's Go types.
type roomJSON struct {
	ID           string            `json:"id"`
	Topic        string            `json:"topic"`
	ClosedAt     *time.Time        `json:"closed_at"`
	Participants []participantJSON `json:"participants"`
	Minutes      *minutesJSON      `json:"minutes"`
}

// openJSON is the POST /api/rooms response (F-0228): the room snapshot plus
// the private facilitator capability, minted once and never repeated.
type openJSON struct {
	roomJSON      `json:"room"`
	FacilitatorID string `json:"facilitator_id"`
}

type minutesJSON struct {
	Conclusion       string   `json:"conclusion"`
	Rationale        string   `json:"rationale"`
	DiscardedOptions []string `json:"discarded_options"`
	Owners           []string `json:"owners"`
	NextSteps        []string `json:"next_steps"`
	OpenItems        []string `json:"open_items"`
}

type participantJSON struct {
	Nick string `json:"nick"`
	Bio  string `json:"bio"`
}

type messageJSON struct {
	Seq  int    `json:"seq"`
	Type string `json:"type"`
	Nick string `json:"nick"`
	Body string `json:"body"`
	Own  *bool  `json:"own"`
}

// sessionJSON mirrors room.Session over HTTP (F-0226).
type sessionJSON struct {
	SessionID string `json:"session_id"`
	RoomID    string `json:"room_id"`
	Nick      string `json:"nick"`
	Bio       string `json:"bio"`
	LastSeq   int    `json:"last_seq"`
}

// joinJSON is the POST .../join response: the room snapshot plus the private
// session capability (F-0226).
type joinJSON struct {
	Room    roomJSON    `json:"room"`
	Session sessionJSON `json:"session"`
}

func newRoomServer(t *testing.T, projectID string) http.Handler {
	t.Helper()
	repoDir, projDir := newBackend(t)
	t.Setenv("CERNE_CACHE_DIR", t.TempDir())
	return New(repoDir, projDir, projectID)
}

// joinAs opens no room; it joins nick into id and returns the session.
func joinAs(t *testing.T, h http.Handler, id, nick string) sessionJSON {
	t.Helper()
	var got joinJSON
	rec := doJSON(t, h, http.MethodPost, "/api/rooms/"+id+"/join", `{"nick":"`+nick+`"}`, &got)
	if rec.Code != http.StatusOK {
		t.Fatalf("join %s status = %d; body: %s", nick, rec.Code, rec.Body.String())
	}
	return got.Session
}

func TestRoomOpenEndpoint(t *testing.T) {
	h := newRoomServer(t, "proj-room-open")
	var got openJSON
	rec := doJSON(t, h, http.MethodPost, "/api/rooms", `{"topic":"deploy quebrado"}`, &got)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", rec.Code, rec.Body.String())
	}
	if got.ID != "R1" || got.Topic != "deploy quebrado" || len(got.Participants) != 0 {
		t.Fatalf("room = %+v", got)
	}
	if len(got.FacilitatorID) < 32 {
		t.Fatalf("facilitator_id = %q, want >= 32 hex chars", got.FacilitatorID)
	}
}

func TestRoomJoinReturnsRoomAndSession(t *testing.T) {
	h := newRoomServer(t, "proj-room-join-session")
	var opened openJSON
	doJSON(t, h, http.MethodPost, "/api/rooms", `{"topic":"t"}`, &opened)

	var got joinJSON
	rec := doJSON(t, h, http.MethodPost, "/api/rooms/"+opened.ID+"/join", `{"nick":"alice","bio":"otimista"}`, &got)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if len(got.Room.Participants) != 1 || got.Room.Participants[0].Nick != "alice" {
		t.Fatalf("room = %+v", got.Room)
	}
	s := got.Session
	if len(s.SessionID) < 32 || s.RoomID != opened.ID || s.Nick != "alice" || s.Bio != "otimista" || s.LastSeq == 0 {
		t.Fatalf("session = %+v", s)
	}
}

// TestRoomSessionNeverLeaksInPublicReads pins criterion 1/10: the capability
// (and its hash) appear in the join response only — never in room list or
// transcript reads.
func TestRoomSessionNeverLeaksInPublicReads(t *testing.T) {
	h := newRoomServer(t, "proj-room-leak")
	var opened openJSON
	doJSON(t, h, http.MethodPost, "/api/rooms", `{"topic":"t"}`, &opened)
	sess := joinAs(t, h, opened.ID, "alice")

	for _, path := range []string{"/api/rooms", "/api/rooms/" + opened.ID + "/messages?since=0"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if strings.Contains(rec.Body.String(), sess.SessionID) {
			t.Fatalf("GET %s leaks the session capability: %s", path, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "session") {
			t.Fatalf("GET %s exposes session fields: %s", path, rec.Body.String())
		}
	}
}

func TestRoomResumeReturnsSessionWithoutEvent(t *testing.T) {
	h := newRoomServer(t, "proj-room-resume")
	var opened openJSON
	doJSON(t, h, http.MethodPost, "/api/rooms", `{"topic":"t"}`, &opened)
	sess := joinAs(t, h, opened.ID, "alice")

	var before struct {
		Messages []messageJSON `json:"messages"`
	}
	doJSON(t, h, http.MethodGet, "/api/rooms/"+opened.ID+"/messages?since=0", "", &before)

	var got sessionJSON
	rec := doJSON(t, h, http.MethodPost, "/api/rooms/"+opened.ID+"/sessions/resume", `{"session_id":"`+sess.SessionID+`"}`, &got)
	if rec.Code != http.StatusOK || got.Nick != "alice" || got.SessionID != sess.SessionID {
		t.Fatalf("resume status=%d session=%+v", rec.Code, got)
	}

	var after struct {
		Messages []messageJSON `json:"messages"`
	}
	doJSON(t, h, http.MethodGet, "/api/rooms/"+opened.ID+"/messages?since=0", "", &after)
	if len(after.Messages) != len(before.Messages) {
		t.Fatalf("resume appended an event: %d -> %d", len(before.Messages), len(after.Messages))
	}

	var errBody map[string]string
	rec = doJSON(t, h, http.MethodPost, "/api/rooms/"+opened.ID+"/sessions/resume", `{"session_id":"deadbeef"}`, &errBody)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("resume with bogus capability status = %d, want 401", rec.Code)
	}
}

func TestRoomJoinDuplicateNickConflict(t *testing.T) {
	h := newRoomServer(t, "proj-room-join-dup")
	var opened openJSON
	doJSON(t, h, http.MethodPost, "/api/rooms", `{"topic":"t"}`, &opened)
	joinAs(t, h, opened.ID, "alice")
	var errBody map[string]string
	rec := doJSON(t, h, http.MethodPost, "/api/rooms/"+opened.ID+"/join", `{"nick":"alice"}`, &errBody)
	if rec.Code != http.StatusConflict {
		t.Fatalf("second join status = %d, want 409; body: %s", rec.Code, rec.Body.String())
	}
}

func TestRoomJoinUnknownRoomNotFound(t *testing.T) {
	h := newRoomServer(t, "proj-room-join-404")
	var errBody map[string]string
	rec := doJSON(t, h, http.MethodPost, "/api/rooms/R99/join", `{"nick":"alice"}`, &errBody)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body: %s", rec.Code, rec.Body.String())
	}
}

// TestRoomSayRequiresSession pins criteria 2/3: the legacy nick payload and
// bogus capabilities are refused with 401 and a migration hint; the nick is
// never accepted as authority.
func TestRoomSayRequiresSession(t *testing.T) {
	h := newRoomServer(t, "proj-room-say-auth")
	var opened openJSON
	doJSON(t, h, http.MethodPost, "/api/rooms", `{"topic":"t"}`, &opened)
	joinAs(t, h, opened.ID, "alice")

	cases := []struct{ name, payload string }{
		{"legacy nick payload", `{"nick":"alice","body":"oi"}`},
		{"bogus capability", `{"session_id":"deadbeefdeadbeefdeadbeefdeadbeef","body":"oi"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var errBody map[string]string
			rec := doJSON(t, h, http.MethodPost, "/api/rooms/"+opened.ID+"/messages", tc.payload, &errBody)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401; body: %s", rec.Code, rec.Body.String())
			}
		})
	}
	var errBody map[string]string
	rec := doJSON(t, h, http.MethodPost, "/api/rooms/"+opened.ID+"/messages", `{"nick":"alice","body":"oi"}`, &errBody)
	if !strings.Contains(errBody["error"], "room join") {
		t.Fatalf("error = %q, want a migration hint mentioning room join; code %d", errBody["error"], rec.Code)
	}
}

func TestRoomSayAndReadSince(t *testing.T) {
	h := newRoomServer(t, "proj-room-say")
	var opened openJSON
	doJSON(t, h, http.MethodPost, "/api/rooms", `{"topic":"t"}`, &opened)
	sess := joinAs(t, h, opened.ID, "alice")

	var said messageJSON
	rec := doJSON(t, h, http.MethodPost, "/api/rooms/"+opened.ID+"/messages", `{"session_id":"`+sess.SessionID+`","body":"oi pessoal"}`, &said)
	if rec.Code != http.StatusCreated || said.Body != "oi pessoal" || said.Seq == 0 {
		t.Fatalf("say status=%d body=%+v", rec.Code, said)
	}
	if said.Nick != "alice" {
		t.Fatalf("author = %q, want alice derived from the session", said.Nick)
	}

	var got struct {
		Messages []messageJSON `json:"messages"`
	}
	rec = doJSON(t, h, http.MethodGet, "/api/rooms/"+opened.ID+"/messages?since=0", "", &got)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	var found bool
	for _, m := range got.Messages {
		if m.Type == "message" && m.Body == "oi pessoal" {
			found = true
		}
		if m.Own != nil {
			t.Fatalf("public read projects own: %+v", m)
		}
	}
	if !found {
		t.Fatalf("messages = %+v, want the said message", got.Messages)
	}
}

// TestRoomReadAuthenticatedProjectsOwn pins criterion 10: with a valid
// X-Room-Session header every event carries own true/false; an invalid
// header is 401.
func TestRoomReadAuthenticatedProjectsOwn(t *testing.T) {
	h := newRoomServer(t, "proj-room-own")
	var opened openJSON
	doJSON(t, h, http.MethodPost, "/api/rooms", `{"topic":"t"}`, &opened)
	alice := joinAs(t, h, opened.ID, "alice")
	bob := joinAs(t, h, opened.ID, "bob")
	doJSON(t, h, http.MethodPost, "/api/rooms/"+opened.ID+"/messages", `{"session_id":"`+alice.SessionID+`","body":"minha"}`, &messageJSON{})
	doJSON(t, h, http.MethodPost, "/api/rooms/"+opened.ID+"/messages", `{"session_id":"`+bob.SessionID+`","body":"do bob"}`, &messageJSON{})

	req := httptest.NewRequest(http.MethodGet, "/api/rooms/"+opened.ID+"/messages?since=0", nil)
	req.Header.Set("X-Room-Session", alice.SessionID)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), alice.SessionID) || strings.Contains(rec.Body.String(), bob.SessionID) {
		t.Fatalf("authenticated read leaks a capability: %s", rec.Body.String())
	}
	var got struct {
		Messages []messageJSON `json:"messages"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON %q: %v", rec.Body.String(), err)
	}
	for _, m := range got.Messages {
		if m.Own == nil {
			t.Fatalf("event %+v missing own on authenticated read", m)
		}
		wantOwn := m.Nick == "alice"
		if *m.Own != wantOwn {
			t.Fatalf("event %+v own = %v, want %v", m, *m.Own, wantOwn)
		}
	}

	req = httptest.NewRequest(http.MethodGet, "/api/rooms/"+opened.ID+"/messages?since=0", nil)
	req.Header.Set("X-Room-Session", "deadbeef")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("read with bogus capability status = %d, want 401", rec.Code)
	}
}

func TestRoomCloseRejectsJoinAndSayButKeepsRead(t *testing.T) {
	h := newRoomServer(t, "proj-room-close")
	var opened openJSON
	doJSON(t, h, http.MethodPost, "/api/rooms", `{"topic":"t"}`, &opened)
	sess := joinAs(t, h, opened.ID, "alice")
	doJSON(t, h, http.MethodPost, "/api/rooms/"+opened.ID+"/messages", `{"session_id":"`+sess.SessionID+`","body":"oi"}`, &messageJSON{})

	var closed roomJSON
	closeBody := `{"facilitator_id":"` + opened.FacilitatorID + `","minutes":{"conclusion":"encerrado","rationale":"fim do teste"}}`
	rec := doJSON(t, h, http.MethodPost, "/api/rooms/"+opened.ID+"/close", closeBody, &closed)
	if rec.Code != http.StatusOK || closed.ClosedAt == nil {
		t.Fatalf("close status=%d body=%+v", rec.Code, closed)
	}

	var errBody map[string]string
	rec = doJSON(t, h, http.MethodPost, "/api/rooms/"+opened.ID+"/join", `{"nick":"bob"}`, &errBody)
	if rec.Code != http.StatusConflict {
		t.Fatalf("join after close status = %d, want 409", rec.Code)
	}
	rec = doJSON(t, h, http.MethodPost, "/api/rooms/"+opened.ID+"/messages", `{"session_id":"`+sess.SessionID+`","body":"oi de novo"}`, &errBody)
	if rec.Code != http.StatusConflict {
		t.Fatalf("say after close status = %d, want 409", rec.Code)
	}

	var got struct {
		Messages []messageJSON `json:"messages"`
	}
	rec = doJSON(t, h, http.MethodGet, "/api/rooms/"+opened.ID+"/messages?since=0", "", &got)
	if rec.Code != http.StatusOK || len(got.Messages) == 0 {
		t.Fatalf("read after close status=%d messages=%+v, want the preserved transcript", rec.Code, got.Messages)
	}
}

// TestRoomReadFollowWakesOnNewMessage exercises the long-poll path end to
// end through the HTTP handler: a GET with follow=1 blocks in one goroutine
// until a concurrent POST .../messages wakes it — the mechanic T-0001 exists
// for (criterion 4 of the T-0001 spec).
// --- F-0228: facilitator, items, minutes ---

// itemJSON mirrors room.FacilitationItem over HTTP.
type itemJSON struct {
	ID            string `json:"id"`
	Kind          string `json:"kind"`
	Author        string `json:"author"`
	Title         string `json:"title"`
	Body          string `json:"body"`
	SourceSeq     int    `json:"source_seq"`
	Status        string `json:"status"`
	Result        string `json:"result"`
	ResolutionSeq int    `json:"resolution_seq"`
}

func TestFacilitatorIDNeverLeaksInPublicReadsOrList(t *testing.T) {
	h := newRoomServer(t, "proj-fac-leak")
	var opened openJSON
	doJSON(t, h, http.MethodPost, "/api/rooms", `{"topic":"t"}`, &opened)

	for _, path := range []string{"/api/rooms", "/api/rooms/" + opened.ID + "/messages?since=0"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if strings.Contains(rec.Body.String(), opened.FacilitatorID) {
			t.Fatalf("GET %s leaks the facilitator capability: %s", path, rec.Body.String())
		}
	}
}

func TestHandoffFacilitatorRotatesAndRejectsOld(t *testing.T) {
	h := newRoomServer(t, "proj-fac-handoff")
	var opened openJSON
	doJSON(t, h, http.MethodPost, "/api/rooms", `{"topic":"t"}`, &opened)

	var got map[string]string
	rec := doJSON(t, h, http.MethodPost, "/api/rooms/"+opened.ID+"/facilitator/handoff", `{"facilitator_id":"`+opened.FacilitatorID+`"}`, &got)
	if rec.Code != http.StatusOK || len(got["facilitator_id"]) < 32 || got["facilitator_id"] == opened.FacilitatorID {
		t.Fatalf("handoff status=%d body=%+v", rec.Code, got)
	}

	closeBody := `{"facilitator_id":"` + opened.FacilitatorID + `","minutes":{"conclusion":"ok","rationale":"ok"}}`
	var errBody map[string]string
	rec = doJSON(t, h, http.MethodPost, "/api/rooms/"+opened.ID+"/close", closeBody, &errBody)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("close with rotated-out capability status = %d, want 403; body: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateItemRequiresSession(t *testing.T) {
	h := newRoomServer(t, "proj-item-auth")
	var opened openJSON
	doJSON(t, h, http.MethodPost, "/api/rooms", `{"topic":"t"}`, &opened)

	var errBody map[string]string
	rec := doJSON(t, h, http.MethodPost, "/api/rooms/"+opened.ID+"/items", `{"kind":"proposal","title":"t","body":"b"}`, &errBody)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("create item without session status = %d, want 401; body: %s", rec.Code, rec.Body.String())
	}

	sess := joinAs(t, h, opened.ID, "alice")
	var item itemJSON
	rec = doJSON(t, h, http.MethodPost, "/api/rooms/"+opened.ID+"/items", `{"session_id":"`+sess.SessionID+`","kind":"proposal","title":"usar postgres","body":"menos manutencao"}`, &item)
	if rec.Code != http.StatusCreated || item.Kind != "proposal" || item.Author != "alice" || item.Status != "open" {
		t.Fatalf("create item status=%d item=%+v", rec.Code, item)
	}
}

func TestPendingRequiresFacilitator(t *testing.T) {
	h := newRoomServer(t, "proj-pending-auth")
	var opened openJSON
	doJSON(t, h, http.MethodPost, "/api/rooms", `{"topic":"t"}`, &opened)
	sess := joinAs(t, h, opened.ID, "alice")
	var item itemJSON
	doJSON(t, h, http.MethodPost, "/api/rooms/"+opened.ID+"/items", `{"session_id":"`+sess.SessionID+`","kind":"proposal","title":"t","body":"b"}`, &item)

	req := httptest.NewRequest(http.MethodGet, "/api/rooms/"+opened.ID+"/items/pending", nil)
	req.Header.Set("X-Room-Facilitator", "deadbeefdeadbeefdeadbeefdeadbeef")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("pending with bogus facilitator status = %d, want 403", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/rooms/"+opened.ID+"/items/pending", nil)
	req.Header.Set("X-Room-Facilitator", opened.FacilitatorID)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var got struct {
		Items []itemJSON `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON %q: %v", rec.Body.String(), err)
	}
	if rec.Code != http.StatusOK || len(got.Items) != 1 || got.Items[0].ID != item.ID {
		t.Fatalf("pending status=%d items=%+v", rec.Code, got.Items)
	}
}

func TestResolveRequiresFacilitatorReturns403ForSession(t *testing.T) {
	h := newRoomServer(t, "proj-resolve-auth")
	var opened openJSON
	doJSON(t, h, http.MethodPost, "/api/rooms", `{"topic":"t"}`, &opened)
	sess := joinAs(t, h, opened.ID, "alice")
	var item itemJSON
	doJSON(t, h, http.MethodPost, "/api/rooms/"+opened.ID+"/items", `{"session_id":"`+sess.SessionID+`","kind":"proposal","title":"t","body":"b"}`, &item)

	var errBody map[string]string
	rec := doJSON(t, h, http.MethodPost, "/api/rooms/"+opened.ID+"/items/"+item.ID+"/resolve", `{"facilitator_id":"`+sess.SessionID+`","result":"aceito"}`, &errBody)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("resolve with a session (not facilitator) capability status = %d, want 403", rec.Code)
	}

	var resolved itemJSON
	rec = doJSON(t, h, http.MethodPost, "/api/rooms/"+opened.ID+"/items/"+item.ID+"/resolve", `{"facilitator_id":"`+opened.FacilitatorID+`","result":"aceito"}`, &resolved)
	if rec.Code != http.StatusOK || resolved.Status != "resolved" || resolved.Result != "aceito" {
		t.Fatalf("resolve status=%d item=%+v", rec.Code, resolved)
	}
}

func TestCloseReturns422OnIncompleteMinutes(t *testing.T) {
	h := newRoomServer(t, "proj-close-422")
	var opened openJSON
	doJSON(t, h, http.MethodPost, "/api/rooms", `{"topic":"t"}`, &opened)

	var errBody map[string]string
	rec := doJSON(t, h, http.MethodPost, "/api/rooms/"+opened.ID+"/close", `{"facilitator_id":"`+opened.FacilitatorID+`","minutes":{"conclusion":"","rationale":""}}`, &errBody)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("close with incomplete minutes status = %d, want 422; body: %s", rec.Code, rec.Body.String())
	}
}

func TestCloseReturns409OnOpenItemsWithoutOverride(t *testing.T) {
	h := newRoomServer(t, "proj-close-409")
	var opened openJSON
	doJSON(t, h, http.MethodPost, "/api/rooms", `{"topic":"t"}`, &opened)
	sess := joinAs(t, h, opened.ID, "alice")
	var item itemJSON
	doJSON(t, h, http.MethodPost, "/api/rooms/"+opened.ID+"/items", `{"session_id":"`+sess.SessionID+`","kind":"proposal","title":"t","body":"b"}`, &item)

	var got map[string]any
	closeBody := `{"facilitator_id":"` + opened.FacilitatorID + `","minutes":{"conclusion":"ok","rationale":"ok"}}`
	rec := doJSON(t, h, http.MethodPost, "/api/rooms/"+opened.ID+"/close", closeBody, &got)
	if rec.Code != http.StatusConflict {
		t.Fatalf("close with an open item status = %d, want 409; body: %s", rec.Code, rec.Body.String())
	}
	if got["pending"] == nil {
		t.Fatalf("409 body = %+v, want the pending items listed", got)
	}
}

func TestCloseWithAllowOpenItemsSucceeds(t *testing.T) {
	h := newRoomServer(t, "proj-close-override")
	var opened openJSON
	doJSON(t, h, http.MethodPost, "/api/rooms", `{"topic":"t"}`, &opened)
	sess := joinAs(t, h, opened.ID, "alice")
	var item itemJSON
	doJSON(t, h, http.MethodPost, "/api/rooms/"+opened.ID+"/items", `{"session_id":"`+sess.SessionID+`","kind":"proposal","title":"t","body":"b"}`, &item)

	var closed roomJSON
	closeBody := `{"facilitator_id":"` + opened.FacilitatorID + `","minutes":{"conclusion":"ok","rationale":"ok"},"allow_open_items":true}`
	rec := doJSON(t, h, http.MethodPost, "/api/rooms/"+opened.ID+"/close", closeBody, &closed)
	if rec.Code != http.StatusOK || closed.ClosedAt == nil {
		t.Fatalf("close --allow-open-items status=%d body=%+v", rec.Code, closed)
	}
	if closed.Minutes == nil || len(closed.Minutes.OpenItems) != 1 || closed.Minutes.OpenItems[0] != item.ID {
		t.Fatalf("closed minutes = %+v, want the open item recorded as an exception", closed.Minutes)
	}
}

func TestRoomReadFollowWakesOnNewMessage(t *testing.T) {
	h := newRoomServer(t, "proj-room-follow")
	var opened openJSON
	doJSON(t, h, http.MethodPost, "/api/rooms", `{"topic":"t"}`, &opened)
	sess := joinAs(t, h, opened.ID, "alice")

	var baseline struct {
		Messages []messageJSON `json:"messages"`
	}
	doJSON(t, h, http.MethodGet, "/api/rooms/"+opened.ID+"/messages?since=0", "", &baseline)
	since := baseline.Messages[len(baseline.Messages)-1].Seq

	type result struct {
		code int
		body string
	}
	done := make(chan result, 1)
	go func() {
		req := httptest.NewRequest(http.MethodGet, "/api/rooms/"+opened.ID+"/messages?since="+strconv.Itoa(since)+"&follow=1", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		done <- result{rec.Code, rec.Body.String()}
	}()

	time.Sleep(50 * time.Millisecond)
	var said messageJSON
	rec := doJSON(t, h, http.MethodPost, "/api/rooms/"+opened.ID+"/messages", `{"session_id":"`+sess.SessionID+`","body":"acordou"}`, &said)
	if rec.Code != http.StatusCreated {
		t.Fatalf("say status = %d, want 201; body: %s", rec.Code, rec.Body.String())
	}

	select {
	case res := <-done:
		if res.code != http.StatusOK {
			t.Fatalf("follow status = %d, want 200; body: %s", res.code, res.body)
		}
		if !strings.Contains(res.body, "acordou") {
			t.Fatalf("follow body = %s, want the new message", res.body)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("GET .../messages?follow=1 did not wake up on the new message")
	}
}

// --- F-0238: the sala->nota vínculo endpoint (POST .../minutes/note) ---

func closeMinimalRoom(t *testing.T, h http.Handler, id, facilitatorID string) roomJSON {
	t.Helper()
	var closed roomJSON
	closeBody := `{"facilitator_id":"` + facilitatorID + `","minutes":{"conclusion":"ok","rationale":"ok"}}`
	rec := doJSON(t, h, http.MethodPost, "/api/rooms/"+id+"/close", closeBody, &closed)
	if rec.Code != http.StatusOK {
		t.Fatalf("close status=%d body=%+v", rec.Code, closed)
	}
	return closed
}

func TestSetMinutesNoteRejectsOpenRoom(t *testing.T) {
	h := newRoomServer(t, "proj-minutes-note-open")
	var opened openJSON
	doJSON(t, h, http.MethodPost, "/api/rooms", `{"topic":"t"}`, &opened)

	var errBody map[string]string
	rec := doJSON(t, h, http.MethodPost, "/api/rooms/"+opened.ID+"/minutes/note", `{"note_id":"N-0001"}`, &errBody)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body: %s", rec.Code, rec.Body.String())
	}
}

func TestSetMinutesNoteHappyPathAndDuplicate(t *testing.T) {
	h := newRoomServer(t, "proj-minutes-note")
	var opened openJSON
	doJSON(t, h, http.MethodPost, "/api/rooms", `{"topic":"t"}`, &opened)
	closeMinimalRoom(t, h, opened.ID, opened.FacilitatorID)

	var linked struct {
		MinutesNote string `json:"minutes_note"`
	}
	rec := doJSON(t, h, http.MethodPost, "/api/rooms/"+opened.ID+"/minutes/note", `{"note_id":"N-0001"}`, &linked)
	if rec.Code != http.StatusOK || linked.MinutesNote != "N-0001" {
		t.Fatalf("status=%d body=%+v, want 200 with minutes_note N-0001", rec.Code, linked)
	}

	var dupErr struct {
		Error       string `json:"error"`
		MinutesNote string `json:"minutes_note"`
	}
	rec = doJSON(t, h, http.MethodPost, "/api/rooms/"+opened.ID+"/minutes/note", `{"note_id":"N-0002"}`, &dupErr)
	if rec.Code != http.StatusConflict || dupErr.MinutesNote != "N-0001" {
		t.Fatalf("duplicate status=%d body=%+v, want 409 pointing at N-0001", rec.Code, dupErr)
	}

	var forced struct {
		MinutesNote string `json:"minutes_note"`
	}
	rec = doJSON(t, h, http.MethodPost, "/api/rooms/"+opened.ID+"/minutes/note", `{"note_id":"N-0002","force":true}`, &forced)
	if rec.Code != http.StatusOK || forced.MinutesNote != "N-0002" {
		t.Fatalf("forced status=%d body=%+v, want 200 with minutes_note N-0002", rec.Code, forced)
	}
}

func TestListedRoomIncludesMinutesNote(t *testing.T) {
	h := newRoomServer(t, "proj-minutes-note-list")
	var opened openJSON
	doJSON(t, h, http.MethodPost, "/api/rooms", `{"topic":"t"}`, &opened)
	closeMinimalRoom(t, h, opened.ID, opened.FacilitatorID)
	var linked struct{}
	doJSON(t, h, http.MethodPost, "/api/rooms/"+opened.ID+"/minutes/note", `{"note_id":"N-0009"}`, &linked)

	var got struct {
		Rooms []struct {
			ID          string `json:"id"`
			MinutesNote string `json:"minutes_note"`
		} `json:"rooms"`
	}
	rec := doJSON(t, h, http.MethodGet, "/api/rooms", "", &got)
	// Rooms[0] is the pinned permanent channel (F-0325).
	if rec.Code != http.StatusOK || len(got.Rooms) != 2 || got.Rooms[1].MinutesNote != "N-0009" {
		t.Fatalf("list status=%d body=%+v, want the room carrying minutes_note", rec.Code, got)
	}
}

// --- F-0325: the permanent channel over the room endpoints ---

// newChannelServer is a room server whose backend has two active works
// (W-0001, W-0002) and one done (W-0003); it returns the backend dir and the
// project cache dir that hold the channel.
func newChannelServer(t *testing.T, projectID string) (h http.Handler, repoDir, cacheDir string) {
	t.Helper()
	repoDir, projDir := newBackend(t)
	t.Setenv("CERNE_CACHE_DIR", t.TempDir())
	now := time.Now().UTC()
	for id, state := range map[string]domain.WorkState{"W-0001": domain.WorkActive, "W-0002": domain.WorkActive, "W-0003": domain.WorkDone} {
		if err := store.SaveWork(repoDir, store.Work{ID: id, Title: "atividade " + id, State: state, Actor: "agent", CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	cacheDir, err := project.CacheDir(projectID)
	if err != nil {
		t.Fatal(err)
	}
	return New(repoDir, projDir, projectID), repoDir, cacheDir
}

func seedServerChannel(t *testing.T, cacheDir, nick, body string, at time.Time) {
	t.Helper()
	if _, err := room.OpenChannel(filepath.Join(cacheDir, "rooms")).Append(room.Message{Type: "message", Nick: nick, Body: body, Time: at}); err != nil {
		t.Fatal(err)
	}
}

type channelReadJSON struct {
	Messages []struct {
		Seq      int    `json:"seq"`
		Nick     string `json:"nick"`
		Body     string `json:"body"`
		Human    bool   `json:"human"`
		State    string `json:"state"`
		Directed bool   `json:"directed"`
		Targets  []struct {
			Work  string `json:"work"`
			State string `json:"state"`
		} `json:"targets"`
	} `json:"messages"`
	Version int64 `json:"version"`
}

// TestRoomListPinsChannel (crit. 3/21): the room list always starts with the
// permanent channel — even before its first message — whose participants are
// the active works.
func TestRoomListPinsChannel(t *testing.T) {
	h, _, _ := newChannelServer(t, "proj-channel-list")
	doJSON(t, h, http.MethodPost, "/api/rooms", `{"topic":"t"}`, &openJSON{})
	var got struct {
		Rooms []struct {
			ID           string            `json:"id"`
			Permanent    bool              `json:"permanent"`
			Participants []participantJSON `json:"participants"`
		} `json:"rooms"`
	}
	rec := doJSON(t, h, http.MethodGet, "/api/rooms", "", &got)
	if rec.Code != http.StatusOK || len(got.Rooms) != 2 || got.Rooms[0].ID != "channel" || !got.Rooms[0].Permanent || got.Rooms[1].ID != "R1" || got.Rooms[1].Permanent {
		t.Fatalf("rooms = %+v", got.Rooms)
	}
	var nicks []string
	for _, p := range got.Rooms[0].Participants {
		nicks = append(nicks, p.Nick)
	}
	if !slices.Equal(nicks, []string{"W-0001", "W-0002"}) {
		t.Fatalf("channel participants = %v, want the active works", nicks)
	}
}

// TestChannelReadServesStates (crit. 13/20): the channel read carries each
// live message's derived state and targets, and drops what expired.
func TestChannelReadServesStates(t *testing.T) {
	h, _, cacheDir := newChannelServer(t, "proj-channel-read")
	now := time.Now().UTC()
	seedServerChannel(t, cacheDir, "W-0002", "velha", now.Add(-25*time.Hour))
	seedServerChannel(t, cacheDir, "W-0003", "de work fechado", now)
	seedServerChannel(t, cacheDir, "W-0002", "W-0001 segura", now)
	var got channelReadJSON
	rec := doJSON(t, h, http.MethodGet, "/api/rooms/channel/messages", "", &got)
	if rec.Code != http.StatusOK || len(got.Messages) != 1 || got.Version == 0 {
		t.Fatalf("status %d, read = %+v", rec.Code, got)
	}
	m := got.Messages[0]
	if m.Seq != 3 || m.State != "not_delivered" || !m.Directed || len(m.Targets) != 1 || m.Targets[0].Work != "W-0001" {
		t.Fatalf("message = %+v", m)
	}
}

// TestChannelReadSince: ?since=N keeps `cerne room read channel` working.
func TestChannelReadSince(t *testing.T) {
	h, _, cacheDir := newChannelServer(t, "proj-channel-since")
	for _, b := range []string{"um", "dois", "três"} {
		seedServerChannel(t, cacheDir, "W-0002", b, time.Now().UTC())
	}
	var got channelReadJSON
	doJSON(t, h, http.MethodGet, "/api/rooms/channel/messages?since=1", "", &got)
	if len(got.Messages) != 2 || got.Messages[0].Seq != 2 {
		t.Fatalf("since=1 read = %+v", got.Messages)
	}
}

// TestChannelReadFollowWakesOnDelivery (crit. 24, server): a follow read
// parked on a version wakes when a hook records a delivery — no new message
// needed — and returns the new state.
func TestChannelReadFollowWakesOnDelivery(t *testing.T) {
	h, repoDir, cacheDir := newChannelServer(t, "proj-channel-follow")
	seedServerChannel(t, cacheDir, "W-0002", "W-0001 segura", time.Now().UTC())
	if err := app.BindSession(cacheDir, "s1", "W-0001"); err != nil {
		t.Fatal(err)
	}
	var first channelReadJSON
	doJSON(t, h, http.MethodGet, "/api/rooms/channel/messages", "", &first)

	done := make(chan channelReadJSON, 1)
	go func() {
		req := httptest.NewRequest(http.MethodGet, "/api/rooms/channel/messages?follow=1&version="+strconv.FormatInt(first.Version, 10), nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		var got channelReadJSON
		_ = json.Unmarshal(rec.Body.Bytes(), &got)
		done <- got
	}()
	time.Sleep(100 * time.Millisecond)
	if _, err := app.ChannelInject(cacheDir, repoDir, "s1", true, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-done:
		if got.Version == first.Version || len(got.Messages) != 1 || got.Messages[0].State != "delivered" {
			t.Fatalf("woken read = %+v", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("follow read did not wake on the delivery")
	}
}

// TestChannelHumanPostUsesSetting (crit. 22): the UI composer posts to the same
// channel the agents read, as the human — named by channel.human_name (host
// scope), else the OS user, else "human". No session needed.
func TestChannelHumanPostUsesSetting(t *testing.T) {
	h, _, cacheDir := newChannelServer(t, "proj-channel-human")
	hostDir := t.TempDir()
	t.Setenv("CERNE_CONFIG_DIR", hostDir)
	post := func() messageJSON {
		t.Helper()
		var got struct {
			messageJSON
			Human bool `json:"human"`
		}
		rec := doJSON(t, h, http.MethodPost, "/api/rooms/channel/messages", `{"body":"segura o deploy"}`, &got)
		if rec.Code != http.StatusCreated || !got.Human || got.Body != "segura o deploy" {
			t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
		}
		return got.messageJSON
	}
	if err := os.WriteFile(filepath.Join(hostDir, "settings.toml"), []byte("[channel]\nhuman_name = \"Henrique\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if m := post(); m.Nick != "Henrique" {
		t.Fatalf("nick = %q, want the setting", m.Nick)
	}
	if err := os.Remove(filepath.Join(hostDir, "settings.toml")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("USER", "ana")
	t.Setenv("USERNAME", "")
	if m := post(); m.Nick != "ana" {
		t.Fatalf("nick = %q, want $USER", m.Nick)
	}
	t.Setenv("USER", "")
	if m := post(); m.Nick != "human" {
		t.Fatalf("nick = %q, want human", m.Nick)
	}
	msgs, err := room.OpenChannel(filepath.Join(cacheDir, "rooms")).Messages()
	if err != nil || len(msgs) != 3 || !msgs[0].Human {
		t.Fatalf("channel = %+v, %v", msgs, err)
	}
	var bad map[string]string
	if rec := doJSON(t, h, http.MethodPost, "/api/rooms/channel/messages", `{"body":"  "}`, &bad); rec.Code != http.StatusBadRequest {
		t.Fatalf("blank body: status %d", rec.Code)
	}
}
