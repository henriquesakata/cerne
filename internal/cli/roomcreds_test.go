package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// B-0084: the credential join/open keep for an agent session lives in a 0600
// file under a 0700 dir, named by the hash of CERNE_SESSION, one per room.
func TestRoomCredsRoundTripAndPermissions(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("CERNE_CACHE_DIR", cache)
	t.Setenv("CERNE_SESSION", "agent-1")

	want := roomCreds{Nick: "ana", SessionID: "s1"}
	if err := saveRoomCreds("p", "R1", want); err != nil {
		t.Fatalf("saveRoomCreds: %v", err)
	}
	path := filepath.Join(cache, "p", "room-creds", "R1", hashSessionID("agent-1")+".json")
	if got, err := roomCredsPath("p", "R1"); err != nil || got != path {
		t.Fatalf("roomCredsPath = %q, %v; want %q", got, err, path)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat file: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("file perm = %o, want 0600", perm)
	}
	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if perm := dirInfo.Mode().Perm(); perm != 0o700 {
		t.Fatalf("dir perm = %o, want 0700", perm)
	}

	got, err := loadRoomCreds("p", "R1")
	if err != nil || got != want {
		t.Fatalf("loadRoomCreds = %+v, %v; want %+v", got, err, want)
	}
	// another room shares nothing.
	if other, err := loadRoomCreds("p", "R2"); err != nil || other != (roomCreds{}) {
		t.Fatalf("loadRoomCreds(R2) = %+v, %v; want empty", other, err)
	}
	// another agent session shares nothing.
	t.Setenv("CERNE_SESSION", "agent-2")
	if other, err := loadRoomCreds("p", "R1"); err != nil || other != (roomCreds{}) {
		t.Fatalf("loadRoomCreds as agent-2 = %+v, %v; want empty", other, err)
	}
}

// B-0084: a human terminal has no CERNE_SESSION, so nothing is stored or read.
func TestRoomCredsNeedAgentSession(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("CERNE_CACHE_DIR", cache)
	t.Setenv("CERNE_SESSION", "agent-1")
	if err := saveRoomCreds("p", "R1", roomCreds{SessionID: "s1"}); err != nil {
		t.Fatalf("saveRoomCreds: %v", err)
	}

	t.Setenv("CERNE_SESSION", "")
	if got, err := loadRoomCreds("p", "R1"); err != nil || got != (roomCreds{}) {
		t.Fatalf("loadRoomCreds without CERNE_SESSION = %+v, %v; want empty", got, err)
	}
	if err := saveRoomCreds("p", "R2", roomCreds{SessionID: "s2"}); err != nil {
		t.Fatalf("saveRoomCreds without CERNE_SESSION: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cache, "p", "room-creds", "R2")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stat R2 creds dir = %v, want not exist", err)
	}
	if _, err := resolveRoomSession("", "p", "R1"); !errors.Is(err, errNoRoomSession) {
		t.Fatalf("resolveRoomSession without CERNE_SESSION err = %v, want errNoRoomSession", err)
	}
}

// B-0084: --session > CERNE_ROOM_SESSION > stored.
func TestRoomCredsResolvePrecedence(t *testing.T) {
	t.Setenv("CERNE_CACHE_DIR", t.TempDir())
	t.Setenv("CERNE_SESSION", "agent-1")
	t.Setenv("CERNE_ROOM_SESSION", "")

	if _, err := resolveRoomSession("", "p", "R1"); !errors.Is(err, errNoRoomSession) {
		t.Fatalf("nothing anywhere: err = %v, want errNoRoomSession", err)
	}
	if err := saveRoomCreds("p", "R1", roomCreds{Nick: "ana", SessionID: "stored-s"}); err != nil {
		t.Fatalf("saveRoomCreds: %v", err)
	}
	if got, err := resolveRoomSession("", "p", "R1"); err != nil || got != "stored-s" {
		t.Fatalf("stored only = %q, %v", got, err)
	}
	t.Setenv("CERNE_ROOM_SESSION", "env-s")
	if got, err := resolveRoomSession("", "p", "R1"); err != nil || got != "env-s" {
		t.Fatalf("env over stored = %q, %v", got, err)
	}
	if got, err := resolveRoomSession("flag-s", "p", "R1"); err != nil || got != "flag-s" {
		t.Fatalf("flag over env = %q, %v", got, err)
	}
}

// B-0084, review round 1: the facilitator capability is --facilitator >
// CERNE_ROOM_FACILITATOR only; a facilitator_id left in the stored file (by
// an earlier build) is never read.
func TestRoomCredsFacilitatorIsNeverStored(t *testing.T) {
	t.Setenv("CERNE_CACHE_DIR", t.TempDir())
	t.Setenv("CERNE_SESSION", "agent-1")
	t.Setenv("CERNE_ROOM_FACILITATOR", "")
	path, err := roomCredsPath("p", "R1")
	if err != nil {
		t.Fatal(err)
	}
	if err := writePrivateFile(path, []byte(`{"facilitator_id":"stored-f"}`)); err != nil {
		t.Fatal(err)
	}
	_, err = resolveRoomFacilitator("", "R1")
	if !errors.Is(err, errNoRoomFacilitator) || !strings.Contains(err.Error(), "cerne room facilitator reset R1") {
		t.Fatalf("only a stored one: err = %v, want errNoRoomFacilitator naming the reset", err)
	}
	t.Setenv("CERNE_ROOM_FACILITATOR", "env-f")
	if got, err := resolveRoomFacilitator("", "R1"); err != nil || got != "env-f" {
		t.Fatalf("env = %q, %v", got, err)
	}
	if got, err := resolveRoomFacilitator("flag-f", "R1"); err != nil || got != "flag-f" {
		t.Fatalf("flag over env = %q, %v", got, err)
	}
}

// B-0084: with a stored session, say/propose/request-decision/watch run
// with no flag and no variable, as the stored nick.
func TestRoomCredsCommandsUseStoredCredential(t *testing.T) {
	dir, port := newRoomTestServer(t, "proj-creds")
	dirFlag := []string{"--dir", dir, "--port", strconv.Itoa(port)}
	t.Setenv("CERNE_SESSION", "agent-1")
	t.Setenv("CERNE_ROOM_SESSION", "")
	t.Setenv("CERNE_ROOM_FACILITATOR", "")
	c, err := newRoomClient(dir, port)
	if err != nil {
		t.Fatalf("newRoomClient: %v", err)
	}
	opened, err := c.open(context.Background(), "topic")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	joined, err := c.join(context.Background(), "R1", "ana", "")
	if err != nil {
		t.Fatalf("join: %v", err)
	}
	if err := saveRoomCreds(c.pid, "R1", roomCreds{Nick: "ana", SessionID: joined.Session.SessionID}); err != nil {
		t.Fatalf("saveRoomCreds: %v", err)
	}

	for _, args := range [][]string{
		{"say", "R1", "via stored"},
		{"propose", "R1", "--title", "usar postgres"},
		{"request-decision", "R1", "--title", "qual banco?"},
	} {
		if out, err := runRoom(t, append(args, dirFlag...)...); err != nil {
			t.Fatalf("%s with the stored session: %v (%s)", args[0], err, out)
		}
	}
	out, err := runRoom(t, append([]string{"read", "R1"}, dirFlag...)...)
	if err != nil || !strings.Contains(out, "[ana] via stored") {
		t.Fatalf("read = %q, %v; want the message said as ana", out, err)
	}
	t.Setenv("CERNE_ROOM_FACILITATOR", opened.FacilitatorID)
	out, err = runRoom(t, append([]string{"pending", "R1"}, dirFlag...)...)
	if err != nil || !strings.Contains(out, "usar postgres") || !strings.Contains(out, "by ana") {
		t.Fatalf("pending = %q, %v; want ana's items", out, err)
	}
	out, err = runRoom(t, append([]string{"watch", "R1", "--once", "--since", "0"}, dirFlag...)...)
	if err != nil || !strings.Contains(out, "#1 [open]") || strings.Contains(out, "via stored") {
		t.Fatalf("watch with the stored session = %q, %v; want the open event and no own echo", out, err)
	}
	cp, err := checkpointPath(c.pid, "R1", joined.Session.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cp); err != nil {
		t.Fatalf("watch cursor for the stored session: %v", err)
	}
	if _, err := runRoom(t, append([]string{"close", "R1", "--minutes", minimalMinutesJSON, "--allow-open-items"}, dirFlag...)...); err != nil {
		t.Fatalf("close: %v", err)
	}
}

// B-0084: with no credential anywhere, the error is English and says join
// stores one when CERNE_SESSION is set.
func TestRoomCredsMissingErrorNamesJoin(t *testing.T) {
	dir, port := newRoomTestServer(t, "proj-creds-missing")
	dirFlag := []string{"--dir", dir, "--port", strconv.Itoa(port)}
	t.Setenv("CERNE_SESSION", "agent-1")
	t.Setenv("CERNE_ROOM_SESSION", "")
	for _, sub := range []string{"say", "watch"} {
		args := []string{sub, "R1"}
		if sub == "say" {
			args = append(args, "oi")
		}
		_, err := runRoom(t, append(args, dirFlag...)...)
		if !errors.Is(err, errNoRoomSession) {
			t.Fatalf("%s err = %v, want errNoRoomSession", sub, err)
		}
		if !strings.Contains(err.Error(), "CERNE_SESSION is set") || !strings.Contains(err.Error(), "room join") {
			t.Fatalf("%s err = %q, want it to say join stores it under CERNE_SESSION", sub, err)
		}
	}
}
