package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	calls    int
	slept    []time.Duration
	clock    time.Time
	warnings bytes.Buffer
}

func (f *fakeWait) env() waitEnv {
	return waitEnv{
		refresh: func() error {
			f.calls++
			return f.errs[f.calls-1]
		},
		load: func() (store.Feature, error) {
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
	if ExitCode(err) != 3 || !strings.Contains(out, id+": still candidate") || strings.Contains(out, "Error:") {
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
