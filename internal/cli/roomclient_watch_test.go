package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestRoomClientReadAsSessionDecodesOwn(t *testing.T) {
	dir, port := newRoomTestServer(t, "proj-own")
	dirFlag := []string{"--dir", dir, "--port", strconv.Itoa(port)}
	runRoom(t, append([]string{"open", "topic"}, dirFlag...)...)
	sessA := joinRoomAs(t, dirFlag, "R1", "ana")
	joinRoomAs(t, dirFlag, "R1", "bob")

	c, err := newRoomClient(dir, port)
	if err != nil {
		t.Fatalf("newRoomClient: %v", err)
	}
	if _, err := c.say(context.Background(), "R1", sessA, "oi de ana"); err != nil {
		t.Fatalf("say: %v", err)
	}

	events, err := c.readAsSession(context.Background(), "R1", 0, sessA)
	if err != nil {
		t.Fatalf("readAsSession: %v", err)
	}
	var sawOwnTrue, sawOwnFalse bool
	for _, ev := range events {
		if ev.Type != "message" {
			continue
		}
		if ev.Own == nil {
			t.Fatalf("event %+v: want Own set for a session-authenticated read", ev)
		}
		if ev.Nick == "ana" {
			if !*ev.Own {
				t.Fatalf("ana's own message: Own = %v, want true", *ev.Own)
			}
			sawOwnTrue = true
		}
	}
	_ = sawOwnFalse
	if !sawOwnTrue {
		t.Fatal("never saw ana's message with Own=true")
	}
}

func TestRoomClientResumeReturnsLastSeq(t *testing.T) {
	dir, port := newRoomTestServer(t, "proj-resume")
	dirFlag := []string{"--dir", dir, "--port", strconv.Itoa(port)}
	runRoom(t, append([]string{"open", "topic"}, dirFlag...)...)
	sess := joinRoomAs(t, dirFlag, "R1", "ana")

	c, err := newRoomClient(dir, port)
	if err != nil {
		t.Fatalf("newRoomClient: %v", err)
	}
	if _, err := c.say(context.Background(), "R1", sess, "oi"); err != nil {
		t.Fatalf("say: %v", err)
	}
	got, err := c.resume(context.Background(), "R1", sess)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if got.LastSeq < 2 {
		t.Fatalf("resume LastSeq = %d, want >= 2 (open+join+say already happened)", got.LastSeq)
	}
	if got.Nick != "ana" {
		t.Fatalf("resume Nick = %q, want ana", got.Nick)
	}
}

func TestRoomClientResumeInvalidSessionIsNotTransient(t *testing.T) {
	dir, port := newRoomTestServer(t, "proj-resume-bad")
	dirFlag := []string{"--dir", dir, "--port", strconv.Itoa(port)}
	runRoom(t, append([]string{"open", "topic"}, dirFlag...)...)

	c, err := newRoomClient(dir, port)
	if err != nil {
		t.Fatalf("newRoomClient: %v", err)
	}
	_, err = c.resume(context.Background(), "R1", "not-a-real-session")
	if err == nil {
		t.Fatal("resume with bogus session: want error, got nil")
	}
	if errors.Is(err, errRoomTransient) {
		t.Fatalf("resume with bogus session: err = %v, must NOT be classified transient", err)
	}
}

func TestRoomClientNoServerIsTransient(t *testing.T) {
	dir := t.TempDir()
	cernerc := "project_id = \"proj-none-transient\"\nbackend = \"/dev/null\"\n"
	if err := os.WriteFile(filepath.Join(dir, ".cernerc"), []byte(cernerc), 0o644); err != nil {
		t.Fatalf("write .cernerc: %v", err)
	}
	c, err := newRoomClient(dir, 1) // nothing listens on port 1
	if err != nil {
		t.Fatalf("newRoomClient: %v", err)
	}
	_, err = c.resume(context.Background(), "R1", "whatever")
	if err == nil {
		t.Fatal("resume against unreachable server: want error, got nil")
	}
	if !errors.Is(err, errRoomTransient) {
		t.Fatalf("resume against unreachable server: err = %v, want errRoomTransient", err)
	}
}
