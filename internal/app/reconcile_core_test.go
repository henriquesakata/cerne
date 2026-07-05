package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/project"
)

// reconcileFixture builds a bare origin, a shared backend clone (repoDir) seeded
// with content/features/F-0001/spec.md, and a projDir whose docs.lock is pinned
// to that seed (base). autocommit_lock is off, so projDir need not be a git repo.
func reconcileFixture(t *testing.T) (origin, repoDir, projDir, base string) {
	t.Helper()
	root := t.TempDir()
	origin = filepath.Join(root, "origin.git")
	if err := gitback.InitBare(origin); err != nil {
		t.Fatalf("InitBare: %v", err)
	}
	repoDir = filepath.Join(root, "repo")
	if err := gitback.Clone(origin, repoDir); err != nil {
		t.Fatalf("Clone: %v", err)
	}
	writeBackendFile(t, repoDir, "content/features/F-0001/spec.md", "base spec\n")
	sha, err := gitback.CommitAndPush(repoDir, "seed")
	if err != nil {
		t.Fatalf("seed CommitAndPush: %v", err)
	}
	base = sha
	projDir = filepath.Join(root, "proj")
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatalf("mkdir proj: %v", err)
	}
	if err := project.SaveConfig(projDir, project.Config{ProjectID: "demo", Backend: origin}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	if err := project.SaveLock(projDir, project.Lock{Version: 1, Backend: origin, Snapshot: base}); err != nil {
		t.Fatalf("SaveLock: %v", err)
	}
	return origin, repoDir, projDir, base
}

func writeBackendFile(t *testing.T, repoDir, rel, content string) {
	t.Helper()
	p := filepath.Join(repoDir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

// advanceRemote pushes content at rel from an independent clone, simulating
// another agent advancing origin. Returns the new origin head.
func advanceRemote(t *testing.T, origin, rel, content string) string {
	t.Helper()
	other := filepath.Join(t.TempDir(), "other")
	if err := gitback.Clone(origin, other); err != nil {
		t.Fatalf("advanceRemote Clone: %v", err)
	}
	writeBackendFile(t, other, rel, content)
	sha, err := gitback.CommitAndPush(other, "advance "+rel)
	if err != nil {
		t.Fatalf("advanceRemote CommitAndPush: %v", err)
	}
	return sha
}

// originHas reports the content of rel in a fresh clone of origin (and whether
// it exists), proving what actually landed on the remote.
func originHas(t *testing.T, origin, rel string) (string, bool) {
	t.Helper()
	verify := filepath.Join(t.TempDir(), "verify")
	if err := gitback.Clone(origin, verify); err != nil {
		t.Fatalf("verify Clone: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(verify, filepath.FromSlash(rel)))
	if errors.Is(err, os.ErrNotExist) {
		return "", false
	}
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(data), true
}

// C1: disjoint documents both land — my net-new result.md merges over a remote
// that added a different document, no conflict, no force.
func TestReconcileDisjointBothLand(t *testing.T) {
	origin, repoDir, projDir, base := reconcileFixture(t)
	advanceRemote(t, origin, "content/notes/N-0001.md", "their note\n")

	changes := []FileChange{{Path: "content/features/F-0001/result.md", Content: []byte("my result\n")}}
	head, conflicts, err := ReconcileCommitPush(repoDir, projDir, base, "push result", changes)
	if err != nil {
		t.Fatalf("ReconcileCommitPush: %v", err)
	}
	if len(conflicts) != 0 {
		t.Fatalf("unexpected conflicts: %+v", conflicts)
	}
	if head == "" {
		t.Fatal("expected a pushed head")
	}
	if got, ok := originHas(t, origin, "content/features/F-0001/result.md"); !ok || got != "my result\n" {
		t.Fatalf("my result on origin = %q ok=%v, want %q", got, ok, "my result\n")
	}
	if got, ok := originHas(t, origin, "content/notes/N-0001.md"); !ok || got != "their note\n" {
		t.Fatalf("their note on origin = %q ok=%v, want preserved", got, ok)
	}
	if snap := loadSnapshot(t, projDir); snap != head {
		t.Fatalf("docs.lock snapshot = %q, want pushed head %q", snap, head)
	}
}

// C3/C7: same document, both sides diverged → conflict with a non-empty diff,
// nothing pushed (the remote keeps its version).
func TestReconcileSameDocConflict(t *testing.T) {
	origin, repoDir, projDir, base := reconcileFixture(t)
	theirHead := advanceRemote(t, origin, "content/features/F-0001/spec.md", "their spec\n")

	changes := []FileChange{{Path: "content/features/F-0001/spec.md", Content: []byte("my spec\n")}}
	head, conflicts, err := ReconcileCommitPush(repoDir, projDir, base, "push spec", changes)
	if err != nil {
		t.Fatalf("ReconcileCommitPush: %v", err)
	}
	if head != "" {
		t.Fatalf("head = %q, want empty (nothing pushed on conflict)", head)
	}
	if len(conflicts) != 1 || conflicts[0].Path != "content/features/F-0001/spec.md" {
		t.Fatalf("conflicts = %+v, want one for spec.md", conflicts)
	}
	if !strings.Contains(conflicts[0].Diff, "their spec") || !strings.Contains(conflicts[0].Diff, "my spec") {
		t.Fatalf("diff missing both sides:\n%s", conflicts[0].Diff)
	}
	// Remote untouched: it still holds their version, mine was not pushed.
	if got, _ := originHas(t, origin, "content/features/F-0001/spec.md"); got != "their spec\n" {
		t.Fatalf("origin spec = %q, want their version (no silent overwrite)", got)
	}
	if rh, _ := gitback.RemoteHeadSHA(repoDir); rh != theirHead {
		t.Fatalf("remote head = %q, want unchanged %q", rh, theirHead)
	}
}

// C5: a concurrent advance lands between fetch and push (first attempt), so the
// push is non-fast-forward; the retry re-fetches and succeeds.
func TestReconcileRetriesOnConcurrentAdvance(t *testing.T) {
	origin, repoDir, projDir, base := reconcileFixture(t)
	defer swapReconcileTiming(t, 4, time.Millisecond)()

	var calls int
	reconcileHookBeforePush = func(attempt int) {
		calls++
		if attempt == 0 {
			advanceRemote(t, origin, "content/notes/RACE.md", "raced in\n")
		}
	}
	defer func() { reconcileHookBeforePush = nil }()

	changes := []FileChange{{Path: "content/features/F-0001/result.md", Content: []byte("my result\n")}}
	head, conflicts, err := ReconcileCommitPush(repoDir, projDir, base, "push result", changes)
	if err != nil {
		t.Fatalf("ReconcileCommitPush: %v", err)
	}
	if len(conflicts) != 0 || head == "" {
		t.Fatalf("head=%q conflicts=%+v, want clean success", head, conflicts)
	}
	if calls < 2 {
		t.Fatalf("push attempted %d time(s), want a retry (>=2)", calls)
	}
	if got, ok := originHas(t, origin, "content/features/F-0001/result.md"); !ok || got != "my result\n" {
		t.Fatalf("my result not on origin after retry: %q ok=%v", got, ok)
	}
	if got, ok := originHas(t, origin, "content/notes/RACE.md"); !ok || got != "raced in\n" {
		t.Fatalf("raced-in change lost: %q ok=%v", got, ok)
	}
}

// C5: when every attempt loses the race, the reconcile gives up with
// ErrTooConcurrent rather than spinning forever — and pushes nothing.
func TestReconcileTooConcurrent(t *testing.T) {
	origin, repoDir, projDir, base := reconcileFixture(t)
	defer swapReconcileTiming(t, 3, time.Millisecond)()

	var n int
	reconcileHookBeforePush = func(attempt int) {
		n++
		advanceRemote(t, origin, "content/notes/r"+strconv.Itoa(n)+".md", "x\n") // unique each time
	}
	defer func() { reconcileHookBeforePush = nil }()

	changes := []FileChange{{Path: "content/features/F-0001/result.md", Content: []byte("my result\n")}}
	_, _, err := ReconcileCommitPush(repoDir, projDir, base, "push result", changes)
	if !errors.Is(err, ErrTooConcurrent) {
		t.Fatalf("err = %v, want ErrTooConcurrent", err)
	}
}

// Append-only event logs union-merge rather than conflict: concurrent appends
// from both sides survive.
func TestReconcileAppendOnlyUnion(t *testing.T) {
	origin, repoDir, projDir, _ := reconcileFixture(t)
	// Seed the events file at base.
	writeBackendFile(t, repoDir, "events/2026-06.ndjson", "e0\n")
	base2, err := gitback.CommitAndPush(repoDir, "seed events")
	if err != nil {
		t.Fatalf("seed events: %v", err)
	}
	if err := project.SaveLock(projDir, project.Lock{Version: 1, Backend: origin, Snapshot: base2}); err != nil {
		t.Fatalf("SaveLock: %v", err)
	}
	// Remote appends "remote"; I append "mine" (append-only, both over e0).
	advanceRemote(t, origin, "events/2026-06.ndjson", "e0\nremote\n")
	changes := []FileChange{{Path: "events/2026-06.ndjson", Content: []byte("e0\nmine\n")}}

	head, conflicts, err := ReconcileCommitPush(repoDir, projDir, base2, "append mine", changes)
	if err != nil || len(conflicts) != 0 || head == "" {
		t.Fatalf("reconcile events: head=%q conflicts=%+v err=%v", head, conflicts, err)
	}
	got, _ := originHas(t, origin, "events/2026-06.ndjson")
	for _, want := range []string{"e0", "remote", "mine"} {
		if !strings.Contains(got, want) {
			t.Fatalf("merged events %q missing %q", got, want)
		}
	}
}

// C1, under REAL concurrency: two goroutines push disjoint documents to the
// SAME shared backend clone at the same time. The lock serializes their git
// operations, so both land with no conflict and no corruption — exactly the
// two-agents-same-project scenario the fix targets.
func TestReconcileConcurrentDisjointBothLand(t *testing.T) {
	origin, repoDir, projDir, base := reconcileFixture(t)
	defer swapReconcileTiming(t, 8, time.Millisecond)()

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	push := func(path, content string) {
		defer wg.Done()
		_, conflicts, err := ReconcileCommitPush(repoDir, projDir, base, "concurrent "+path,
			[]FileChange{{Path: path, Content: []byte(content)}})
		switch {
		case err != nil:
			errs <- err
		case len(conflicts) > 0:
			errs <- fmt.Errorf("unexpected conflict on %s: %+v", path, conflicts)
		default:
			errs <- nil
		}
	}
	wg.Add(2)
	go push("content/features/F-0001/result.md", "result A\n")
	go push("content/notes/N-0001.md", "note B\n")
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent disjoint push: %v", err)
		}
	}

	if got, ok := originHas(t, origin, "content/features/F-0001/result.md"); !ok || got != "result A\n" {
		t.Fatalf("result A missing on origin: %q ok=%v", got, ok)
	}
	if got, ok := originHas(t, origin, "content/notes/N-0001.md"); !ok || got != "note B\n" {
		t.Fatalf("note B missing on origin: %q ok=%v", got, ok)
	}
}

func loadSnapshot(t *testing.T, projDir string) string {
	t.Helper()
	lock, err := project.LoadLock(projDir)
	if err != nil {
		t.Fatalf("LoadLock: %v", err)
	}
	return lock.Snapshot
}

func swapReconcileTiming(t *testing.T, attempts int, backoff time.Duration) func() {
	t.Helper()
	a0, b0 := reconcileMaxAttempts, reconcileBackoff
	reconcileMaxAttempts, reconcileBackoff = attempts, backoff
	return func() { reconcileMaxAttempts, reconcileBackoff = a0, b0 }
}

// F-0121 crit. 1: the reconcile commit covers exactly the applied documents. A
// stranger file landing in the shared worktree in the write→commit window (a
// concurrent process's half-written state) must not be swept into the pushed
// commit — it stays untracked, owned by whoever wrote it.
func TestReconcileCommitsOnlyAppliedPaths(t *testing.T) {
	origin, repoDir, projDir, base := reconcileFixture(t)
	reconcileHookAfterWrite = func() {
		writeBackendFile(t, repoDir, "entities/features/F-0099.json", "{\"id\":\"F-0099\"}\n")
	}
	defer func() { reconcileHookAfterWrite = nil }()

	changes := []FileChange{{Path: "content/features/F-0002/spec.md", Content: []byte("mine\n")}}
	head, conflicts, err := ReconcileCommitPush(repoDir, projDir, base, "create F-0002", changes)
	if err != nil {
		t.Fatalf("ReconcileCommitPush: %v", err)
	}
	if len(conflicts) != 0 {
		t.Fatalf("unexpected conflicts: %+v", conflicts)
	}
	if head == "" {
		t.Fatal("expected a pushed head")
	}
	if got, ok := originHas(t, origin, "content/features/F-0002/spec.md"); !ok || got != "mine\n" {
		t.Fatalf("my spec on origin = %q ok=%v, want %q", got, ok, "mine\n")
	}
	if _, ok := originHas(t, origin, "entities/features/F-0099.json"); ok {
		t.Fatal("stranger file was swept into the pushed commit")
	}
}
