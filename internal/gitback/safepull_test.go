package gitback

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// safePullFixture creates a bare origin, a working clone of it with one pushed
// commit (wiki.md + notes.md), and returns both paths.
func safePullFixture(t *testing.T) (repoDir, origin string) {
	t.Helper()
	base := t.TempDir()
	origin = filepath.Join(base, "origin.git")
	if err := InitBare(origin); err != nil {
		t.Fatalf("InitBare: %v", err)
	}
	repoDir = filepath.Join(base, "repo")
	if err := Clone(origin, repoDir); err != nil {
		t.Fatalf("Clone: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "wiki.md"), []byte("wiki v1\n"), 0o644); err != nil {
		t.Fatalf("write wiki: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "notes.md"), []byte("notes v1\n"), 0o644); err != nil {
		t.Fatalf("write notes: %v", err)
	}
	if _, err := CommitAndPush(repoDir, "seed"); err != nil {
		t.Fatalf("CommitAndPush seed: %v", err)
	}
	// The seed push marks the sync (F-0156/F-0200), which would let SafePull's
	// fast path elide. The tests below are about the full path; the ones about
	// the fast path mark it themselves, deliberately.
	resetSyncMarks()
	return repoDir, origin
}

// advanceRemote clones origin into a sibling dir, rewrites relPath there and
// pushes, returning the new remote head.
func advanceRemote(t *testing.T, origin, relPath, content string) string {
	t.Helper()
	mate := filepath.Join(filepath.Dir(origin), "mate")
	if err := Clone(origin, mate); err != nil {
		t.Fatalf("Clone mate: %v", err)
	}
	if err := os.WriteFile(filepath.Join(mate, relPath), []byte(content), 0o644); err != nil {
		t.Fatalf("write mate %s: %v", relPath, err)
	}
	sha, err := CommitAndPush(mate, "remote update")
	if err != nil {
		t.Fatalf("CommitAndPush mate: %v", err)
	}
	return sha
}

// TestSafePullDirtyWorktreeNeverDivergesRefFromFiles pins the F-0192/N-0027
// poison: go-git's Worktree.Pull moves refs/heads/main to the fetched head
// BEFORE its MergeReset, and MergeReset aborts with ErrUnstagedChanges when the
// shared clone has uncommitted writes (e.g. a CLI compile's LLM synthesis in
// flight). The branch ref then points at the new head while index and files
// stay on the old tree — a poisoned clone whose next sweep commit reverts the
// remote advance. The invariant: SafePull either moves ref AND files together,
// or moves nothing; uncommitted writes survive untouched.
func TestSafePullDirtyWorktreeNeverDivergesRefFromFiles(t *testing.T) {
	repoDir, origin := safePullFixture(t)
	remoteSHA := advanceRemote(t, origin, "wiki.md", "wiki v2\n")

	// Poison window: an uncommitted write to a tracked file in the shared clone.
	notes := filepath.Join(repoDir, "notes.md")
	if err := os.WriteFile(notes, []byte("notes: synthesis in flight\n"), 0o644); err != nil {
		t.Fatalf("poison notes: %v", err)
	}

	baseline, err := HeadSHA(repoDir)
	if err != nil {
		t.Fatalf("HeadSHA baseline: %v", err)
	}
	if err := SafePull(repoDir); err != nil {
		t.Fatalf("SafePull (dirty): %v", err)
	}

	head, err := HeadSHA(repoDir)
	if err != nil {
		t.Fatalf("HeadSHA after dirty pull: %v", err)
	}
	if head != baseline {
		t.Fatalf("dirty pull moved HEAD %s -> %s while the worktree was uncommitted: ref diverged from files", baseline[:8], head[:8])
	}
	if got, _ := os.ReadFile(filepath.Join(repoDir, "wiki.md")); string(got) != "wiki v1\n" {
		t.Fatalf("dirty pull changed wiki.md = %q, want untouched v1", got)
	}
	if got, _ := os.ReadFile(notes); string(got) != "notes: synthesis in flight\n" {
		t.Fatalf("dirty pull clobbered the uncommitted write: notes.md = %q", got)
	}

	// Poison gone: the next pull must fast-forward ref AND files together.
	if err := os.WriteFile(notes, []byte("notes v1\n"), 0o644); err != nil {
		t.Fatalf("restore notes: %v", err)
	}
	if err := SafePull(repoDir); err != nil {
		t.Fatalf("SafePull (clean): %v", err)
	}
	head, err = HeadSHA(repoDir)
	if err != nil {
		t.Fatalf("HeadSHA after clean pull: %v", err)
	}
	if head != remoteSHA {
		t.Fatalf("clean pull HEAD = %s, want remote %s", head[:8], remoteSHA[:8])
	}
	if got, _ := os.ReadFile(filepath.Join(repoDir, "wiki.md")); string(got) != "wiki v2\n" {
		t.Fatalf("clean pull HEAD advanced but wiki.md = %q, want v2", got)
	}
}

// TestSafePullEmptyRemoteIsNoOp: a clone of a still-empty backend (Clone
// tolerates ErrEmptyRemoteRepository — e.g. an init that crashed before its
// first push) has no tracking ref. SafePull must treat that as "nothing to
// pull" and return nil, so the caller proceeds and seeds the backend, instead
// of dead-ending on ErrNoCommits.
func TestSafePullEmptyRemoteIsNoOp(t *testing.T) {
	base := t.TempDir()
	origin := filepath.Join(base, "origin.git")
	if err := InitBare(origin); err != nil {
		t.Fatalf("InitBare: %v", err)
	}
	repoDir := filepath.Join(base, "repo")
	if err := Clone(origin, repoDir); err != nil {
		t.Fatalf("Clone: %v", err)
	}
	if err := SafePull(repoDir); err != nil {
		t.Fatalf("SafePull on an empty-remote clone = %v, want nil", err)
	}
}

// TestSafePullKeepsLocalAheadCommits guards the reset against the offline case:
// a committed-but-unpushed local advance (push failed) must never be discarded
// by a reset to the remote-tracking head.
func TestSafePullKeepsLocalAheadCommits(t *testing.T) {
	repoDir, _ := safePullFixture(t)

	if err := os.WriteFile(filepath.Join(repoDir, "notes.md"), []byte("notes v2, push pending\n"), 0o644); err != nil {
		t.Fatalf("write notes: %v", err)
	}
	localSHA, err := CommitAll(repoDir, "local commit, push pending")
	if err != nil {
		t.Fatalf("CommitAll: %v", err)
	}

	if err := SafePull(repoDir); err != nil {
		t.Fatalf("SafePull: %v", err)
	}
	head, err := HeadSHA(repoDir)
	if err != nil {
		t.Fatalf("HeadSHA: %v", err)
	}
	if head != localSHA {
		t.Fatalf("SafePull discarded the unpushed local commit: HEAD = %s, want %s", head[:8], localSHA[:8])
	}
}

// removeOrigin makes any real fetch fail. It is the discriminator these tests
// use: a SafePull that still returns nil demonstrably never talked to the
// remote — no test hook or counter needed.
func removeOrigin(t *testing.T, origin string) {
	t.Helper()
	if err := os.RemoveAll(origin); err != nil {
		t.Fatalf("RemoveAll origin: %v", err)
	}
}

// TestSafePullElidesOnFreshMarkAtTracking pins criterion 1 (F-0200): a fresh
// sync mark plus a clone already sitting on the tracking head means there is
// nothing a fetch could teach us, so SafePull returns without one. Proven by
// deleting origin first: the full path would reach Fetch and fail.
func TestSafePullElidesOnFreshMarkAtTracking(t *testing.T) {
	withFakeClock(t)
	repoDir, origin := safePullFixture(t)
	if err := Fetch(repoDir); err != nil { // tracking ref set, mark stamped
		t.Fatalf("Fetch: %v", err)
	}
	removeOrigin(t, origin)

	if err := SafePull(repoDir); err != nil {
		t.Fatalf("SafePull under a fresh mark = %v, want nil (it must not fetch)", err)
	}
}

// TestSafePullFetchesWithoutMark pins criterion 2: no mark — every CLI process
// before this feature, and any process whose window lapsed — keeps today's
// behaviour, fetching and adopting the remote advance.
func TestSafePullFetchesWithoutMark(t *testing.T) {
	withFakeClock(t)
	repoDir, origin := safePullFixture(t)
	remoteSHA := advanceRemote(t, origin, "wiki.md", "wiki v2\n")

	if err := SafePull(repoDir); err != nil {
		t.Fatalf("SafePull: %v", err)
	}
	head, err := HeadSHA(repoDir)
	if err != nil {
		t.Fatalf("HeadSHA: %v", err)
	}
	if head != remoteSHA {
		t.Fatalf("unmarked SafePull HEAD = %s, want remote %s — it must fetch", head[:8], remoteSHA[:8])
	}
}

// TestSafePullFetchesWhenMarkExpired pins the other half of criterion 2: the
// mark is a TTL, not a latch. Past the window the full path runs again.
func TestSafePullFetchesWhenMarkExpired(t *testing.T) {
	now := withFakeClock(t)
	repoDir, origin := safePullFixture(t)
	if err := Fetch(repoDir); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	remoteSHA := advanceRemote(t, origin, "wiki.md", "wiki v2\n")

	*now = now.Add(safePullWindow + time.Second)
	if err := SafePull(repoDir); err != nil {
		t.Fatalf("SafePull: %v", err)
	}
	head, err := HeadSHA(repoDir)
	if err != nil {
		t.Fatalf("HeadSHA: %v", err)
	}
	if head != remoteSHA {
		t.Fatalf("expired-mark SafePull HEAD = %s, want remote %s", head[:8], remoteSHA[:8])
	}
}

// TestSafePullFallsBackWhenTrackingAhead pins criterion 3: freshness alone is
// not enough. Another process may have fetched (advancing the tracking ref)
// and then skipped its own reset on a dirty worktree — the mark is fresh, yet
// this clone's files are behind. The guard must see head != tracking and take
// the full path so the fast-forward still happens.
func TestSafePullFallsBackWhenTrackingAhead(t *testing.T) {
	withFakeClock(t)
	repoDir, origin := safePullFixture(t)
	remoteSHA := advanceRemote(t, origin, "wiki.md", "wiki v2\n")
	if err := Fetch(repoDir); err != nil { // tracking now ahead of HEAD, mark fresh
		t.Fatalf("Fetch: %v", err)
	}

	if err := SafePull(repoDir); err != nil {
		t.Fatalf("SafePull: %v", err)
	}
	head, err := HeadSHA(repoDir)
	if err != nil {
		t.Fatalf("HeadSHA: %v", err)
	}
	if head != remoteSHA {
		t.Fatalf("HEAD = %s, want %s — a fresh mark must not skip a pending fast-forward", head[:8], remoteSHA[:8])
	}
	if got, _ := os.ReadFile(filepath.Join(repoDir, "wiki.md")); string(got) != "wiki v2\n" {
		t.Fatalf("wiki.md = %q, want v2 — ref moved without the files", got)
	}
}

// TestSafePullNowAlwaysFetches pins criterion 10 (F-0200): SafePull's two other
// callers must never elide. internal/server/live.go refreshes every 15s to keep
// the UI live — inheriting a 60s window would freeze it, the very regression
// F-0517 paid to fix — and cerne init is a one-shot where a skipped fetch buys
// nothing. Same fixture as the elision test, opposite expectation.
func TestSafePullNowAlwaysFetches(t *testing.T) {
	withFakeClock(t)
	repoDir, origin := safePullFixture(t)
	if err := Fetch(repoDir); err != nil { // fresh mark: SafePull would elide here
		t.Fatalf("Fetch: %v", err)
	}
	remoteSHA := advanceRemote(t, origin, "wiki.md", "wiki v2\n")

	if err := SafePullNow(repoDir); err != nil {
		t.Fatalf("SafePullNow: %v", err)
	}
	head, err := HeadSHA(repoDir)
	if err != nil {
		t.Fatalf("HeadSHA: %v", err)
	}
	if head != remoteSHA {
		t.Fatalf("SafePullNow HEAD = %s, want remote %s — it must ignore the mark", head[:8], remoteSHA[:8])
	}
}
