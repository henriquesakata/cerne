package server

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/project"
	"gitlab.com/rockt/cerne/internal/store"
)

// newGitBackendNoClean mirrors newGitBackend but manages its own temp dir with
// a best-effort RemoveAll. Tests that drive watchHead/pullLoop in goroutines
// keep the go-git clone open; on Windows, t.TempDir's strict cleanup would fail
// to unlink files still held by an in-flight git op. Swallowing the cleanup
// error keeps the test about behaviour, not the OS's unlink timing.
func newGitBackendNoClean(t *testing.T) (repoDir, projDir, origin string) {
	t.Helper()
	base, err := os.MkdirTemp("", "cerne-live")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(base) })

	origin = filepath.Join(base, "origin.git")
	if err := gitback.InitBare(origin); err != nil {
		t.Fatalf("InitBare: %v", err)
	}
	repoDir = filepath.Join(base, "repo")
	if err := gitback.Clone(origin, repoDir); err != nil {
		t.Fatalf("Clone: %v", err)
	}
	if err := store.InitLayout(repoDir, store.Project{Name: "Projeto Demo"}); err != nil {
		t.Fatalf("InitLayout: %v", err)
	}
	seedFeature(t, repoDir, "F-0001", "Feature A", domain.StateCandidate)
	if _, err := gitback.CommitAndPush(repoDir, "seed"); err != nil {
		t.Fatalf("CommitAndPush seed: %v", err)
	}
	projDir = filepath.Join(base, "proj")
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := project.SaveLock(projDir, project.Lock{Version: 1, Backend: origin}); err != nil {
		t.Fatalf("SaveLock: %v", err)
	}
	return repoDir, projDir, origin
}

// subCount reads the hub's subscriber count under its lock (same-package test).
func subCount(h *liveHub) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}

// readSSEData reads one SSE event from br and returns its data value, skipping
// comment/keepalive lines and the "event:" line.
func readSSEData(br *bufio.Reader) (string, error) {
	var data string
	have := false
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return "", err
		}
		line = strings.TrimRight(line, "\r\n")
		switch {
		case line == "":
			if have {
				return data, nil
			}
		case strings.HasPrefix(line, ":"):
			// comment / keepalive — ignore
		case strings.HasPrefix(line, "data:"):
			data = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			have = true
		}
	}
}

func waitEvent(t *testing.T, events <-chan string) string {
	t.Helper()
	select {
	case e := <-events:
		return e
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for SSE event")
		return ""
	}
}

func waitFor(t *testing.T, cond func() bool, timeout time.Duration, what string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out: %s", what)
}

// openStream connects to a test server serving hub.handleStream(repoDir) and
// returns a channel of the data values it streams plus a cancel that simulates
// the client disconnecting. Cleanup tears everything down.
func openStream(t *testing.T, hub *liveHub, repoDir string) (<-chan string, context.CancelFunc) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/stream", func(w http.ResponseWriter, r *http.Request) {
		hub.handleStream(w, r, repoDir)
	})
	srv := httptest.NewServer(mux)
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/stream", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		srv.Close()
		t.Fatalf("GET /api/stream: %v", err)
	}
	events := make(chan string, 8)
	go func() {
		br := bufio.NewReader(resp.Body)
		for {
			data, err := readSSEData(br)
			if err != nil {
				return
			}
			events <- data
		}
	}()
	t.Cleanup(func() {
		cancel()
		resp.Body.Close()
		srv.Close()
	})
	return events, cancel
}

func TestHubBroadcastsToSubscribers(t *testing.T) {
	hub := newLiveHub()
	a := hub.subscribe()
	b := hub.subscribe()

	hub.broadcast("sha-1")
	if got := <-a; got != "sha-1" {
		t.Fatalf("subscriber a got %q, want sha-1", got)
	}
	if got := <-b; got != "sha-1" {
		t.Fatalf("subscriber b got %q, want sha-1", got)
	}

	// A full buffer must not block broadcast: nobody drains between these two.
	hub.broadcast("sha-2")
	hub.broadcast("sha-3")
	if got := <-a; got != "sha-2" {
		t.Fatalf("subscriber a second got %q, want sha-2 (sha-3 dropped on full buffer)", got)
	}
}

func TestHubUnsubscribe(t *testing.T) {
	hub := newLiveHub()
	ch := hub.subscribe()
	if subCount(hub) != 1 {
		t.Fatalf("subs=%d, want 1 after subscribe", subCount(hub))
	}
	hub.unsubscribe(ch)
	if subCount(hub) != 0 {
		t.Fatalf("subs=%d, want 0 after unsubscribe", subCount(hub))
	}
	if _, ok := <-ch; ok {
		t.Fatal("channel should be closed after unsubscribe")
	}
	hub.unsubscribe(ch) // idempotent: must not panic on a closed/absent channel
}

func TestStreamSendsCurrentShaOnConnect(t *testing.T) {
	repoDir, _, _ := newGitBackend(t)
	hub := newLiveHub()
	events, _ := openStream(t, hub, repoDir)

	head, err := gitback.HeadSHA(repoDir)
	if err != nil {
		t.Fatalf("HeadSHA: %v", err)
	}
	if got := waitEvent(t, events); got != head {
		t.Fatalf("connect frame = %q, want current HEAD %q", got, head)
	}
}

func TestStreamEmitsChangeOnHeadChange(t *testing.T) {
	repoDir, _, _ := newGitBackendNoClean(t)
	hub := newLiveHub()
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); hub.watchHead(ctx, repoDir, 10*time.Millisecond) }()
	defer wg.Wait() // runs after cancel: no git op in flight at TempDir cleanup
	defer cancel()

	events, _ := openStream(t, hub, repoDir)
	waitEvent(t, events) // drain the connect frame

	seedFeature(t, repoDir, "F-0002", "Feature B", domain.StateCandidate)
	sha2, err := gitback.CommitAndPush(repoDir, "second")
	if err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}
	if got := waitEvent(t, events); got != sha2 {
		t.Fatalf("change frame = %q, want new HEAD %q", got, sha2)
	}
}

func TestStreamUnsubscribesOnClientCancel(t *testing.T) {
	repoDir, _, _ := newGitBackend(t)
	hub := newLiveHub()
	events, cancel := openStream(t, hub, repoDir)

	waitEvent(t, events) // connect frame ⇒ the handler has subscribed
	if n := subCount(hub); n != 1 {
		t.Fatalf("subs=%d, want 1 while connected", n)
	}

	cancel() // client disconnects
	waitFor(t, func() bool { return subCount(hub) == 0 }, 3*time.Second,
		"handler did not unsubscribe after client cancel")
}

func TestPullLoopAdvancesHeadFromRemote(t *testing.T) {
	repoDir, _, origin := newGitBackendNoClean(t)

	// A second clone of the same origin (sibling of origin, under the same
	// best-effort temp base) simulates a teammate pushing.
	mate := filepath.Join(filepath.Dir(origin), "mate")
	if err := gitback.Clone(origin, mate); err != nil {
		t.Fatalf("Clone mate: %v", err)
	}
	seedFeature(t, mate, "F-0002", "Feature B", domain.StateCandidate)
	sha, err := gitback.CommitAndPush(mate, "remote change")
	if err != nil {
		t.Fatalf("CommitAndPush mate: %v", err)
	}

	hub := newLiveHub()
	sub := hub.subscribe()
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); hub.watchHead(ctx, repoDir, 10*time.Millisecond) }()
	go func() { defer wg.Done(); pullLoop(ctx, repoDir, 10*time.Millisecond) }()
	defer wg.Wait() // stop the loops before the clone's temp dir is removed
	defer cancel()

	select {
	case got := <-sub:
		if got != sha {
			t.Fatalf("broadcast sha=%q, want remote HEAD %q", got, sha)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("pull loop did not bring the remote commit to the dashboard")
	}
}
