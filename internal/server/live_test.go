package server

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
)

// broadcastWait bounds how long a test blocks for an SSE/registry broadcast.
// A healthy path delivers the event in milliseconds, so this ceiling only ever
// caps the failure path. It is set generously because the shared GitLab CI
// runner can stall a commit+push+poll cycle well past a few seconds under load;
// a tight deadline turned that scheduling jitter into a flaky failure.
const broadcastWait = 20 * time.Second

// newGitBackendNoClean mirrors newGitBackend but manages its own temp dir with
// a best-effort RemoveAll. Tests that drive watchHeadMulti/pullLoop in
// goroutines keep the go-git clone open; on Windows, t.TempDir's strict cleanup
// would fail to unlink files still held by an in-flight git op. Swallowing the
// cleanup error keeps the test about behaviour, not the OS's unlink timing.
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
	case <-time.After(broadcastWait):
		t.Fatal("timed out waiting for SSE event")
		return ""
	}
}

// The pull tick's own invariants (dirty-worktree skip, ff-only reset — the
// N-0027 poison) are pinned in internal/gitback's SafePull tests; here only the
// loop wiring is covered.

// TestPullLoopAdvancesHeadFromRemote proves pullLoop brings a teammate's push to
// the dashboard: with watchHeadMulti and pullLoop on the same clone, a subscriber
// receives the remote HEAD as a "change" event. pullLoop is the only surviving
// piece of the original single-project live path (case 0034) — the multiplexed
// hub of `cerne ui` drives it now.
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

	hub := newMultiHub(nil)
	sub := hub.subscribe()
	ctx, cancel := context.WithCancel(context.Background())
	baseline, _ := gitback.HeadSHA(repoDir) // HEAD before pullLoop brings the remote commit
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); watchHeadMulti(ctx, hub, "proj-a", repoDir, 10*time.Millisecond, baseline) }()
	go func() { defer wg.Done(); pullLoop(ctx, repoDir, 10*time.Millisecond) }()
	defer wg.Wait() // stop the loops before the clone's temp dir is removed
	defer cancel()

	select {
	case ev := <-sub:
		if ev.kind != "change" || ev.pid != "proj-a" || ev.sha != sha {
			t.Fatalf("change event = %+v, want kind=change pid=proj-a sha=%q", ev, sha)
		}
	case <-time.After(broadcastWait):
		t.Fatal("pull loop did not bring the remote commit to the dashboard")
	}
}
