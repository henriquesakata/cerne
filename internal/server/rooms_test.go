package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// roomJSON/messageJSON mirror the HTTP contract of the war room endpoints
// (T-0001) — a black-box view, independent of internal/room's Go types.
type roomJSON struct {
	ID           string            `json:"id"`
	Topic        string            `json:"topic"`
	ClosedAt     *time.Time        `json:"closed_at"`
	Participants []participantJSON `json:"participants"`
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
	var got roomJSON
	rec := doJSON(t, h, http.MethodPost, "/api/rooms", `{"topic":"deploy quebrado"}`, &got)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", rec.Code, rec.Body.String())
	}
	if got.ID != "R1" || got.Topic != "deploy quebrado" || len(got.Participants) != 0 {
		t.Fatalf("room = %+v", got)
	}
}

func TestRoomJoinReturnsRoomAndSession(t *testing.T) {
	h := newRoomServer(t, "proj-room-join-session")
	var opened roomJSON
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
	var opened roomJSON
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
	var opened roomJSON
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
	var opened roomJSON
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
	var opened roomJSON
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
	var opened roomJSON
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
	var opened roomJSON
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
	var opened roomJSON
	doJSON(t, h, http.MethodPost, "/api/rooms", `{"topic":"t"}`, &opened)
	sess := joinAs(t, h, opened.ID, "alice")
	doJSON(t, h, http.MethodPost, "/api/rooms/"+opened.ID+"/messages", `{"session_id":"`+sess.SessionID+`","body":"oi"}`, &messageJSON{})

	var closed roomJSON
	rec := doJSON(t, h, http.MethodPost, "/api/rooms/"+opened.ID+"/close", "", &closed)
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
func TestRoomReadFollowWakesOnNewMessage(t *testing.T) {
	h := newRoomServer(t, "proj-room-follow")
	var opened roomJSON
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
