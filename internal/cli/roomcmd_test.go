package cli

import (
	"bytes"
	"context"
	"net"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/server"
)

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
	runRoom(t, append([]string{"join", "R1", "--nick", "ana"}, dirFlag...)...)

	if _, err := runRoom(t, append([]string{"say", "R1", "--nick", "ana", "ola pessoal"}, dirFlag...)...); err != nil {
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
	runRoom(t, append([]string{"join", "R1", "--nick", "ana"}, dirFlag...)...)

	c, err := newRoomClient(dir, port)
	if err != nil {
		t.Fatalf("newRoomClient: %v", err)
	}

	done := make(chan struct{})
	var out bytes.Buffer
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = roomReadLoop(ctx, &out, c, "R1", 0, true)
		close(done)
	}()

	time.Sleep(200 * time.Millisecond)
	if _, err := c.say(context.Background(), "R1", "ana", "chegou"); err != nil {
		t.Fatalf("say: %v", err)
	}
	<-done
	if !strings.Contains(out.String(), "chegou") {
		t.Fatalf("follow output = %q, want it to contain the new message", out.String())
	}
}

func TestRoomClose(t *testing.T) {
	dir, port := newRoomTestServer(t, "proj-close")
	dirFlag := []string{"--dir", dir, "--port", strconv.Itoa(port)}
	runRoom(t, append([]string{"open", "topic"}, dirFlag...)...)

	out, err := runRoom(t, append([]string{"close", "R1"}, dirFlag...)...)
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
	if _, err := runRoom(t, append([]string{"say", "R1", "--nick", "ana", "oi"}, dirFlag...)...); err == nil {
		t.Fatal("say after close: want error, got nil")
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
