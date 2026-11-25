package app

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/cost"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/project"
)

// TestNoticeAppendAndTake (F-0504 crit. 5): the notices of a session come out
// once, in order, ahead of nothing else, and the file is gone afterwards.
func TestNoticeAppendAndTake(t *testing.T) {
	cacheDir := t.TempDir()
	now := time.Date(2026, 11, 24, 21, 0, 0, 0, time.UTC)
	for _, line := range []string{"F-0001: candidate → approved by local", "F-0002: implemented → done by local"} {
		if err := appendNotice(cacheDir, "sess-a", notice{Entity: line[:6], Line: line, At: now}); err != nil {
			t.Fatalf("appendNotice: %v", err)
		}
	}
	got := TakeNotices(cacheDir, "sess-a", now.Add(time.Minute))
	want := "cerne: card state changed since this session last looked:\n" +
		"- F-0001: candidate → approved by local\n" +
		"- F-0002: implemented → done by local\n"
	if got != want {
		t.Fatalf("TakeNotices =\n%q\nwant\n%q", got, want)
	}
	if again := TakeNotices(cacheDir, "sess-a", now.Add(time.Minute)); again != "" {
		t.Fatalf("second TakeNotices = %q, want empty (shown once)", again)
	}
	if left, _ := filepath.Glob(filepath.Join(cacheDir, "notices", "*")); len(left) != 0 {
		t.Fatalf("notices left behind: %v", left)
	}
	if other := TakeNotices(cacheDir, "sess-b", now); other != "" {
		t.Fatalf("TakeNotices of another session = %q, want empty", other)
	}
}

// TestNoticeTakeDropsOld (F-0504 crit. 5): a notice older than the channel TTL
// is deleted without being shown.
func TestNoticeTakeDropsOld(t *testing.T) {
	cacheDir := t.TempDir()
	now := time.Date(2026, 11, 24, 21, 0, 0, 0, time.UTC)
	if err := appendNotice(cacheDir, "s", notice{Entity: "F-0001", Line: "F-0001: old", At: now.Add(-25 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := appendNotice(cacheDir, "s", notice{Entity: "F-0002", Line: "F-0002: fresh", At: now.Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	got := TakeNotices(cacheDir, "s", now)
	if strings.Contains(got, "old") || !strings.Contains(got, "F-0002: fresh") {
		t.Fatalf("TakeNotices = %q, want only the fresh notice", got)
	}
	if err := appendNotice(cacheDir, "s", notice{Entity: "F-0001", Line: "F-0001: old", At: now.Add(-25 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if got := TakeNotices(cacheDir, "s", now); got != "" {
		t.Fatalf("TakeNotices with only an old notice = %q, want empty", got)
	}
	if _, err := os.Stat(filepath.Join(cacheDir, "notices", "s.ndjson")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old notice file still there: %v", err)
	}
}

// TestNoticeTakeConcurrent (F-0504 crit. 5): two contact points at once — a
// PreToolUse and a PostToolUse — show each notice once in total.
func TestNoticeTakeConcurrent(t *testing.T) {
	cacheDir := t.TempDir()
	now := time.Now().UTC()
	for range 20 {
		if err := appendNotice(cacheDir, "s", notice{Entity: "F-0001", Line: "F-0001: x", At: now}); err != nil {
			t.Fatal(err)
		}
	}
	var mu sync.Mutex
	var all strings.Builder
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got := TakeNotices(cacheDir, "s", now)
			mu.Lock()
			all.WriteString(got)
			mu.Unlock()
		}()
	}
	wg.Wait()
	if n := strings.Count(all.String(), "- F-0001: x\n"); n != 20 {
		t.Fatalf("notices shown %d times in total, want 20", n)
	}
}

// TestNoticeUnsafeSession (F-0504 crit. 5): a session id that could escape the
// notices dir is refused on write and reads as nothing.
func TestNoticeUnsafeSession(t *testing.T) {
	cacheDir := t.TempDir()
	for _, sess := range []string{"", "../x", "a/b", "s.agent"} {
		if err := appendNotice(cacheDir, sess, notice{Entity: "F-0001", Line: "x", At: time.Now()}); !errors.Is(err, ErrUnsafeSessionID) {
			t.Fatalf("appendNotice(%q) = %v, want ErrUnsafeSessionID", sess, err)
		}
		if got := TakeNotices(cacheDir, sess, time.Now()); got != "" {
			t.Fatalf("TakeNotices(%q) = %q, want empty", sess, got)
		}
	}
}

// noticeProject wires a test backend to a cache of its own (CERNE_CACHE_DIR)
// and marks sessions as having worked on card id, as the hooks would.
func noticeProject(t *testing.T, id string, sessions ...string) (repoDir, projDir, cacheDir string) {
	t.Helper()
	repoDir, projDir, _ = newFeatureBackend(t)
	root := t.TempDir()
	t.Setenv("CERNE_CACHE_DIR", root)
	t.Setenv("CERNE_SESSION", "")
	if err := project.SaveConfig(projDir, project.Config{ProjectID: "p"}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	cacheDir = filepath.Join(root, "p")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, sess := range sessions {
		if err := cost.AppendMark(cacheDir, cost.Bind{Session: sess, Entity: id, Role: RoleOpened, Harness: cost.HarnessClaudeCode, TS: time.Now().UTC()}); err != nil {
			t.Fatalf("AppendMark: %v", err)
		}
	}
	return repoDir, projDir, cacheDir
}

// TestTransitionNotifiesCardSessions (F-0504 crit. 1, 3): the five human
// moves leave a notice for every session that worked on the card; the agent's
// own start and complete leave none; a card nobody worked on writes nothing.
func TestTransitionNotifiesCardSessions(t *testing.T) {
	cases := []struct {
		action     domain.Action
		from       domain.FeatureState
		docs       []string
		by, reason string
		notify     bool
	}{
		{domain.ActionApprove, domain.StateCandidate, []string{"spec.md"}, "", "", true},
		{domain.ActionAccept, domain.StateImplemented, nil, "", "", true},
		{domain.ActionReject, domain.StateCandidate, nil, "", "não vai ser feito", true},
		{domain.ActionRevert, domain.StateApproved, []string{"spec.md"}, "", "volta para a spec", true},
		{domain.ActionSupersede, domain.StateInDevelopment, nil, "F-0099", "a F-0099 faz melhor", true},
		{domain.ActionStart, domain.StateApproved, []string{"spec.md", "plan.md"}, "", "", false},
	}
	for _, c := range cases {
		t.Run(string(c.action), func(t *testing.T) {
			repoDir, projDir, cacheDir := noticeProject(t, "F-0001", "sess-a", "sess-b")
			seedFeatureInState(t, repoDir, "F-0001", c.from, c.docs...)
			if c.by != "" {
				seedFeatureInState(t, repoDir, c.by, domain.StateCandidate)
			}
			if _, err := TransitionFeature(repoDir, projDir, "F-0001", c.action, c.by, c.reason, nil); err != nil {
				t.Fatalf("%s: %v", c.action, err)
			}
			for _, sess := range []string{"sess-a", "sess-b"} {
				got := TakeNotices(cacheDir, sess, time.Now().UTC())
				if c.notify != strings.Contains(got, "F-0001: "+string(c.from)+" → ") {
					t.Fatalf("%s notice for %s = %q, want notify=%v", c.action, sess, got, c.notify)
				}
			}
		})
	}

	t.Run("actor session left out", func(t *testing.T) {
		repoDir, projDir, cacheDir := noticeProject(t, "F-0001", "sess-a", "sess-b")
		seedFeatureInState(t, repoDir, "F-0001", domain.StateCandidate, "spec.md")
		t.Setenv("CERNE_SESSION", "sess-a")
		if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionApprove, "", "", nil); err != nil {
			t.Fatal(err)
		}
		if got := TakeNotices(cacheDir, "sess-a", time.Now().UTC()); got != "" {
			t.Fatalf("the actor's own session got %q", got)
		}
		if got := TakeNotices(cacheDir, "sess-b", time.Now().UTC()); got == "" {
			t.Fatal("the other session got no notice")
		}
	})

	t.Run("no session", func(t *testing.T) {
		repoDir, projDir, cacheDir := noticeProject(t, "F-0001")
		seedFeatureInState(t, repoDir, "F-0001", domain.StateCandidate, "spec.md")
		if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionApprove, "", "", nil); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(cacheDir, noticesDir)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("notices dir created for a card nobody worked on: %v", err)
		}
	})
}

// TestTransitionNoticeLine (F-0504 crit. 2): the notice reads like the wait's
// line — the move, who made it and when, and the next step with its command.
func TestTransitionNoticeLine(t *testing.T) {
	cases := []struct {
		action domain.Action
		from   domain.FeatureState
		docs   []string
		reason string
		to     domain.FeatureState
	}{
		{domain.ActionApprove, domain.StateCandidate, []string{"spec.md"}, "", domain.StateApproved},
		{domain.ActionAccept, domain.StateImplemented, nil, "", domain.StateDone},
		{domain.ActionReject, domain.StateCandidate, nil, "fora do foco", domain.StateRejected},
	}
	for _, c := range cases {
		t.Run(string(c.to), func(t *testing.T) {
			repoDir, projDir, cacheDir := noticeProject(t, "F-0001", "sess-a")
			seedFeatureInState(t, repoDir, "F-0001", c.from, c.docs...)
			if _, err := TransitionFeature(repoDir, projDir, "F-0001", c.action, "", c.reason, nil); err != nil {
				t.Fatal(err)
			}
			got := TakeNotices(cacheDir, "sess-a", time.Now().UTC())
			want := "- F-0001: " + string(c.from) + " → " + string(c.to) + " by " + EventActor() + " at "
			if !strings.Contains(got, want) {
				t.Fatalf("notice = %q, want it to contain %q", got, want)
			}
			if tail := NextStepTail(repoDir, projDir, "F-0001"); tail == "" || !strings.Contains(got, tail+"\n") {
				t.Fatalf("notice = %q, want it to end with the next step %q", got, tail)
			}
		})
	}
}

// TestTransitionNoticeFailureWarns (F-0504 crit. 4): a notice that cannot be
// written is a warning; the transition still succeeds.
func TestTransitionNoticeFailureWarns(t *testing.T) {
	repoDir, projDir, cacheDir := noticeProject(t, "F-0001", "sess-a")
	seedFeatureInState(t, repoDir, "F-0001", domain.StateCandidate, "spec.md")
	if err := os.WriteFile(filepath.Join(cacheDir, noticesDir), []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionApprove, "", "", nil)
	if err != nil {
		t.Fatalf("TransitionFeature = %v, want success despite the notice", err)
	}
	if out.To != domain.StateApproved {
		t.Fatalf("outcome = %+v, want approved", out)
	}
	if !slices.ContainsFunc(out.Warnings, func(w string) bool { return strings.Contains(w, "card notice") }) {
		t.Fatalf("warnings = %q, want a card notice warning", out.Warnings)
	}
}

// TestTransitionNoticeLeavesChannel (F-0504 crit. 6): the notice never enters
// the project channel, so no wait wakes on it and no UI thread shows it.
func TestTransitionNoticeLeavesChannel(t *testing.T) {
	repoDir, projDir, cacheDir := noticeProject(t, "F-0001", "sess-a")
	seedFeatureInState(t, repoDir, "F-0001", domain.StateCandidate, "spec.md")
	if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionApprove, "", "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(cacheDir, "rooms")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the transition wrote to the channel: %v", err)
	}
	if wake, err := ChannelWake(cacheDir, repoDir, "sess-a", time.Now().UTC()); err != nil || wake != "" {
		t.Fatalf("ChannelWake = %q, %v; want nothing", wake, err)
	}
}

// TestNoticeTakeLongLine (F-0504 crit. 5): a notice longer than a scanner's
// 64 KiB token still comes out, and so does the one after it.
func TestNoticeTakeLongLine(t *testing.T) {
	cacheDir := t.TempDir()
	now := time.Now().UTC()
	long := "F-0001: " + strings.Repeat("x", 70<<10)
	for _, line := range []string{long, "F-0002: after"} {
		if err := appendNotice(cacheDir, "s", notice{Entity: line[:6], Line: line, At: now}); err != nil {
			t.Fatal(err)
		}
	}
	got := TakeNotices(cacheDir, "s", now)
	if !strings.Contains(got, long) || !strings.Contains(got, "F-0002: after") {
		t.Fatalf("TakeNotices dropped a notice (%d bytes back)", len(got))
	}
}
