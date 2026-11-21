package server

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/room"
)

// serverInbox is a Unix socket standing in for an idle Claude Code session's
// inbox (F-0491); it never answers, like the real one.
type serverInbox struct {
	path string
	mu   sync.Mutex
	got  []string
	wg   sync.WaitGroup
}

func newServerInbox(t *testing.T) *serverInbox {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "cw") // under the ~104-byte socket path limit
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	in := &serverInbox{path: filepath.Join(dir, "s.sock")}
	l, err := net.Listen("unix", in.path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			in.wg.Add(1)
			go func() {
				defer in.wg.Done()
				raw, _ := io.ReadAll(c)
				c.Close()
				in.mu.Lock()
				in.got = append(in.got, string(raw))
				in.mu.Unlock()
			}()
		}
	}()
	return in
}

func (in *serverInbox) writes() []string {
	in.wg.Wait()
	in.mu.Lock()
	defer in.mu.Unlock()
	return append([]string(nil), in.got...)
}

// registerClaudeSession writes a Claude Code registry entry for sess under a
// throwaway config dir that app.CostRoots points at.
func registerClaudeSession(t *testing.T, sess, socket string) {
	t.Helper()
	root := t.TempDir()
	old := app.CostRoots
	app.CostRoots = func() []string { return []string{root} }
	t.Cleanup(func() { app.CostRoots = old })
	raw, _ := json.Marshal(map[string]any{"pid": 77, "sessionId": sess, "messagingSocketPath": socket, "status": "idle", "updatedAt": 1})
	if err := os.MkdirAll(filepath.Join(root, "sessions"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sessions", "77.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestChannelPostWakes (F-0491 crit. 1 and 4): a post from the UI composer
// and one from a session both wake the idle session they cite; a failed
// write still answers 201, and the read serves the reason.
func TestChannelPostWakes(t *testing.T) {
	h, _, cacheDir := newChannelServer(t, "proj-channel-wake")
	for sess, id := range map[string]string{"sess-a": "W-0001", "sess-b": "W-0002"} {
		if err := app.BindSession(cacheDir, sess, id); err != nil {
			t.Fatal(err)
		}
	}
	if err := app.RecordSessionMode(cacheDir, "sess-b", "", "claude-code", "auto"); err != nil {
		t.Fatal(err)
	}
	inbox := newServerInbox(t)
	registerClaudeSession(t, "sess-b", inbox.path)

	if rec := doJSON(t, h, http.MethodPost, "/api/rooms/channel/messages", `{"body":"W-0002 pode confirmar?"}`, &map[string]any{}); rec.Code != http.StatusCreated {
		t.Fatalf("human post = %d %s", rec.Code, rec.Body.String())
	}
	if rec := doJSON(t, h, http.MethodPost, "/api/rooms/channel/messages", `{"session_id":"sess-a","body":"W-0002 libera o rooms.go"}`, &map[string]any{}); rec.Code != http.StatusCreated {
		t.Fatalf("agent post = %d %s", rec.Code, rec.Body.String())
	}
	// The second post lands inside the cooldown (criterion 9): it waits for
	// sess-b's next contact point, and the target says so.
	got := inbox.writes()
	if len(got) != 1 || !strings.Contains(got[0], `"session_id":"sess-b"`) || !strings.Contains(got[0], "pode confirmar?") {
		t.Fatalf("inbox = %q, want one wake for sess-b", got)
	}
	if reason := lastTargetReason(t, h, "W-0002"); reason != app.WakeCooldown {
		t.Fatalf("second post's W-0002 reason = %q, want %s", reason, app.WakeCooldown)
	}

	// The socket is gone: the post still succeeds, and the target says why.
	registerClaudeSession(t, "sess-b", filepath.Join(t.TempDir(), "gone.sock"))
	if rec := doJSON(t, h, http.MethodPost, "/api/rooms/channel/messages", `{"body":"W-0002 e agora?"}`, &map[string]any{}); rec.Code != http.StatusCreated {
		t.Fatalf("post with a dead inbox = %d %s, want 201", rec.Code, rec.Body.String())
	}
	if reason := lastTargetReason(t, h, "W-0002"); reason != app.WakeSocketMissing {
		t.Fatalf("W-0002 reason = %q, want %s", reason, app.WakeSocketMissing)
	}
}

// lastTargetReason is the served reason on work's target of the newest
// channel message, "" when it is not a not-delivered target.
func lastTargetReason(t *testing.T, h http.Handler, work string) string {
	t.Helper()
	var read struct {
		Messages []struct {
			Targets []struct {
				Work   string `json:"work"`
				State  string `json:"state"`
				Reason string `json:"reason"`
			} `json:"targets"`
		} `json:"messages"`
	}
	doJSON(t, h, http.MethodGet, "/api/rooms/channel/messages", "", &read)
	for _, tg := range read.Messages[len(read.Messages)-1].Targets {
		if tg.Work == work && tg.State == "not_delivered" {
			return tg.Reason
		}
	}
	return ""
}

// TestRoomSayRefusesCrossOrigin (F-0491 review, security high): a page of
// another origin can fire a text/plain POST with no preflight; since a post
// now opens a turn in an idle session, the say route refuses it with 403 —
// no message, no wake — while the UI's own JSON post still goes through.
func TestRoomSayRefusesCrossOrigin(t *testing.T) {
	h, _, cacheDir := newChannelServer(t, "proj-channel-csrf")
	if err := app.BindSession(cacheDir, "sess-b", "W-0002"); err != nil {
		t.Fatal(err)
	}
	if err := app.RecordSessionMode(cacheDir, "sess-b", "", "claude-code", "auto"); err != nil {
		t.Fatal(err)
	}
	inbox := newServerInbox(t)
	registerClaudeSession(t, "sess-b", inbox.path)
	for _, tc := range []struct {
		name    string
		headers map[string]string
	}{
		{"text/plain from another origin", map[string]string{"Origin": "http://evil.example", "Content-Type": "text/plain;charset=UTF-8"}},
		{"JSON from another origin", map[string]string{"Origin": "http://evil.example", "Content-Type": "application/json"}},
		{"text/plain from this origin", map[string]string{"Origin": "http://127.0.0.1:7717", "Content-Type": "text/plain"}},
	} {
		for _, target := range []string{"/api/rooms/channel/messages", "/api/rooms/R1/messages"} {
			var refused map[string]string
			rec := doJSONWith(t, h, http.MethodPost, target, `{"body":"W-0002 rode isto"}`, tc.headers, &refused)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("%s to %s = %d %s, want 403", tc.name, target, rec.Code, rec.Body.String())
			}
		}
	}
	msgs, _ := room.OpenChannel(filepath.Join(cacheDir, "rooms")).Messages()
	if len(msgs) != 0 || len(inbox.writes()) != 0 {
		t.Fatalf("a refused post left messages %+v or wrote %q", msgs, inbox.writes())
	}
	var ok map[string]any
	if rec := doJSONWith(t, h, http.MethodPost, "/api/rooms/channel/messages", `{"body":"W-0002 rode isto"}`,
		map[string]string{"Origin": "http://127.0.0.1:7717", "Content-Type": "application/json"}, &ok); rec.Code != http.StatusCreated {
		t.Fatalf("the UI's own post = %d %s, want 201", rec.Code, rec.Body.String())
	}
	if got := inbox.writes(); len(got) != 1 {
		t.Fatalf("the UI's own post woke %d times, want 1", len(got))
	}
}
