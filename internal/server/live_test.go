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

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/project"
	"gitlab.com/rockt/cerne/internal/store"
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

// TestPullTickDirtyWorktreeNeverDivergesRefFromFiles pins the F-0192/N-0027
// poison: go-git's Worktree.Pull moves refs/heads/main to the fetched head
// BEFORE its MergeReset, and MergeReset aborts with ErrUnstagedChanges when the
// shared clone has uncommitted writes (e.g. a CLI compile's LLM synthesis in
// flight). The branch ref then points at the new head while index and files
// stay on the old tree — a poisoned clone whose next sweep commit reverts the
// remote advance. The invariant: a pull tick either moves ref AND files
// together, or moves nothing; uncommitted writes survive untouched.
func TestPullTickDirtyWorktreeNeverDivergesRefFromFiles(t *testing.T) {
	repoDir, _, origin := newGitBackendNoClean(t)

	wiki := filepath.Join(repoDir, "wiki.md")
	notes := filepath.Join(repoDir, "notes.md")
	if err := os.WriteFile(wiki, []byte("wiki v1\n"), 0o644); err != nil {
		t.Fatalf("write wiki: %v", err)
	}
	if err := os.WriteFile(notes, []byte("notes v1\n"), 0o644); err != nil {
		t.Fatalf("write notes: %v", err)
	}
	if _, err := gitback.CommitAndPush(repoDir, "add docs"); err != nil {
		t.Fatalf("CommitAndPush docs: %v", err)
	}

	// A teammate advances wiki.md on the remote.
	mate := filepath.Join(filepath.Dir(origin), "mate")
	if err := gitback.Clone(origin, mate); err != nil {
		t.Fatalf("Clone mate: %v", err)
	}
	if err := os.WriteFile(filepath.Join(mate, "wiki.md"), []byte("wiki v2\n"), 0o644); err != nil {
		t.Fatalf("write mate wiki: %v", err)
	}
	remoteSHA, err := gitback.CommitAndPush(mate, "remote wiki update")
	if err != nil {
		t.Fatalf("CommitAndPush mate: %v", err)
	}

	// Poison window: an uncommitted write to a tracked file in the shared clone.
	if err := os.WriteFile(notes, []byte("notes: synthesis in flight\n"), 0o644); err != nil {
		t.Fatalf("poison notes: %v", err)
	}

	baseline, err := gitback.HeadSHA(repoDir)
	if err != nil {
		t.Fatalf("HeadSHA baseline: %v", err)
	}
	_ = pullTick(repoDir) // pullLoop swallows errors; the invariant must hold regardless

	head, err := gitback.HeadSHA(repoDir)
	if err != nil {
		t.Fatalf("HeadSHA after dirty tick: %v", err)
	}
	if head != baseline {
		t.Fatalf("dirty tick moved HEAD %s -> %s while the worktree was uncommitted: ref diverged from files", baseline[:8], head[:8])
	}
	if got, _ := os.ReadFile(wiki); string(got) != "wiki v1\n" {
		t.Fatalf("dirty tick changed wiki.md = %q, want untouched v1", got)
	}
	if got, _ := os.ReadFile(notes); string(got) != "notes: synthesis in flight\n" {
		t.Fatalf("dirty tick clobbered the uncommitted write: notes.md = %q", got)
	}

	// Poison gone: the next tick must fast-forward ref AND files together.
	if err := os.WriteFile(notes, []byte("notes v1\n"), 0o644); err != nil {
		t.Fatalf("restore notes: %v", err)
	}
	if err := pullTick(repoDir); err != nil {
		t.Fatalf("pullTick (clean): %v", err)
	}
	head, err = gitback.HeadSHA(repoDir)
	if err != nil {
		t.Fatalf("HeadSHA after clean tick: %v", err)
	}
	if head != remoteSHA {
		t.Fatalf("clean tick HEAD = %s, want remote %s", head[:8], remoteSHA[:8])
	}
	if got, _ := os.ReadFile(wiki); string(got) != "wiki v2\n" {
		t.Fatalf("clean tick HEAD advanced but wiki.md = %q, want v2", got)
	}
}

// TestPullTickKeepsLocalAheadCommits guards the tick's reset against the
// offline case: a committed-but-unpushed local advance (push failed) must never
// be discarded by a reset to the remote-tracking head.
func TestPullTickKeepsLocalAheadCommits(t *testing.T) {
	repoDir, _, _ := newGitBackendNoClean(t)

	seedFeature(t, repoDir, "F-0002", "Feature B", domain.StateCandidate)
	localSHA, err := gitback.CommitAll(repoDir, "local commit, push pending")
	if err != nil {
		t.Fatalf("CommitAll: %v", err)
	}

	if err := pullTick(repoDir); err != nil {
		t.Fatalf("pullTick: %v", err)
	}
	head, err := gitback.HeadSHA(repoDir)
	if err != nil {
		t.Fatalf("HeadSHA: %v", err)
	}
	if head != localSHA {
		t.Fatalf("tick discarded the unpushed local commit: HEAD = %s, want %s", head[:8], localSHA[:8])
	}
}

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
