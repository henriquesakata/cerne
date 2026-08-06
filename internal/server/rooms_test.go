package server

import (
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
}

func newRoomServer(t *testing.T, projectID string) http.Handler {
	t.Helper()
	repoDir, projDir := newBackend(t)
	t.Setenv("CERNE_CACHE_DIR", t.TempDir())
	return New(repoDir, projDir, projectID)
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

func TestRoomListEndpointIncludesRoster(t *testing.T) {
	h := newRoomServer(t, "proj-room-list")
	var opened roomJSON
	doJSON(t, h, http.MethodPost, "/api/rooms", `{"topic":"t"}`, &opened)
	doJSON(t, h, http.MethodPost, "/api/rooms/"+opened.ID+"/join", `{"nick":"alice","bio":"otimista"}`, &roomJSON{})

	var got struct {
		Rooms []roomJSON `json:"rooms"`
	}
	rec := doJSON(t, h, http.MethodGet, "/api/rooms", "", &got)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if len(got.Rooms) != 1 || len(got.Rooms[0].Participants) != 1 || got.Rooms[0].Participants[0].Nick != "alice" {
		t.Fatalf("rooms = %+v", got.Rooms)
	}
}

func TestRoomJoinDuplicateNickConflict(t *testing.T) {
	h := newRoomServer(t, "proj-room-join-dup")
	var opened roomJSON
	doJSON(t, h, http.MethodPost, "/api/rooms", `{"topic":"t"}`, &opened)
	var got roomJSON
	rec := doJSON(t, h, http.MethodPost, "/api/rooms/"+opened.ID+"/join", `{"nick":"alice"}`, &got)
	if rec.Code != http.StatusOK {
		t.Fatalf("first join status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	var errBody map[string]string
	rec = doJSON(t, h, http.MethodPost, "/api/rooms/"+opened.ID+"/join", `{"nick":"alice"}`, &errBody)
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

func TestRoomSayNotJoinedUnprocessable(t *testing.T) {
	h := newRoomServer(t, "proj-room-say-notjoined")
	var opened roomJSON
	doJSON(t, h, http.MethodPost, "/api/rooms", `{"topic":"t"}`, &opened)
	var errBody map[string]string
	rec := doJSON(t, h, http.MethodPost, "/api/rooms/"+opened.ID+"/messages", `{"nick":"alice","body":"oi"}`, &errBody)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body: %s", rec.Code, rec.Body.String())
	}
}

func TestRoomSayAndReadSince(t *testing.T) {
	h := newRoomServer(t, "proj-room-say")
	var opened roomJSON
	doJSON(t, h, http.MethodPost, "/api/rooms", `{"topic":"t"}`, &opened)
	doJSON(t, h, http.MethodPost, "/api/rooms/"+opened.ID+"/join", `{"nick":"alice"}`, &roomJSON{})

	var said messageJSON
	rec := doJSON(t, h, http.MethodPost, "/api/rooms/"+opened.ID+"/messages", `{"nick":"alice","body":"oi pessoal"}`, &said)
	if rec.Code != http.StatusCreated || said.Body != "oi pessoal" || said.Seq == 0 {
		t.Fatalf("say status=%d body=%+v", rec.Code, said)
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
	}
	if !found {
		t.Fatalf("messages = %+v, want the said message", got.Messages)
	}
}

func TestRoomCloseRejectsJoinAndSayButKeepsRead(t *testing.T) {
	h := newRoomServer(t, "proj-room-close")
	var opened roomJSON
	doJSON(t, h, http.MethodPost, "/api/rooms", `{"topic":"t"}`, &opened)
	doJSON(t, h, http.MethodPost, "/api/rooms/"+opened.ID+"/join", `{"nick":"alice"}`, &roomJSON{})
	doJSON(t, h, http.MethodPost, "/api/rooms/"+opened.ID+"/messages", `{"nick":"alice","body":"oi"}`, &messageJSON{})

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
	rec = doJSON(t, h, http.MethodPost, "/api/rooms/"+opened.ID+"/messages", `{"nick":"alice","body":"oi de novo"}`, &errBody)
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
	doJSON(t, h, http.MethodPost, "/api/rooms/"+opened.ID+"/join", `{"nick":"alice"}`, &roomJSON{})

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
	rec := doJSON(t, h, http.MethodPost, "/api/rooms/"+opened.ID+"/messages", `{"nick":"alice","body":"acordou"}`, &said)
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
