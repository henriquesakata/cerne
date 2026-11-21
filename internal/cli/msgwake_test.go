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
