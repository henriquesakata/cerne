package cli

import (
	"context"
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// B-0084: the sessions join keeps for an agent session live in a 0600 file
// under a 0700 dir, named by the hash of CERNE_SESSION, one per room, one
// entry per nick; a re-join of the same nick replaces its entry.
func TestRoomCredsRoundTripAndPermissions(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("CERNE_CACHE_DIR", cache)
	t.Setenv("CERNE_SESSION", "agent-1")

	if nicks, err := storeRoomSession("p", "R1", "ana", "s0"); err != nil || !slices.Equal(nicks, []string{"ana"}) {
		t.Fatalf("store ana = %v, %v; want [ana]", nicks, err)
	}
	if nicks, err := storeRoomSession("p", "R1", "bia", "s2"); err != nil || !slices.Equal(nicks, []string{"ana", "bia"}) {
		t.Fatalf("store bia = %v, %v; want [ana bia]", nicks, err)
	}
	if _, err := storeRoomSession("p", "R1", "ana", "s1"); err != nil {
		t.Fatalf("re-store ana: %v", err)
	}
	want := roomCreds{Sessions: map[string]string{"ana": "s1", "bia": "s2"}}
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
	if err != nil || !maps.Equal(got.Sessions, want.Sessions) {
		t.Fatalf("loadRoomCreds = %+v, %v; want %+v", got, err, want)
	}
	// another room shares nothing.
	if other, err := loadRoomCreds("p", "R2"); err != nil || len(other.Sessions) != 0 {
		t.Fatalf("loadRoomCreds(R2) = %+v, %v; want empty", other, err)
	}
	// another agent session shares nothing.
	t.Setenv("CERNE_SESSION", "agent-2")
	if other, err := loadRoomCreds("p", "R1"); err != nil || len(other.Sessions) != 0 {
		t.Fatalf("loadRoomCreds as agent-2 = %+v, %v; want empty", other, err)
	}
}

// B-0084: a human terminal has no CERNE_SESSION, so nothing is stored or read.
func TestRoomCredsNeedAgentSession(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("CERNE_CACHE_DIR", cache)
	t.Setenv("CERNE_SESSION", "agent-1")
	if _, err := storeRoomSession("p", "R1", "ana", "s1"); err != nil {
		t.Fatalf("storeRoomSession: %v", err)
	}

	t.Setenv("CERNE_SESSION", "")
	if got, err := loadRoomCreds("p", "R1"); err != nil || len(got.Sessions) != 0 {
		t.Fatalf("loadRoomCreds without CERNE_SESSION = %+v, %v; want empty", got, err)
	}
	if nicks, err := storeRoomSession("p", "R2", "ana", "s2"); err != nil || nicks != nil {
		t.Fatalf("storeRoomSession without CERNE_SESSION = %v, %v; want nil, nil", nicks, err)
	}
	if _, err := os.Stat(filepath.Join(cache, "p", "room-creds", "R2")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stat R2 creds dir = %v, want not exist", err)
	}
	if _, err := resolveRoomSession("", "", "p", "R1"); !errors.Is(err, errNoRoomSession) {
		t.Fatalf("resolveRoomSession without CERNE_SESSION err = %v, want errNoRoomSession", err)
	}
	if _, err := resolveRoomSession("", "ana", "p", "R1"); !errors.Is(err, errRoomNickNotStored) {
		t.Fatalf("resolveRoomSession --nick ana without CERNE_SESSION err = %v, want errRoomNickNotStored", err)
	}
}

// B-0084: --session > CERNE_ROOM_SESSION > stored; among stored nicks,
// --nick picks one, a single one needs no --nick, and several without --nick
// are refused rather than guessed (review round 1).
func TestRoomCredsResolvePrecedence(t *testing.T) {
	t.Setenv("CERNE_CACHE_DIR", t.TempDir())
	t.Setenv("CERNE_SESSION", "agent-1")
	t.Setenv("CERNE_ROOM_SESSION", "")

	if _, err := resolveRoomSession("", "", "p", "R1"); !errors.Is(err, errNoRoomSession) {
		t.Fatalf("nothing anywhere: err = %v, want errNoRoomSession", err)
	}
	if _, err := storeRoomSession("p", "R1", "ana", "stored-ana"); err != nil {
		t.Fatalf("storeRoomSession: %v", err)
	}
	if got, err := resolveRoomSession("", "", "p", "R1"); err != nil || got != "stored-ana" {
		t.Fatalf("one stored nick = %q, %v", got, err)
	}
	if _, err := resolveRoomSession("", "bia", "p", "R1"); !errors.Is(err, errRoomNickNotStored) {
		t.Fatalf("--nick bia not stored: err = %v, want errRoomNickNotStored", err)
	}
	if _, err := storeRoomSession("p", "R1", "bia", "stored-bia"); err != nil {
		t.Fatalf("storeRoomSession: %v", err)
	}
	_, err := resolveRoomSession("", "", "p", "R1")
	if !errors.Is(err, errRoomNickAmbiguous) || !strings.Contains(err.Error(), "ana, bia") {
		t.Fatalf("two stored nicks, no --nick: err = %v, want errRoomNickAmbiguous listing ana, bia", err)
	}
	for nick, want := range map[string]string{"ana": "stored-ana", "bia": "stored-bia"} {
		if got, err := resolveRoomSession("", nick, "p", "R1"); err != nil || got != want {
			t.Fatalf("--nick %s = %q, %v; want %q", nick, got, err, want)
		}
	}
	// review round 2: an explicit --nick beats the environment, so a
	// CERNE_ROOM_SESSION inherited from a human's shell never speaks for a
	// stored nick; the variable still beats a bare stored lookup.
	t.Setenv("CERNE_ROOM_SESSION", "env-s")
	if got, err := resolveRoomSession("", "bia", "p", "R1"); err != nil || got != "stored-bia" {
		t.Fatalf("--nick bia with the variable set = %q, %v; want stored-bia", got, err)
	}
	if _, err := resolveRoomSession("", "cia", "p", "R1"); !errors.Is(err, errRoomNickNotStored) {
		t.Fatalf("--nick cia with the variable set: err = %v, want errRoomNickNotStored (variable ignored)", err)
	}
	if got, err := resolveRoomSession("", "", "p", "R1"); err != nil || got != "env-s" {
		t.Fatalf("variable over a bare stored lookup = %q, %v", got, err)
	}
	if got, err := resolveRoomSession("flag-s", "", "p", "R1"); err != nil || got != "flag-s" {
		t.Fatalf("--session over the variable = %q, %v", got, err)
	}
	if _, err := resolveRoomSession("flag-s", "bia", "p", "R1"); !errors.Is(err, errRoomSessionAndNick) {
		t.Fatalf("--session with --nick: err = %v, want errRoomSessionAndNick", err)
	}
}

// B-0084, review round 2: a human who exported CERNE_ROOM_SESSION and then
// started an agent from that shell hands the variable to every agent and
// subagent. --nick still speaks as the stored nick, and join warns that the
// variable overrides bare commands.
func TestRoomJoinWithInheritedRoomSessionVariable(t *testing.T) {
	dir, port := newRoomTestServer(t, "proj-b0084-env")
	dirFlag := []string{"--dir", dir, "--port", strconv.Itoa(port)}
	runRoom(t, append([]string{"open", "topic"}, dirFlag...)...)
	t.Setenv("CERNE_SESSION", "")
	human := joinRoomAs(t, dirFlag, "R1", "hum")

	t.Setenv("CERNE_ROOM_SESSION", human)
	t.Setenv("CERNE_SESSION", "agent-1")
	out, err := runRoom(t, append([]string{"join", "R1", "--nick", "ana"}, dirFlag...)...)
	if err != nil {
		t.Fatalf("join ana: %v", err)
	}
	if !strings.Contains(out, "CERNE_ROOM_SESSION is set") || !strings.Contains(out, "--nick ana") || !strings.Contains(out, "room leave R1 --nick ana") {
		t.Fatalf("join output = %q, want the warning that the variable overrides bare commands, leave included", out)
	}
	if _, err := runRoom(t, append([]string{"say", "R1", "from ana", "--nick", "ana"}, dirFlag...)...); err != nil {
		t.Fatalf("say --nick ana: %v", err)
	}
	read, err := runRoom(t, append([]string{"read", "R1"}, dirFlag...)...)
	if err != nil || !strings.Contains(read, "[ana] from ana") {
		t.Fatalf("read = %q, %v; want the --nick message under ana, not the variable's nick", read, err)
	}
}

// B-0084, review round 1: parallel joins from one CERNE_SESSION (subagents
// of one parent) all end up stored — the read-modify-write is locked.
func TestRoomCredsConcurrentStoresKeepEveryNick(t *testing.T) {
	t.Setenv("CERNE_CACHE_DIR", t.TempDir())
	t.Setenv("CERNE_SESSION", "agent-1")
	const n = 16
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := storeRoomSession("p", "R1", "nick"+strconv.Itoa(i), "s"+strconv.Itoa(i))
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("storeRoomSession: %v", err)
		}
	}
	got, err := loadRoomCreds("p", "R1")
	if err != nil || len(got.Sessions) != n {
		t.Fatalf("stored %d nick(s), %v; want %d", len(got.Sessions), err, n)
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
	if _, err := storeRoomSession(c.pid, "R1", "ana", joined.Session.SessionID); err != nil {
		t.Fatalf("storeRoomSession: %v", err)
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
	if err != nil || !strings.Contains(out, "[open]") || strings.Contains(out, "via stored") {
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

// B-0084, review round 1: the room argument names the stored file's
// directory, so a path in it (say ../../x) is refused before anything is
// read or written outside room-creds/.
func TestRoomCredsRefuseRoomIDThatIsAPath(t *testing.T) {
	dir, port := newRoomTestServer(t, "proj-path")
	dirFlag := []string{"--dir", dir, "--port", strconv.Itoa(port)}
	t.Setenv("CERNE_SESSION", "agent-1")
	t.Setenv("CERNE_ROOM_SESSION", "")
	cache := os.Getenv("CERNE_CACHE_DIR")
	// where <cache>/proj-path/room-creds/../../x would land.
	planted := filepath.Join(cache, "x", hashSessionID("agent-1")+".json")
	if err := writePrivateFile(planted, []byte(`{"sessions":{"ana":"planted"}}`)); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"../../x", "a/b", "..", ""} {
		if got, err := resolveRoomSession("", "", "proj-path", id); !errors.Is(err, errRoomIDNotLocal) {
			t.Errorf("resolveRoomSession(%q) = %q, %v; want errRoomIDNotLocal", id, got, err)
		}
		if _, err := storeRoomSession("proj-path", id, "ana", "s"); !errors.Is(err, errRoomIDNotLocal) {
			t.Errorf("storeRoomSession(%q) err = %v, want errRoomIDNotLocal", id, err)
		}
	}
	if _, err := runRoom(t, append([]string{"say", "../../x", "oi"}, dirFlag...)...); !errors.Is(err, errRoomIDNotLocal) {
		t.Fatalf("say ../../x err = %v, want errRoomIDNotLocal", err)
	}
	if _, err := storeRoomSession("proj-path", "../../y", "ana", "s"); !errors.Is(err, errRoomIDNotLocal) {
		t.Fatalf("storeRoomSession(../../y) err = %v, want errRoomIDNotLocal", err)
	}
	if _, err := os.Stat(filepath.Join(cache, "y")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a store outside room-creds/: %v", err)
	}
}

// B-0084: `room leave --nick` resolves like every session command and, once
// the server revoked the session, drops only the entry that held it — so a
// bare command then picks the nick that is left, and a new join of the same
// nick is stored again. A refused leave (401) keeps the entry.
func TestRoomLeaveForgetsOnlyTheRevokedNick(t *testing.T) {
	dir, port := newRoomTestServer(t, "proj-leave-nick")
	dirFlag := []string{"--dir", dir, "--port", strconv.Itoa(port)}
	t.Setenv("CERNE_ROOM_SESSION", "")
	t.Setenv("CERNE_SESSION", "agent-1")
	runRoom(t, append([]string{"open", "topic"}, dirFlag...)...)
	ana := joinRoomAs(t, dirFlag, "R1", "ana")
	bia := joinRoomAs(t, dirFlag, "R1", "bia")

	if out, err := runRoom(t, append([]string{"leave", "R1", "--nick", "bia"}, dirFlag...)...); err != nil {
		t.Fatalf("leave --nick bia: %v (%s)", err, out)
	}
	if stored, _ := loadRoomCreds("proj-leave-nick", "R1"); !maps.Equal(stored.Sessions, map[string]string{"ana": ana}) {
		t.Fatalf("stored after leave = %+v, want only ana", stored)
	}
	if _, err := runRoom(t, append([]string{"say", "R1", "ghost", "--session", bia}, dirFlag...)...); err == nil {
		t.Fatal("say with bia's revoked session: want error")
	}
	if _, err := runRoom(t, append([]string{"say", "R1", "after leave"}, dirFlag...)...); err != nil {
		t.Fatalf("bare say after bia left: %v", err)
	}
	read, err := runRoom(t, append([]string{"read", "R1"}, dirFlag...)...)
	if err != nil || !strings.Contains(read, "[ana] after leave") {
		t.Fatalf("read = %q, %v; want the bare say under ana", read, err)
	}

	bia2 := joinRoomAs(t, dirFlag, "R1", "bia")
	if stored, _ := loadRoomCreds("proj-leave-nick", "R1"); bia2 == bia || stored.Sessions["bia"] != bia2 {
		t.Fatalf("stored after re-join = %+v, want bia's new session", stored)
	}

	if _, err := storeRoomSession("proj-leave-nick", "R1", "zed", "deadbeefdeadbeefdeadbeefdeadbeef"); err != nil {
		t.Fatal(err)
	}
	if _, err := runRoom(t, append([]string{"leave", "R1", "--nick", "zed"}, dirFlag...)...); err == nil {
		t.Fatal("leave with a session the server does not know: want error")
	}
	// T-0019: a 401 means the session is already dead, so the entry goes too.
	if stored, _ := loadRoomCreds("proj-leave-nick", "R1"); stored.Sessions["zed"] != "" {
		t.Fatalf("stored after a 401 leave = %+v, want zed dropped", stored)
	}
}

// B-0084: a leave the server fails (5xx) keeps the stored entry.
func TestRoomLeaveServerErrorKeepsTheEntry(t *testing.T) {
	t.Setenv("CERNE_CACHE_DIR", t.TempDir())
	t.Setenv("CERNE_ROOM_SESSION", "")
	t.Setenv("CERNE_SESSION", "agent-1")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"boom"}`))
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".cernerc"), []byte("project_id = \"proj-leave-5xx\"\nbackend = \"/dev/null\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := storeRoomSession("proj-leave-5xx", "R1", "ana", "s-ana"); err != nil {
		t.Fatal(err)
	}
	if _, err := runRoom(t, "leave", "R1", "--nick", "ana", "--dir", dir, "--port", u.Port()); err == nil {
		t.Fatal("leave against a failing server: want error")
	}
	if stored, _ := loadRoomCreds("proj-leave-5xx", "R1"); stored.Sessions["ana"] != "s-ana" {
		t.Fatalf("stored after a failed leave = %+v, want ana kept", stored)
	}
}
