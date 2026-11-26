package room

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// activeOf returns nick's entry in id's active roster, failing when absent.
func activeOf(t *testing.T, m *Manager, id, nick string) Participant {
	t.Helper()
	for _, r := range m.List() {
		if r.ID != id {
			continue
		}
		for _, p := range r.ActiveParticipants {
			if p.Nick == nick {
				return p
			}
		}
	}
	t.Fatalf("%s is not an active participant of %s", nick, id)
	return Participant{}
}

// waitListening polls until nick's listening flag equals want: the follow
// read runs in a goroutine, so its registration is not instantaneous.
func waitListening(t *testing.T, m *Manager, id, nick string, want bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for activeOf(t, m, id, nick).Listening != want {
		if time.Now().After(deadline) {
			t.Fatalf("%s listening never became %v", nick, want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// follow starts a follow read for sessionID that only ends when the returned
// cancel is called: since is far past the log, so no event wakes it.
func follow(t *testing.T, m *Manager, id, sessionID string) (cancel func()) {
	t.Helper()
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, _, err := m.ReadForSession(ctx, id, sessionID, 1<<30, true, time.Minute); err != nil {
			t.Errorf("ReadForSession: %v", err)
		}
	}()
	return func() {
		stop()
		<-done
	}
}

// T-0021 crit. 1
// E-0006 crit. 7
func TestPresenceListeningWhileFollowReadInFlight(t *testing.T) {
	m := newManager(t)
	r, _, _ := m.Open("topico")
	alice := mustJoin(t, m, r.ID, "alice", "")
	mustJoin(t, m, r.ID, "bob", "")

	cancel := follow(t, m, r.ID, alice.SessionID)
	waitListening(t, m, r.ID, "alice", true)
	if activeOf(t, m, r.ID, "bob").Listening {
		t.Fatal("bob listening without a follow read")
	}
	cancel()
	p := activeOf(t, m, r.ID, "alice")
	if p.Listening {
		t.Fatal("alice still listening after her follow read was canceled")
	}
	if p.LastSeenAt == nil {
		t.Fatal("last_seen_at lost when the follow read ended")
	}
}

// TestPresenceLastSeenStampedWhenFollowReadEnds: a long read that just ended
// is the participant's latest activity, not the moment it began.
// T-0021 crit. 1
// E-0006 crit. 7
func TestPresenceLastSeenStampedWhenFollowReadEnds(t *testing.T) {
	m := newManager(t)
	r, _, _ := m.Open("topico")
	alice := mustJoin(t, m, r.ID, "alice", "")
	cancel := follow(t, m, r.ID, alice.SessionID)
	waitListening(t, m, r.ID, "alice", true)
	time.Sleep(2 * time.Millisecond) // the read's start stamp must be strictly earlier
	before := time.Now().UTC()
	cancel()
	if got := activeOf(t, m, r.ID, "alice").LastSeenAt; got == nil || got.Before(before) {
		t.Fatalf("last_seen_at = %v, want the end of the read (>= %v)", got, before)
	}
}

// T-0021 crit. 1
// E-0006 crit. 7
func TestPresenceTwoFollowReadsOfOneSession(t *testing.T) {
	m := newManager(t)
	r, _, _ := m.Open("topico")
	alice := mustJoin(t, m, r.ID, "alice", "")

	first := follow(t, m, r.ID, alice.SessionID)
	second := follow(t, m, r.ID, alice.SessionID)
	waitListening(t, m, r.ID, "alice", true)
	first()
	if !activeOf(t, m, r.ID, "alice").Listening {
		t.Fatal("alice stopped listening while her second follow read is in flight")
	}
	second()
	if activeOf(t, m, r.ID, "alice").Listening {
		t.Fatal("alice still listening after both follow reads ended")
	}
}

// T-0021 crit. 1
// E-0006 crit. 7
func TestPresenceLastSeenUpdatedByAuthenticatedInteractions(t *testing.T) {
	m := newManager(t)
	r, _, _ := m.Open("topico")
	before := time.Now().UTC()
	alice := mustJoin(t, m, r.ID, "alice", "")
	steps := []struct {
		name string
		do   func() error
	}{
		{"Join", func() error { return nil }},
		{"Resume", func() error { _, err := m.Resume(r.ID, alice.SessionID); return err }},
		{"Say", func() error { _, err := m.Say(r.ID, alice.SessionID, "oi"); return err }},
		{"Propose", func() error { _, err := m.Propose(r.ID, alice.SessionID, "t", "b"); return err }},
		{"RequestDecision", func() error { _, err := m.RequestDecision(r.ID, alice.SessionID, "t", "b"); return err }},
		{"Read", func() error {
			_, _, err := m.ReadForSession(context.Background(), r.ID, alice.SessionID, 0, false, time.Second)
			return err
		}},
	}
	for _, s := range steps {
		if s.name != "Join" {
			time.Sleep(2 * time.Millisecond) // the previous step's stamp must be strictly earlier
			before = time.Now().UTC()
		}
		if err := s.do(); err != nil {
			t.Fatalf("%s: %v", s.name, err)
		}
		got := activeOf(t, m, r.ID, "alice").LastSeenAt
		if got == nil || got.Before(before) {
			t.Fatalf("after %s last_seen_at = %v, want >= %v", s.name, got, before)
		}
	}
}

// T-0021 crit. 1
// E-0006 crit. 7
func TestPresenceClosedRoomNeverListening(t *testing.T) {
	m := newManager(t)
	r, fac, _ := m.Open("topico")
	alice := mustJoin(t, m, r.ID, "alice", "")
	if _, err := m.Close(r.ID, fac, minimalMinutes, false); err != nil {
		t.Fatalf("Close: %v", err)
	}
	cancel := follow(t, m, r.ID, alice.SessionID)
	defer cancel()
	waitWatching(t, m, r.ID, "alice", 1)
	if activeOf(t, m, r.ID, "alice").Listening {
		t.Fatal("listening in a closed room")
	}
	b, _ := json.Marshal(m.List())
	if strings.Contains(string(b), `"listening"`) {
		t.Fatalf("closed room serves listening: %s", b)
	}
}

// waitWatching polls until nick has n follow reads counted: in a closed room
// listening stays false, so the count itself is the state to wait for.
func waitWatching(t *testing.T, m *Manager, id, nick string, n int) {
	t.Helper()
	rs, err := m.getRoom(id)
	if err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(2 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		rs.mu.Lock()
		got := rs.watchers[nick]
		rs.mu.Unlock()
		if got == n {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s has %d follow reads counted, want %d", nick, got, n)
		}
	}
}

// T-0021 crit. 1
// E-0006 crit. 7
func TestPresenceCloseWhileFollowReadInFlight(t *testing.T) {
	m := newManager(t)
	r, fac, _ := m.Open("topico")
	alice := mustJoin(t, m, r.ID, "alice", "")
	cancel := follow(t, m, r.ID, alice.SessionID)
	defer cancel()
	waitListening(t, m, r.ID, "alice", true)

	if _, err := m.Close(r.ID, fac, minimalMinutes, false); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if activeOf(t, m, r.ID, "alice").Listening {
		t.Fatal("still listening after the room closed under her follow read")
	}
	b, _ := json.Marshal(m.List())
	if strings.Contains(string(b), `"listening"`) {
		t.Fatalf("closed room serves listening: %s", b)
	}
}

// T-0021 crit. 1
// E-0006 crit. 7
func TestPresenceLeaveClearsAndRejoinStartsFresh(t *testing.T) {
	m := newManager(t)
	r, _, _ := m.Open("topico")
	old := mustJoin(t, m, r.ID, "alice", "")
	stale := follow(t, m, r.ID, old.SessionID)
	waitListening(t, m, r.ID, "alice", true)
	if _, err := m.Leave(r.ID, old.SessionID); err != nil {
		t.Fatalf("Leave: %v", err)
	}

	time.Sleep(2 * time.Millisecond)
	before := time.Now().UTC()
	fresh := mustJoin(t, m, r.ID, "alice", "")
	p := activeOf(t, m, r.ID, "alice")
	if p.Listening {
		t.Fatal("re-joined alice listening through the read of her revoked session")
	}
	if p.LastSeenAt == nil || p.LastSeenAt.Before(before) {
		t.Fatalf("re-joined last_seen_at = %v, want the re-join time", p.LastSeenAt)
	}

	cancel := follow(t, m, r.ID, fresh.SessionID)
	defer cancel()
	waitListening(t, m, r.ID, "alice", true)
	stale()
	if !activeOf(t, m, r.ID, "alice").Listening {
		t.Fatal("the revoked session's read, ending, cleared the new session's listening")
	}
}

// T-0021 crit. 1
// E-0006 crit. 7
func TestPresenceNeverServesCapabilities(t *testing.T) {
	m := newManager(t)
	r, fac, _ := m.Open("topico")
	alice := mustJoin(t, m, r.ID, "alice", "")
	cancel := follow(t, m, r.ID, alice.SessionID)
	defer cancel()
	waitListening(t, m, r.ID, "alice", true)
	b, _ := json.Marshal(m.List())
	for _, secret := range []string{alice.SessionID, fac, "session", "hash"} {
		if strings.Contains(string(b), secret) {
			t.Fatalf("room list serves %q: %s", secret, b)
		}
	}
}
