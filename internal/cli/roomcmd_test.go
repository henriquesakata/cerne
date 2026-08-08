package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/room"
	"gitlab.com/rockt/cerne/internal/server"
)

// minimalMinutesJSON satisfies room close's required-field validation with
// no substantive content — for tests that only care about close mechanics.
const minimalMinutesJSON = `{"conclusion":"ok","rationale":"ok"}`

// newRoomTestServer starts a server.NewUIMulti host whose only registered
// project is pid, and writes a matching .cernerc (project_id = pid) under a
// fresh temp dir so newRoomClient resolves the same pid locally, with no
// network. Room handlers never touch repoDir/projDir, so both are left
// empty. Returns the project dir and the port to pass as --dir/--port.
func newRoomTestServer(t *testing.T, pid string) (dir string, port int) {
	t.Helper()
	// room state persists as jsonl on disk keyed by project.CacheDir(pid);
	// without this, nextID (and room ids like "R1") would carry over from
	// this pid's disk state left by a previous run of this test.
	t.Setenv("CERNE_CACHE_DIR", t.TempDir())
	resolve := func(d string) (string, string, string, string, error) {
		return "", "", pid, "Test Project", nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	handler, err := server.NewUIMulti(ctx, resolve, 0, "seed")
	if err != nil {
		t.Fatalf("NewUIMulti: %v", err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := httptest.NewUnstartedServer(handler)
	srv.Listener.Close()
	srv.Listener = ln
	srv.Start()
	t.Cleanup(srv.Close)

	u, _ := url.Parse(srv.URL)
	port, _ = strconv.Atoi(u.Port())

	dir = t.TempDir()
	cernerc := "project_id = \"" + pid + "\"\nbackend = \"/dev/null\"\n"
	if err := os.WriteFile(filepath.Join(dir, ".cernerc"), []byte(cernerc), 0o644); err != nil {
		t.Fatalf("write .cernerc: %v", err)
	}
	return dir, port
}

func runRoom(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := newRoomCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

// joinRoomAs joins nick via the CLI and returns the session capability
// printed by `room join` (F-0226).
func joinRoomAs(t *testing.T, dirFlag []string, roomID, nick string, extra ...string) string {
	t.Helper()
	args := append([]string{"join", roomID, "--nick", nick}, extra...)
	out, err := runRoom(t, append(args, dirFlag...)...)
	if err != nil {
		t.Fatalf("join %s: %v", nick, err)
	}
	for _, line := range strings.Split(out, "\n") {
		if tok, ok := strings.CutPrefix(line, "session: "); ok {
			return strings.TrimSpace(tok)
		}
	}
	t.Fatalf("join output = %q, want a session: line", out)
	return ""
}

// openRoomCLI opens a room via the CLI and returns its id and the
// facilitator capability printed by `room open` (F-0228).
func openRoomCLI(t *testing.T, dirFlag []string, topic string) (roomID, facilitator string) {
	t.Helper()
	out, err := runRoom(t, append([]string{"open", topic}, dirFlag...)...)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	for _, line := range strings.Split(out, "\n") {
		if tok, ok := strings.CutPrefix(line, "room "); ok {
			roomID = strings.SplitN(tok, " ", 2)[0]
		}
		if tok, ok := strings.CutPrefix(line, "facilitator capability: "); ok {
			facilitator = strings.TrimSpace(tok)
		}
	}
	if roomID == "" || facilitator == "" {
		t.Fatalf("open output = %q, want a room id and a facilitator capability line", out)
	}
	return roomID, facilitator
}

func TestRoomOpenPrintsEntryCommand(t *testing.T) {
	dir, port := newRoomTestServer(t, "proj-open")
	out, err := runRoom(t, "open", "how do we ship F-0211", "--dir", dir, "--port", strconv.Itoa(port))
	if err != nil {
		t.Fatalf("room open: %v", err)
	}
	if !strings.Contains(out, "room R1 opened") {
		t.Fatalf("output = %q, want room id R1 opened", out)
	}
	if !strings.Contains(out, "cerne room join R1 --nick") {
		t.Fatalf("output = %q, want a ready-to-paste join command", out)
	}
}

func TestRoomJoinDuplicateNickIsClearError(t *testing.T) {
	dir, port := newRoomTestServer(t, "proj-join")
	dirFlag := []string{"--dir", dir, "--port", strconv.Itoa(port)}
	if _, err := runRoom(t, append([]string{"open", "topic"}, dirFlag...)...); err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := runRoom(t, append([]string{"join", "R1", "--nick", "ana"}, dirFlag...)...); err != nil {
		t.Fatalf("first join: %v", err)
	}
	_, err := runRoom(t, append([]string{"join", "R1", "--nick", "ana"}, dirFlag...)...)
	if err == nil {
		t.Fatal("second join with same nick: want error, got nil")
	}
	if strings.Contains(err.Error(), "{") {
		t.Fatalf("error leaks raw JSON: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "already in use") {
		t.Fatalf("error = %q, want a clear duplicate-nick message", err.Error())
	}
}

func TestRoomSayThenRead(t *testing.T) {
	dir, port := newRoomTestServer(t, "proj-sayread")
	dirFlag := []string{"--dir", dir, "--port", strconv.Itoa(port)}
	runRoom(t, append([]string{"open", "topic"}, dirFlag...)...)
	sess := joinRoomAs(t, dirFlag, "R1", "ana")

	if _, err := runRoom(t, append([]string{"say", "R1", "--session", sess, "ola pessoal"}, dirFlag...)...); err != nil {
		t.Fatalf("say: %v", err)
	}
	out, err := runRoom(t, append([]string{"read", "R1"}, dirFlag...)...)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(out, "[ana] ola pessoal") {
		t.Fatalf("read output = %q, want the message formatted with nick", out)
	}

	// --since past the last seq: nothing new.
	out2, err := runRoom(t, append([]string{"read", "R1", "--since", "999"}, dirFlag...)...)
	if err != nil {
		t.Fatalf("read --since: %v", err)
	}
	if strings.TrimSpace(out2) != "" {
		t.Fatalf("read --since 999 = %q, want empty", out2)
	}
}

func TestRoomReadFollowSeesNewMessage(t *testing.T) {
	dir, port := newRoomTestServer(t, "proj-follow")
	dirFlag := []string{"--dir", dir, "--port", strconv.Itoa(port)}
	runRoom(t, append([]string{"open", "topic"}, dirFlag...)...)
	sess := joinRoomAs(t, dirFlag, "R1", "ana")

	c, err := newRoomClient(dir, port)
	if err != nil {
		t.Fatalf("newRoomClient: %v", err)
	}

	done := make(chan struct{})
	var out bytes.Buffer
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = roomReadLoop(ctx, &out, c, "R1", 0, true, false)
		close(done)
	}()

	time.Sleep(200 * time.Millisecond)
	if _, err := c.say(context.Background(), "R1", sess, "chegou"); err != nil {
		t.Fatalf("say: %v", err)
	}
	<-done
	if !strings.Contains(out.String(), "chegou") {
		t.Fatalf("follow output = %q, want it to contain the new message", out.String())
	}
}

func TestRoomReadOnceAndFollowMutuallyExclusive(t *testing.T) {
	dir, port := newRoomTestServer(t, "proj-once-follow")
	dirFlag := []string{"--dir", dir, "--port", strconv.Itoa(port)}
	runRoom(t, append([]string{"open", "topic"}, dirFlag...)...)

	_, err := runRoom(t, append([]string{"read", "R1", "--once", "--follow"}, dirFlag...)...)
	if err == nil {
		t.Fatal("--once --follow: want error, got nil")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("error = %q, want mutually exclusive message", err.Error())
	}
}

func TestRoomReadOnceExitsOnFirstBatch(t *testing.T) {
	dir, port := newRoomTestServer(t, "proj-once")
	dirFlag := []string{"--dir", dir, "--port", strconv.Itoa(port)}
	runRoom(t, append([]string{"open", "topic"}, dirFlag...)...)
	sess := joinRoomAs(t, dirFlag, "R1", "ana")

	c, err := newRoomClient(dir, port)
	if err != nil {
		t.Fatalf("newRoomClient: %v", err)
	}

	done := make(chan struct{})
	var out bytes.Buffer
	var loopErr error
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		loopErr = roomReadLoop(ctx, &out, c, "R1", 2, false, true)
		close(done)
	}()

	time.Sleep(200 * time.Millisecond)
	if _, err := c.say(context.Background(), "R1", sess, "chegou"); err != nil {
		t.Fatalf("say: %v", err)
	}
	<-done
	if loopErr != nil {
		t.Fatalf("roomReadLoop: %v", loopErr)
	}
	if !strings.Contains(out.String(), "#3 [ana] chegou") {
		t.Fatalf("once output = %q, want the new message with its seq", out.String())
	}
}

func TestRoomReadOnceOnClose(t *testing.T) {
	dir, port := newRoomTestServer(t, "proj-once-close")
	dirFlag := []string{"--dir", dir, "--port", strconv.Itoa(port)}
	_, facilitator := openRoomCLI(t, dirFlag, "topic")

	c, err := newRoomClient(dir, port)
	if err != nil {
		t.Fatalf("newRoomClient: %v", err)
	}

	done := make(chan struct{})
	var out bytes.Buffer
	var loopErr error
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		loopErr = roomReadLoop(ctx, &out, c, "R1", 1, false, true)
		close(done)
	}()

	time.Sleep(200 * time.Millisecond)
	if _, err := c.close(context.Background(), "R1", facilitator, room.Minutes{Conclusion: "ok", Rationale: "ok"}, false); err != nil {
		t.Fatalf("close: %v", err)
	}
	<-done
	if loopErr != nil {
		t.Fatalf("roomReadLoop: %v", loopErr)
	}
	if !strings.Contains(out.String(), "close") {
		t.Fatalf("once output on close = %q, want the close event", out.String())
	}
}

func TestRoomReadOnceUnknownRoomErrors(t *testing.T) {
	dir, port := newRoomTestServer(t, "proj-once-missing")
	dirFlag := []string{"--dir", dir, "--port", strconv.Itoa(port)}

	_, err := runRoom(t, append([]string{"read", "R1", "--once"}, dirFlag...)...)
	if err == nil {
		t.Fatal("read --once on unknown room: want error, got nil")
	}
}

func TestRoomOpenPrintsListenLine(t *testing.T) {
	dir, port := newRoomTestServer(t, "proj-listen")
	out, err := runRoom(t, "open", "topic", "--dir", dir, "--port", strconv.Itoa(port))
	if err != nil {
		t.Fatalf("room open: %v", err)
	}
	if !strings.Contains(out, "listen: cerne room read R1 --since 0 --once") {
		t.Fatalf("output = %q, want a listen: suggestion with --once", out)
	}
}

func TestRoomClose(t *testing.T) {
	dir, port := newRoomTestServer(t, "proj-close")
	dirFlag := []string{"--dir", dir, "--port", strconv.Itoa(port)}
	_, facilitator := openRoomCLI(t, dirFlag, "topic")

	out, err := runRoom(t, append([]string{"close", "R1", "--facilitator", facilitator, "--minutes", minimalMinutesJSON}, dirFlag...)...)
	if err != nil {
		t.Fatalf("close: %v", err)
	}
	if !strings.Contains(out, "room R1 closed") {
		t.Fatalf("output = %q", out)
	}

	// read still works post-close.
	if _, err := runRoom(t, append([]string{"read", "R1"}, dirFlag...)...); err != nil {
		t.Fatalf("read after close: %v", err)
	}
	// say is refused post-close.
	if _, err := runRoom(t, append([]string{"say", "R1", "--session", "deadbeef", "oi"}, dirFlag...)...); err == nil {
		t.Fatal("say after close: want error, got nil")
	}
}

func TestRoomJoinPrintsSessionAndNextSteps(t *testing.T) {
	dir, port := newRoomTestServer(t, "proj-join-session")
	dirFlag := []string{"--dir", dir, "--port", strconv.Itoa(port)}
	runRoom(t, append([]string{"open", "topic"}, dirFlag...)...)

	out, err := runRoom(t, append([]string{"join", "R1", "--nick", "ana"}, dirFlag...)...)
	if err != nil {
		t.Fatalf("join: %v", err)
	}
	sess := joinRoomAs(t, dirFlag, "R1", "bia") // helper parses the session: line
	if len(sess) < 32 {
		t.Fatalf("session = %q, want >= 32 chars", sess)
	}
	if !strings.Contains(out, "session: ") {
		t.Fatalf("output = %q, want the session line", out)
	}
	if !strings.Contains(out, "cerne room say R1 \"<message>\" --session ") {
		t.Fatalf("output = %q, want a copyable say example", out)
	}
	if !strings.Contains(out, "export CERNE_ROOM_SESSION=") {
		t.Fatalf("output = %q, want the env convenience line", out)
	}
	if !strings.Contains(out, "keep this session token") {
		t.Fatalf("output = %q, want the keep-the-token warning", out)
	}
	if strings.Contains(out, "watch") {
		t.Fatalf("output = %q, watch example must wait for F-0227", out)
	}
}

func TestRoomSayWithoutSessionIsLocalError(t *testing.T) {
	// no server needed: the check fires before any HTTP.
	_, err := runRoom(t, "say", "R1", "oi")
	if err == nil {
		t.Fatal("say without session: want error, got nil")
	}
	if !strings.Contains(err.Error(), "--session") || !strings.Contains(err.Error(), "CERNE_ROOM_SESSION") {
		t.Fatalf("error = %q, want both --session and CERNE_ROOM_SESSION mentioned", err.Error())
	}
	if !strings.Contains(err.Error(), "room join") {
		t.Fatalf("error = %q, want a pointer to room join", err.Error())
	}
}

func TestRoomSayNickFlagRemoved(t *testing.T) {
	_, err := runRoom(t, "say", "R1", "oi", "--nick", "ana")
	if err == nil || !strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("say --nick: err = %v, want unknown flag (contract migrated to --session)", err)
	}
}

func TestRoomSayEnvFallbackAndFlagPrecedence(t *testing.T) {
	dir, port := newRoomTestServer(t, "proj-say-env")
	dirFlag := []string{"--dir", dir, "--port", strconv.Itoa(port)}
	runRoom(t, append([]string{"open", "topic"}, dirFlag...)...)
	sess := joinRoomAs(t, dirFlag, "R1", "ana")

	// env fallback: no flag, valid capability in CERNE_ROOM_SESSION.
	t.Setenv("CERNE_ROOM_SESSION", sess)
	if _, err := runRoom(t, append([]string{"say", "R1", "via env"}, dirFlag...)...); err != nil {
		t.Fatalf("say via env: %v", err)
	}

	// flag precedence: bogus env, valid flag — the flag must win.
	t.Setenv("CERNE_ROOM_SESSION", "deadbeef")
	if _, err := runRoom(t, append([]string{"say", "R1", "--session", sess, "via flag"}, dirFlag...)...); err != nil {
		t.Fatalf("say with flag over bogus env: %v", err)
	}

	// bogus env alone is refused by the server (401).
	if _, err := runRoom(t, append([]string{"say", "R1", "still bogus"}, dirFlag...)...); err == nil {
		t.Fatal("say with bogus env capability: want error, got nil")
	}
}

func TestRoomReadRendersJoinWithNickAndBio(t *testing.T) {
	dir, port := newRoomTestServer(t, "proj-read-join")
	dirFlag := []string{"--dir", dir, "--port", strconv.Itoa(port)}
	runRoom(t, append([]string{"open", "topic"}, dirFlag...)...)
	joinRoomAs(t, dirFlag, "R1", "ana", "--bio", "pessimista de plantão")
	joinRoomAs(t, dirFlag, "R1", "bob")

	out, err := runRoom(t, append([]string{"read", "R1"}, dirFlag...)...)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(out, "[join: ana — pessimista de plantão]") {
		t.Fatalf("read output = %q, want join rendered with nick and bio", out)
	}
	if !strings.Contains(out, "[join: bob]") {
		t.Fatalf("read output = %q, want join without bio rendered without separator", out)
	}
	if !strings.Contains(out, "#1 [open]") {
		t.Fatalf("read output = %q, want open still rendered as a system event", out)
	}
}

func TestRoomListFormatsRooms(t *testing.T) {
	dir, port := newRoomTestServer(t, "proj-list")
	dirFlag := []string{"--dir", dir, "--port", strconv.Itoa(port)}
	runRoom(t, append([]string{"open", "topico um"}, dirFlag...)...)
	runRoom(t, append([]string{"join", "R1", "--nick", "ana"}, dirFlag...)...)

	out, err := runRoom(t, append([]string{"list"}, dirFlag...)...)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(out, "R1") || !strings.Contains(out, "open") || !strings.Contains(out, "1 participant") || !strings.Contains(out, "topico um") {
		t.Fatalf("list output = %q, want id/status/count/topic", out)
	}
}

// --- F-0228: facilitator, items, minutes ---

func TestRoomFacilitatorHandoffCmd(t *testing.T) {
	dir, port := newRoomTestServer(t, "proj-handoff")
	dirFlag := []string{"--dir", dir, "--port", strconv.Itoa(port)}
	_, facilitator := openRoomCLI(t, dirFlag, "topic")

	out, err := runRoom(t, append([]string{"facilitator", "handoff", "R1", "--facilitator", facilitator}, dirFlag...)...)
	if err != nil {
		t.Fatalf("handoff: %v", err)
	}
	if !strings.Contains(out, "new facilitator capability: ") {
		t.Fatalf("output = %q, want the new capability line", out)
	}
	if strings.Contains(out, facilitator) {
		t.Fatalf("output = %q, leaks the old capability", out)
	}

	// the old capability no longer closes the room.
	if _, err := runRoom(t, append([]string{"close", "R1", "--facilitator", facilitator, "--minutes", minimalMinutesJSON}, dirFlag...)...); err == nil {
		t.Fatal("close with rotated-out capability: want error, got nil")
	}
}

func TestRoomProposeAndRequestDecisionCmd(t *testing.T) {
	dir, port := newRoomTestServer(t, "proj-propose")
	dirFlag := []string{"--dir", dir, "--port", strconv.Itoa(port)}
	openRoomCLI(t, dirFlag, "topic")
	sess := joinRoomAs(t, dirFlag, "R1", "ana")

	out, err := runRoom(t, append([]string{"propose", "R1", "--session", sess, "--title", "usar postgres", "--body", "menos manutenção"}, dirFlag...)...)
	if err != nil {
		t.Fatalf("propose: %v", err)
	}
	if !strings.Contains(out, "created") {
		t.Fatalf("propose output = %q, want confirmation", out)
	}

	out, err = runRoom(t, append([]string{"request-decision", "R1", "--session", sess, "--title", "qual banco?"}, dirFlag...)...)
	if err != nil {
		t.Fatalf("request-decision: %v", err)
	}
	if !strings.Contains(out, "created") {
		t.Fatalf("request-decision output = %q, want confirmation", out)
	}
}

func TestRoomPendingAndResolveCmd(t *testing.T) {
	dir, port := newRoomTestServer(t, "proj-pending")
	dirFlag := []string{"--dir", dir, "--port", strconv.Itoa(port)}
	_, facilitator := openRoomCLI(t, dirFlag, "topic")
	sess := joinRoomAs(t, dirFlag, "R1", "ana")
	runRoom(t, append([]string{"propose", "R1", "--session", sess, "--title", "usar postgres", "--body", "menos manutenção"}, dirFlag...)...)

	out, err := runRoom(t, append([]string{"pending", "R1", "--facilitator", facilitator}, dirFlag...)...)
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	if !strings.Contains(out, "usar postgres") || !strings.Contains(out, "ana") {
		t.Fatalf("pending output = %q, want the open proposal listed", out)
	}

	// a regular session cannot resolve.
	itemID := strings.Fields(out)[0]
	if _, err := runRoom(t, append([]string{"resolve", "R1", itemID, "--facilitator", "deadbeefdeadbeefdeadbeefdeadbeef", "--result", "aceito"}, dirFlag...)...); err == nil {
		t.Fatal("resolve with a bogus facilitator capability: want error, got nil")
	}

	out, err = runRoom(t, append([]string{"resolve", "R1", itemID, "--facilitator", facilitator, "--result", "aceito"}, dirFlag...)...)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !strings.Contains(out, "resolved") || !strings.Contains(out, "aceito") {
		t.Fatalf("resolve output = %q", out)
	}

	out, err = runRoom(t, append([]string{"pending", "R1", "--facilitator", facilitator}, dirFlag...)...)
	if err != nil {
		t.Fatalf("pending after resolve: %v", err)
	}
	if !strings.Contains(out, "no pending items") {
		t.Fatalf("pending after resolve = %q, want empty queue", out)
	}
}

func TestRoomCloseBlocksOnOpenItemsAndListsThem(t *testing.T) {
	dir, port := newRoomTestServer(t, "proj-close-blocked")
	dirFlag := []string{"--dir", dir, "--port", strconv.Itoa(port)}
	_, facilitator := openRoomCLI(t, dirFlag, "topic")
	sess := joinRoomAs(t, dirFlag, "R1", "ana")
	runRoom(t, append([]string{"propose", "R1", "--session", sess, "--title", "usar postgres"}, dirFlag...)...)

	out, err := runRoom(t, append([]string{"close", "R1", "--facilitator", facilitator, "--minutes", minimalMinutesJSON}, dirFlag...)...)
	if err == nil {
		t.Fatal("close with an open item: want error, got nil")
	}
	if !strings.Contains(out, "usar postgres") {
		t.Fatalf("close-refused output = %q, want the pending item listed", out)
	}

	// --allow-open-items forces it through.
	_, err = runRoom(t, append([]string{"close", "R1", "--facilitator", facilitator, "--minutes", minimalMinutesJSON, "--allow-open-items"}, dirFlag...)...)
	if err != nil {
		t.Fatalf("close --allow-open-items: %v", err)
	}
}

func TestRoomCloseRejectsIncompleteMinutes(t *testing.T) {
	dir, port := newRoomTestServer(t, "proj-close-incomplete")
	dirFlag := []string{"--dir", dir, "--port", strconv.Itoa(port)}
	_, facilitator := openRoomCLI(t, dirFlag, "topic")

	_, err := runRoom(t, append([]string{"close", "R1", "--facilitator", facilitator, "--minutes", `{"conclusion":"","rationale":""}`}, dirFlag...)...)
	if err == nil {
		t.Fatal("close with incomplete minutes: want error, got nil")
	}
}

func TestRoomMinutesTemplateCmd(t *testing.T) {
	out, err := runRoom(t, "minutes", "template")
	if err != nil {
		t.Fatalf("minutes template: %v", err)
	}
	var mn room.Minutes
	if err := json.Unmarshal([]byte(out), &mn); err != nil {
		t.Fatalf("minutes template output %q is not valid JSON: %v", out, err)
	}
}

func TestRoomMinutesShowMarkdownCmd(t *testing.T) {
	dir, port := newRoomTestServer(t, "proj-minutes-show")
	dirFlag := []string{"--dir", dir, "--port", strconv.Itoa(port)}
	_, facilitator := openRoomCLI(t, dirFlag, "topic")

	if _, err := runRoom(t, append([]string{"minutes", "show", "R1"}, dirFlag...)...); err == nil {
		t.Fatal("minutes show on an open room: want error, got nil")
	}

	closeBody := `{"conclusion":"reverter o deploy","rationale":"mais rapido que corrigir ao vivo"}`
	if _, err := runRoom(t, append([]string{"close", "R1", "--facilitator", facilitator, "--minutes", closeBody}, dirFlag...)...); err != nil {
		t.Fatalf("close: %v", err)
	}

	out, err := runRoom(t, append([]string{"minutes", "show", "R1"}, dirFlag...)...)
	if err != nil {
		t.Fatalf("minutes show: %v", err)
	}
	if !strings.Contains(out, "reverter o deploy") || !strings.Contains(out, "## Conclusão") {
		t.Fatalf("minutes show output = %q, want the rendered markdown", out)
	}
}

func TestRoomNoServerRunningIsClearError(t *testing.T) {
	dir := t.TempDir()
	cernerc := "project_id = \"proj-none\"\nbackend = \"/dev/null\"\n"
	if err := os.WriteFile(filepath.Join(dir, ".cernerc"), []byte(cernerc), 0o644); err != nil {
		t.Fatalf("write .cernerc: %v", err)
	}
	// A closed listener's port: nothing answers there.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	_, err = runRoom(t, "list", "--dir", dir, "--port", strconv.Itoa(port))
	if err == nil {
		t.Fatal("want error when no cerne ui is running")
	}
	if !strings.Contains(err.Error(), "cerne ui") {
		t.Fatalf("error = %q, want it to mention 'cerne ui'", err.Error())
	}
}
