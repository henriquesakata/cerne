package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// presenceJSON mirrors an active participant's live presence (T-0021).
type presenceJSON struct {
	Nick       string     `json:"nick"`
	Listening  bool       `json:"listening"`
	LastSeenAt *time.Time `json:"last_seen_at"`
}

// presenceOf reads GET /api/rooms and returns nick's active entry in id,
// plus the raw body for leak checks.
func presenceOf(t *testing.T, h http.Handler, id, nick string) (presenceJSON, string) {
	t.Helper()
	var list struct {
		Rooms []struct {
			ID                 string         `json:"id"`
			ActiveParticipants []presenceJSON `json:"active_participants"`
		} `json:"rooms"`
	}
	rec := doJSON(t, h, http.MethodGet, "/api/rooms", "", &list)
	for _, r := range list.Rooms {
		if r.ID != id {
			continue
		}
		for _, p := range r.ActiveParticipants {
			if p.Nick == nick {
				return p, rec.Body.String()
			}
		}
	}
	t.Fatalf("%s not active in %s: %s", nick, id, rec.Body.String())
	return presenceJSON{}, ""
}

// followAs holds GET .../messages?follow=1 as sessionID until cancel. since
// is past the transcript, so only cancel ends it.
func followAs(t *testing.T, h http.Handler, id, sessionID string) (cancel func()) {
	t.Helper()
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		req := localRequest(http.MethodGet, "/api/rooms/"+id+"/messages?since=1000000&follow=1", nil).WithContext(ctx)
		req.Header.Set("X-Room-Session", sessionID)
		h.ServeHTTP(httptest.NewRecorder(), req)
	}()
	return func() {
		stop()
		<-done
	}
}

// T-0021 crit. 1
// E-0006 crit. 7
func TestRoomListServesLiveListening(t *testing.T) {
	h := newRoomServer(t, "proj-room-presence")
	var opened openJSON
	doJSON(t, h, http.MethodPost, "/api/rooms", `{"topic":"t"}`, &opened)
	alice := joinAs(t, h, opened.ID, "alice")
	joinAs(t, h, opened.ID, "bob")

	cancel := followAs(t, h, opened.ID, alice.SessionID)
	deadline := time.Now().Add(2 * time.Second)
	for {
		p, body := presenceOf(t, h, opened.ID, "alice")
		if p.Listening {
			if strings.Contains(body, alice.SessionID) || strings.Contains(body, opened.FacilitatorID) ||
				strings.Contains(body, "session") || strings.Contains(body, "hash") {
				t.Fatalf("GET /api/rooms leaks a capability: %s", body)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("alice never listening during her follow read: %s", body)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if bob, _ := presenceOf(t, h, opened.ID, "bob"); bob.Listening {
		t.Fatal("bob listening without a follow read")
	}

	cancel()
	p, _ := presenceOf(t, h, opened.ID, "alice")
	if p.Listening || p.LastSeenAt == nil {
		t.Fatalf("after the request ended: %+v, want not listening with last_seen_at", p)
	}
}

// T-0021 crit. 1
// E-0006 crit. 7
func TestRoomListClosedRoomOmitsListening(t *testing.T) {
	h := newRoomServer(t, "proj-room-presence-closed")
	var opened openJSON
	doJSON(t, h, http.MethodPost, "/api/rooms", `{"topic":"t"}`, &opened)
	alice := joinAs(t, h, opened.ID, "alice")
	closeMinimalRoom(t, h, opened.ID, opened.FacilitatorID)

	cancel := followAs(t, h, opened.ID, alice.SessionID)
	defer cancel()
	time.Sleep(50 * time.Millisecond) // give the read time to register
	if _, body := presenceOf(t, h, opened.ID, "alice"); strings.Contains(body, `"listening"`) {
		t.Fatalf("closed room serves listening: %s", body)
	}
}
