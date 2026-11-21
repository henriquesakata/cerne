package cli

import (
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/app"
)

// TestMsgSendWakes (F-0491 crit. 1 and 4): `cerne msg send` naming another
// work wakes that work's idle session through its inbox socket, and the
// message is delivered there.
func TestMsgSendWakes(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	cacheDir := filepath.Dir(repoDir)
	startBoundWork(t, projDir, repoDir, "sess-a", "Mexer no servidor")
	b := startBoundWork(t, projDir, repoDir, "sess-b", "Revisar a interface")
	if err := app.RecordSessionMode(cacheDir, "sess-b", "", "claude-code", "auto"); err != nil {
		t.Fatal(err)
	}

	dir, err := os.MkdirTemp("/tmp", "cw") // under the socket path limit
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "s.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	var mu sync.Mutex
	var got []string
	var wg sync.WaitGroup
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				raw, _ := io.ReadAll(c)
				c.Close()
				mu.Lock()
				got = append(got, string(raw))
				mu.Unlock()
			}()
		}
	}()
	claude := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", claude)
	raw, _ := json.Marshal(map[string]any{"pid": 5, "sessionId": "sess-b", "messagingSocketPath": sock, "status": "idle"})
	if err := os.MkdirAll(filepath.Join(claude, "sessions"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(claude, "sessions", "5.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}

	if out, err := execCerne(t, "msg", "send", b+" pode revisar?", "--session", "sess-a", "--dir", projDir); err != nil {
		t.Fatalf("msg send: %v\n%s", err, out)
	}
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 || !strings.Contains(got[0], `"session_id":"sess-b"`) || !strings.Contains(got[0], "pode revisar?") {
		t.Fatalf("inbox = %q, want one line for sess-b", got)
	}
	out, err := execCerne(t, "msg", "read", "--json", "--dir", projDir)
	if err != nil || !strings.Contains(out, `"state": "delivered"`) {
		t.Fatalf("msg read: %v\n%s", err, out)
	}
}

// TestHookPromptWokenTurn (F-0491 crit. 5): in the turn a wake opened, the
// UserPromptSubmit hook — whose prompt is the envelope written to the inbox —
// does not repeat what woke the session, and the PreToolUse after it does not
// either; the next prompt the human types shows it again (F-0490).
func TestHookPromptWokenTurn(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	cacheDir := filepath.Dir(repoDir)
	startBoundWork(t, projDir, repoDir, "sess-a", "Mexer no servidor")
	b := startBoundWork(t, projDir, repoDir, "sess-b", "Revisar a interface")
	if out, err := execCerne(t, "msg", "send", b+" pode revisar o painel?", "--session", "sess-a", "--dir", projDir); err != nil {
		t.Fatalf("msg send: %v\n%s", err, out)
	}
	// What a successful wake records and writes (no live inbox here).
	woke, err := app.ChannelWake(cacheDir, repoDir, "sess-b", time.Now().UTC())
	if err != nil || !strings.Contains(woke, "pode revisar o painel?") {
		t.Fatalf("wake text = %q, %v", woke, err)
	}
	envelope := "<cross-session-message from-name=\"cerne channel\">\n" + strings.TrimRight(woke, "\n") + "\n</cross-session-message>"

	out, err := execCerneStdin(t, promptJSON(t, "sess-b", projDir, envelope), "hook", "prompt-notes")
	if err != nil || strings.Contains(out, "pode revisar o painel?") {
		t.Fatalf("woken UserPromptSubmit repeated the message: %v %q", err, out)
	}
	tool := `{"session_id":"sess-b","cwd":"` + projDir + `","hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"ls"}}`
	if out, err := execCerneStdin(t, tool, "hook", "tool-notes"); err != nil || strings.Contains(out, "pode revisar o painel?") {
		t.Fatalf("woken PreToolUse repeated the message: %v %q", err, out)
	}
	if out, _ := execCerneStdin(t, promptJSON(t, "sess-b", projDir, "e aí?"), "hook", "prompt-notes"); !strings.Contains(out, "pode revisar o painel?") {
		t.Fatalf("the next typed prompt lost the unanswered message: %q", out)
	}
}
