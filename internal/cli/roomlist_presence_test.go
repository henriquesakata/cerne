package cli

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/room"
)

// TestRoomListCountsActiveAndListening: an open formal room counts its active
// participants and how many listen; a closed room and the channel keep the
// historical count with no listening suffix.
// T-0021 crit. 2
// E-0006 crit. 8
func TestRoomListCountsActiveAndListening(t *testing.T) {
	dir, port := newRoomTestServer(t, "proj-list-listening")
	c, err := newRoomClient(dir, port)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	closed, err := c.open(ctx, "fechada")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := c.join(ctx, closed.Room.ID, "ana", ""); err != nil {
		t.Fatalf("join: %v", err)
	}
	if _, err := c.close(ctx, closed.Room.ID, closed.FacilitatorID, room.Minutes{Conclusion: "ok", Rationale: "ok"}, false); err != nil {
		t.Fatalf("close: %v", err)
	}

	opened, _ := c.open(ctx, "aberta")
	var sessions []string
	for _, nick := range []string{"ana", "bob", "cid"} {
		res, err := c.join(ctx, opened.Room.ID, nick, "")
		if err != nil {
			t.Fatalf("join %s: %v", nick, err)
		}
		sessions = append(sessions, res.Session.SessionID)
	}
	if _, err := c.leave(ctx, opened.Room.ID, sessions[2]); err != nil { // history 3, active 2
		t.Fatalf("leave: %v", err)
	}

	fctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = c.readAsSession(fctx, opened.Room.ID, 1<<30, sessions[0])
	}()
	t.Cleanup(func() { cancel(); <-done })
	for deadline := time.Now().Add(2 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		rooms, _ := c.list(ctx)
		listening := false
		for _, r := range rooms {
			for _, p := range r.ActiveParticipants {
				listening = listening || (r.ID == opened.Room.ID && p.Listening)
			}
		}
		if listening {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("ana never listening")
		}
	}

	out, err := runRoom(t, "list", "--dir", dir, "--port", strconv.Itoa(port))
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, want := range []string{
		opened.Room.ID + "  open   2 participant(s) (1 listening)  aberta\n",
		closed.Room.ID + "  closed 1 participant(s)  fechada\n",
		"channel  permanent 0 participant(s)  \n",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("list output = %q, want line %q", out, want)
		}
	}
}
