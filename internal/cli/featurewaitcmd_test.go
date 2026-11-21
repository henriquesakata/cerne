package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// fakeWait scripts the backend as seen by successive refreshes: states[i] is
// the feature's state after refresh i (the last one repeats); stale[i], when
// set, marks the approval stale at that refresh; errs[i] makes refresh i fail.
type fakeWait struct {
	states   []domain.FeatureState
	stale    map[int]bool
	errs     map[int]error
	loadErrs map[int]error
	calls    int
	slept    []time.Duration
	clock    time.Time
	warnings bytes.Buffer
}

func (f *fakeWait) env() waitEnv {
	return waitEnv{
		refresh: func(context.Context) error {
			f.calls++
			return f.errs[f.calls-1]
		},
		load: func() (store.Feature, error) {
			if err := f.loadErrs[f.calls-1]; err != nil {
				return store.Feature{}, err
			}
			i := min(f.calls-1, len(f.states)-1)
			return store.Feature{ID: "F-0001", State: f.states[i]}, nil
		},
		stale: func(store.Feature) bool { return f.stale[f.calls-1] },
		sleep: func(_ context.Context, d time.Duration) error {
			f.slept = append(f.slept, d)
			f.clock = f.clock.Add(d)
			return nil
		},
		now:  func() time.Time { return f.clock },
		warn: &f.warnings,
	}
}

func newFakeWait(states ...domain.FeatureState) *fakeWait {
	return &fakeWait{states: states, clock: time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)}
}

// Criterion 1: the transition seen on the third refresh ends the wait with 0,
// after two interval sleeps.
func TestFeatureWaitReachesMidLoop(t *testing.T) {
	f := newFakeWait(domain.StateCandidate, domain.StateCandidate, domain.StateApproved)
	r, err := waitFeature(context.Background(), f.env(), domain.StateApproved, 24*time.Hour, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if r.code != 0 || r.start.State != domain.StateCandidate || r.end.State != domain.StateApproved {
		t.Fatalf("result = %+v", r)
	}
	if f.calls != 3 || len(f.slept) != 2 || f.slept[0] != 30*time.Second {
		t.Fatalf("calls %d, slept %v", f.calls, f.slept)
	}
}

// Criterion 2: already there, or past it on the forward path: no sleep at all.
func TestFeatureWaitAlreadyThere(t *testing.T) {
	for _, st := range []domain.FeatureState{domain.StateApproved, domain.StateInDevelopment, domain.StateDone} {
		f := newFakeWait(st)
		r, err := waitFeature(context.Background(), f.env(), domain.StateApproved, 24*time.Hour, 30*time.Second)
		if err != nil || r.code != 0 || len(f.slept) != 0 {
			t.Fatalf("%s: r=%+v err=%v slept=%v", st, r, err, f.slept)
		}
	}
}

// Criterion 3: an approved feature whose approval went stale is not there yet;
// the re-approval (stale cleared) ends the wait.
func TestFeatureWaitStaleApproval(t *testing.T) {
	f := newFakeWait(domain.StateApproved)
	f.stale = map[int]bool{0: true, 1: true}
	r, err := waitFeature(context.Background(), f.env(), domain.StateApproved, 24*time.Hour, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if r.code != 0 || !r.startStale || f.calls != 3 {
		t.Fatalf("r=%+v calls=%d", r, f.calls)
	}
}

// Criterion 4: rejected while waiting for approved is unreachable: code 2.
func TestFeatureWaitUnreachable(t *testing.T) {
	f := newFakeWait(domain.StateCandidate, domain.StateRejected)
	r, err := waitFeature(context.Background(), f.env(), domain.StateApproved, 24*time.Hour, 30*time.Second)
	if err != nil || r.code != 2 || r.end.State != domain.StateRejected {
		t.Fatalf("r=%+v err=%v", r, err)
	}
	// Approved can still end rejected (revert, then reject): not unreachable.
	f = newFakeWait(domain.StateApproved)
	r, _ = waitFeature(context.Background(), f.env(), domain.StateRejected, time.Minute, 30*time.Second)
	if r.code != 3 {
		t.Fatalf("approved waiting for rejected: code %d, want 3 (timeout)", r.code)
	}
}

// Criterion 5: the timeout ends the wait with 3, never sleeping past it;
// --timeout 0 checks once.
func TestFeatureWaitTimeout(t *testing.T) {
	f := newFakeWait(domain.StateCandidate)
	r, err := waitFeature(context.Background(), f.env(), domain.StateApproved, 70*time.Second, 30*time.Second)
	if err != nil || r.code != 3 {
		t.Fatalf("r=%+v err=%v", r, err)
	}
	var total time.Duration
	for _, d := range f.slept {
		total += d
	}
	if total != 70*time.Second || r.waited != 70*time.Second {
		t.Fatalf("slept %v (total %v), waited %v", f.slept, total, r.waited)
	}
	f = newFakeWait(domain.StateCandidate)
	r, _ = waitFeature(context.Background(), f.env(), domain.StateApproved, 0, 30*time.Second)
	if r.code != 3 || f.calls != 1 || len(f.slept) != 0 {
		t.Fatalf("--timeout 0: r=%+v calls=%d slept=%v", r, f.calls, f.slept)
	}
}

// Criterion 6: a failed refresh warns, backs off (doubling, capped at 5 min)
// and resumes the normal interval after the first good read.
func TestFeatureWaitTransientError(t *testing.T) {
	boom := errors.New("network down")
	f := newFakeWait(domain.StateCandidate, domain.StateCandidate, domain.StateCandidate, domain.StateCandidate, domain.StateCandidate, domain.StateApproved)
	f.errs = map[int]error{0: boom, 1: boom, 2: boom}
	r, err := waitFeature(context.Background(), f.env(), domain.StateApproved, 24*time.Hour, 2*time.Minute)
	if err != nil || r.code != 0 {
		t.Fatalf("r=%+v err=%v", r, err)
	}
	want := []time.Duration{2 * time.Minute, 4 * time.Minute, 5 * time.Minute, 2 * time.Minute, 2 * time.Minute}
	if len(f.slept) != len(want) {
		t.Fatalf("slept %v, want %v", f.slept, want)
	}
	for i := range want {
		if f.slept[i] != want[i] {
			t.Fatalf("slept %v, want %v", f.slept, want)
		}
	}
	if n := strings.Count(f.warnings.String(), "warning: "); n != 3 || !strings.Contains(f.warnings.String(), "network down") {
		t.Fatalf("warnings:\n%s", f.warnings.String())
	}
}

// Criterion 7: a cancelled context (SIGINT/SIGTERM) ends the wait at once.
func TestFeatureWaitCancelled(t *testing.T) {
	f := newFakeWait(domain.StateCandidate)
	env := f.env()
	ctx, cancel := context.WithCancel(context.Background())
	env.sleep = func(ctx context.Context, d time.Duration) error {
		cancel()
		return ctx.Err()
	}
	if _, err := waitFeature(ctx, env, domain.StateApproved, 24*time.Hour, 30*time.Second); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if signalExitCode(os.Interrupt) != 130 || signalExitCode(sigTERM) != 143 {
		t.Fatal("signal exit codes")
	}
}

// End to end on a real project (criteria 1, 2, 4, 5, 6): the command reads the
// backend, prints the one line with actor, time and next step, exits with the
// documented codes, and never writes to .docs/ or docs.lock.
func TestFeatureWaitCommand(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	id := newFeature(t, projDir, "Feature esperada", 1)
	writeDoc(t, repoDir, id, "spec.md", realDoc(id, "Problema real descrito aqui."))
	commitBackend(t, repoDir)
	// wait runs feature wait and fails if it wrote docs.lock or .docs/.
	wait := func(args ...string) (string, error) {
		t.Helper()
		lock := mustRead(t, filepath.Join(projDir, "docs.lock"))
		docs := treeDigest(t, filepath.Join(projDir, ".docs"))
		out, err := execCerne(t, append(append([]string{"feature", "wait"}, args...), "--dir", projDir)...)
		if string(mustRead(t, filepath.Join(projDir, "docs.lock"))) != string(lock) {
			t.Errorf("feature wait %v changed docs.lock", args)
		}
		if treeDigest(t, filepath.Join(projDir, ".docs")) != docs {
			t.Errorf("feature wait %v changed .docs/", args)
		}
		return out, err
	}

	out, err := wait(id, "--for", "approved", "--timeout", "0")
	if ExitCode(err) != 3 || !strings.Contains(out, id+": still candidate") || strings.Contains(out, "Error:") || strings.Contains(out, "Usage:") {
		t.Fatalf("timeout 0 on a candidate: code %d\n%s", ExitCode(err), out)
	}

	t.Setenv("CERNE_ACTOR", "human")
	if out, err := execCerne(t, "feature", "approve", id, "--dir", projDir); err != nil {
		t.Fatalf("approve: %v\n%s", err, out)
	}
	out, err = wait("f-0001", "--for", "approved", "--timeout", "0")
	if err != nil || !strings.Contains(out, id+": approved by human") || !strings.Contains(out, " — next: ") {
		t.Fatalf("approved: %v\n%s", err, out)
	}

	for _, bad := range [][]string{{"F-0999", "--for", "approved"}, {id, "--for", "shipped"}, {id}} {
		_, err := wait(append(bad, "--timeout", "0")...)
		if ExitCode(err) != 1 {
			t.Errorf("%v: code %d (%v), want 1", bad, ExitCode(err), err)
		}
	}

	store.SaveFeature(repoDir, store.Feature{ID: id, Title: "Feature esperada", State: domain.StateRejected, Reason: "fora de escopo", CreatedAt: time.Now(), UpdatedAt: time.Now()})
	commitBackend(t, repoDir)
	out, err = wait(id, "--for", "done", "--timeout", "0")
	if ExitCode(err) != 2 || !strings.Contains(out, "rejected") || !strings.Contains(out, "fora de escopo") {
		t.Fatalf("unreachable: code %d\n%s", ExitCode(err), out)
	}

}

// TestFeatureWaitWakesOnMessage (B-0065): a message addressed to the
// session ends the wait — an idle session wakes only when a background
// command ends — but a reached state still wins.
func TestFeatureWaitWakesOnMessage(t *testing.T) {
	f := newFakeWait(domain.StateImplemented)
	env := f.env()
	env.inbox = func() (string, error) {
		if f.calls < 2 {
			return "", nil
		}
		return "cerne channel:\n#19 Henrique (human) → you: corrige?\n", nil
	}
	r, err := waitFeature(context.Background(), env, domain.StateDone, time.Hour, 30*time.Second)
	if err != nil || r.code != waitMessage || !strings.Contains(r.inbox, "#19") || f.calls != 2 {
		t.Fatalf("got %+v, %v after %d refreshes; want code %d with the message on the 2nd", r, err, f.calls, waitMessage)
	}
	f = newFakeWait(domain.StateDone)
	env = f.env()
	env.inbox = func() (string, error) { return "cerne channel:\n#19 x\n", nil }
	if r, err := waitFeature(context.Background(), env, domain.StateDone, time.Hour, 30*time.Second); err != nil || r.code != 0 {
		t.Fatalf("reached and a message: %+v, %v; want code 0", r, err)
	}
}

// TestFeatureWaitMessageCommand (B-0065): the human's message from the card's
// page ends the session's wait with exit 4, the message, how to answer and
// how to wait again; without a session, or once acked, it does not.
func TestFeatureWaitMessageCommand(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	id := newFeature(t, projDir, "Feature esperada", 1)
	if _, err := app.ChannelPost(filepath.Dir(repoDir), "Henrique", id, []string{"sess-dev"}, "pode arrumar os conflitos?", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CERNE_SESSION", "")
	if out, err := execCerne(t, "feature", "wait", id, "--for", "done", "--timeout", "0", "--dir", projDir); ExitCode(err) != 3 {
		t.Fatalf("no session: code %d, want 3 (timeout)\n%s", ExitCode(err), out)
	}
	t.Setenv("CERNE_SESSION", "sess-dev")
	out, err := execCerne(t, "feature", "wait", id, "--for", "done", "--timeout", "0", "--dir", projDir)
	for _, want := range []string{id + ": still candidate", "#1 Henrique (human) → you, about " + id + ": pode arrumar os conflitos?", "cerne msg send --ref " + id, "cerne feature wait " + id + " --for done"} {
		if ExitCode(err) != 4 || !strings.Contains(out, want) {
			t.Fatalf("addressed message: code %d, want 4 and %q\n%s", ExitCode(err), want, out)
		}
	}
	if out, err := execCerne(t, "msg", "ack", "1", "--dir", projDir); err != nil {
		t.Fatalf("ack: %v\n%s", err, out)
	}
	if out, err := execCerne(t, "feature", "wait", id, "--for", "done", "--timeout", "0", "--dir", projDir); ExitCode(err) != 3 {
		t.Fatalf("acked: code %d, want 3\n%s", ExitCode(err), out)
	}
}

// Critic finding 1 (criterion 6): a read of the clone that fails after the
// first one (a torn file while another agent pushes) is transient: warn, back
// off, keep waiting. Only the first read is fatal.
func TestFeatureWaitLoadErrorMidLoop(t *testing.T) {
	torn := errors.New("unexpected end of JSON input")
	f := newFakeWait(domain.StateCandidate, domain.StateCandidate, domain.StateApproved)
	f.loadErrs = map[int]error{1: torn}
	r, err := waitFeature(context.Background(), f.env(), domain.StateApproved, 24*time.Hour, 30*time.Second)
	if err != nil || r.code != 0 || !strings.Contains(f.warnings.String(), "unexpected end of JSON input") {
		t.Fatalf("r=%+v err=%v warnings=%q", r, err, f.warnings.String())
	}
	if len(f.slept) != 2 || f.slept[0] != 30*time.Second || f.slept[1] != 30*time.Second {
		t.Fatalf("slept %v: a first failed read waits the interval, like a first failed fetch", f.slept)
	}
	f = newFakeWait(domain.StateCandidate)
	f.loadErrs = map[int]error{0: torn}
	if _, err := waitFeature(context.Background(), f.env(), domain.StateApproved, 24*time.Hour, 30*time.Second); !errors.Is(err, torn) {
		t.Fatalf("first read: err = %v, want it returned", err)
	}
}

// Critic finding 2 (criterion 7): a hung fetch neither blocks a signal nor
// outlives --timeout.
func TestFeatureWaitHungRefresh(t *testing.T) {
	hung := func(c context.Context) error { <-c.Done(); return c.Err() }
	env := waitEnv{
		refresh: hung,
		load:    func() (store.Feature, error) { return store.Feature{ID: "F-0001", State: domain.StateCandidate}, nil },
		stale:   func(store.Feature) bool { return false },
		sleep:   func(c context.Context, d time.Duration) error { return nil },
		now:     time.Now,
		warn:    io.Discard,
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(100 * time.Millisecond); cancel() }()
	start := time.Now()
	if _, err := waitFeature(ctx, env, domain.StateApproved, 24*time.Hour, 30*time.Second); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel during a hung fetch: err = %v", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("cancel took %v", d)
	}
	start = time.Now()
	r, err := waitFeature(context.Background(), env, domain.StateApproved, 300*time.Millisecond, 30*time.Second)
	if err != nil || r.code != waitTimedOut || time.Since(start) > 3*time.Second {
		t.Fatalf("timeout during a hung fetch: r=%+v err=%v after %v", r, err, time.Since(start))
	}
}

// Critic finding 8: the error backoff never waits less than --interval.
func TestFeatureWaitBackoffNotBelowInterval(t *testing.T) {
	f := newFakeWait(domain.StateCandidate, domain.StateCandidate, domain.StateApproved)
	f.errs = map[int]error{0: errors.New("down")}
	if _, err := waitFeature(context.Background(), f.env(), domain.StateApproved, 24*time.Hour, 10*time.Minute); err != nil {
		t.Fatal(err)
	}
	for _, d := range f.slept {
		if d < 10*time.Minute {
			t.Fatalf("slept %v below the 10m interval: %v", d, f.slept)
		}
	}
}

// Critic finding 3 (criteria 1, 6) and 10: the transition is made on another
// clone and pushed; the wait only sees it through its fetch, finds a feature
// it had never fetched, and still writes neither .docs/ nor docs.lock.
func TestFeatureWaitSeesRemoteAdvance(t *testing.T) {
	projDir, origin, repoDir := setupProject(t)
	id := newFeature(t, projDir, "Feature esperada", 1)
	other := filepath.Join(t.TempDir(), "other")
	if out, err := exec.Command("git", "clone", "-q", origin, other).CombinedOutput(); err != nil {
		t.Fatalf("clone: %v\n%s", err, out)
	}
	now := time.Now().UTC()
	for _, f := range []store.Feature{
		{ID: id, Title: "Feature esperada", State: domain.StateApproved, CreatedAt: now, UpdatedAt: now},
		{ID: "F-0002", Title: "Criada em outra máquina", State: domain.StateApproved, CreatedAt: now, UpdatedAt: now},
	} {
		if err := store.SaveFeature(other, f); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"add", "-A"}, {"-c", "user.email=t@x", "-c", "user.name=t", "commit", "-qm", "approve elsewhere"}, {"push", "-q", "origin", "HEAD"}} {
		if out, err := exec.Command("git", append([]string{"-C", other}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if f, _ := store.LoadFeature(repoDir, id); f.State != domain.StateCandidate {
		t.Fatalf("the project's clone already saw it: %s", f.State)
	}
	lock := mustRead(t, filepath.Join(projDir, "docs.lock"))
	docs := treeDigest(t, filepath.Join(projDir, ".docs"))
	for _, fid := range []string{id, "F-0002"} {
		out, err := execCerne(t, "feature", "wait", fid, "--for", "approved", "--timeout", "0", "--dir", projDir)
		if err != nil || !strings.Contains(out, fid+": approved") {
			t.Fatalf("%s: %v\n%s", fid, err, out)
		}
	}
	if string(mustRead(t, filepath.Join(projDir, "docs.lock"))) != string(lock) || treeDigest(t, filepath.Join(projDir, ".docs")) != docs {
		t.Fatal("the wait wrote docs.lock or .docs/ while fetching")
	}
}

// Critic finding 4 (criterion 3) end to end: a spec edited after the approve
// makes the approval stale, so the wait does not count it; the re-approval does.
func TestFeatureWaitStaleApprovalCommand(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	id := newFeature(t, projDir, "Feature esperada", 1)
	writeDoc(t, repoDir, id, "spec.md", realDoc(id, "Problema real descrito aqui."))
	commitBackend(t, repoDir)
	t.Setenv("CERNE_ACTOR", "human")
	if out, err := execCerne(t, "feature", "approve", id, "--dir", projDir); err != nil {
		t.Fatalf("approve: %v\n%s", err, out)
	}
	writeDoc(t, repoDir, id, "spec.md", realDoc(id, "Problema real, agora reescrito depois do approve."))
	commitBackend(t, repoDir)
	out, err := execCerne(t, "feature", "wait", id, "--for", "approved", "--timeout", "0", "--dir", projDir)
	if ExitCode(err) != waitTimedOut || !strings.Contains(out, "still approved") {
		t.Fatalf("stale approval counted: code %d\n%s", ExitCode(err), out)
	}
	if out, err := execCerne(t, "feature", "approve", id, "--dir", projDir); err != nil {
		t.Fatalf("re-approve: %v\n%s", err, out)
	}
	if out, err := execCerne(t, "feature", "wait", id, "--for", "approved", "--timeout", "0", "--dir", projDir); err != nil || !strings.Contains(out, " by ") {
		t.Fatalf("after re-approval: %v\n%s", err, out)
	}
}

// Critic finding 5 (criterion 7) end to end: SIGTERM and SIGINT end the
// command with 143 and 130, one stderr line, no cobra error or usage.
func TestFeatureWaitSignalCommand(t *testing.T) {
	projDir, _, _ := setupProject(t)
	id := newFeature(t, projDir, "Feature esperada", 1)
	for sig, want := range map[os.Signal]int{sigTERM: 143, os.Interrupt: 130} {
		ch := make(chan chan<- os.Signal, 1)
		old := notifyWaitSignals
		notifyWaitSignals = func(c chan<- os.Signal) { ch <- c }
		go func() { (<-ch) <- sig }()
		stdout, stderr, err := execCerneSplit(t, "feature", "wait", id, "--for", "approved", "--interval", "1h", "--dir", projDir)
		notifyWaitSignals = old
		if ExitCode(err) != want || stdout != "" || !strings.Contains(stderr, id+": wait interrupted (still candidate)") || strings.Contains(stderr, "Error:") || strings.Contains(stderr, "Usage:") {
			t.Fatalf("%v: code %d\nstdout %q\nstderr %q", sig, ExitCode(err), stdout, stderr)
		}
	}
}

// Critic finding 13: a negative --timeout is a usage error; an --interval
// under the 5s floor is raised with a warning.
func TestFeatureWaitFlagLimits(t *testing.T) {
	projDir, _, _ := setupProject(t)
	id := newFeature(t, projDir, "Feature esperada", 1)
	if _, err := execCerne(t, "feature", "wait", id, "--for", "approved", "--timeout", "-1s", "--dir", projDir); ExitCode(err) != 1 {
		t.Fatalf("negative timeout: code %d", ExitCode(err))
	}
	_, stderr, err := execCerneSplit(t, "feature", "wait", id, "--for", "approved", "--timeout", "0", "--interval", "1s", "--dir", projDir)
	if ExitCode(err) != waitTimedOut || !strings.Contains(stderr, "--interval raised to 5s") {
		t.Fatalf("interval floor: code %d stderr %q", ExitCode(err), stderr)
	}
}
