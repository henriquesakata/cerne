package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/lint"
	"github.com/henriquesakata/cerne/internal/store"
)

// Feature wait (F-0460): an agent that asked a human gate leaves this running
// in the background and ends its turn; when the feature reaches the state, the
// command exits with one line and the harness wakes the agent. It reads the
// backend through the clone's fetch + fast-forward (no pull: .docs/, docs.lock
// and the backend are never written).

// Exit codes of feature wait, besides 0 (reached) and 1 (usage error).
const (
	waitUnreachable = 2
	waitTimedOut    = 3
)

// waitMaxBackoff caps the wait between refreshes after a transient error.
const waitMaxBackoff = 5 * time.Minute

var sigTERM os.Signal = syscall.SIGTERM

// transitionEvents are the event types that move a feature's state.
var transitionEvents = []string{
	"feature.approved", "feature.started", "feature.completed", "feature.accepted",
	"feature.rejected", "feature.superseded", "feature.reverted",
}

func newFeatureWaitCmd(dir *string) *cobra.Command {
	var forState string
	var timeout, interval time.Duration
	cmd := &cobra.Command{
		Use:   "wait <id> --for <state>",
		Short: "Block until a feature reaches a state (use it in the background after asking a human gate)",
		Long: "Blocks until the feature reaches --for (approved, in_development, implemented, done, rejected, superseded), " +
			"then prints one line — the transition, who made it and when, and the next step — and exits 0. A state at or past " +
			"--for on the forward path counts as reached; an approved feature whose approval went stale (its spec changed) " +
			"is not approved until a human re-approves it. Exit 2: the state became unreachable (e.g. rejected while waiting " +
			"for approved). Exit 3: --timeout ran out (--timeout 0 checks once). It refreshes the backend clone every " +
			"--interval (fetch + fast-forward, no pull: .docs/ and docs.lock are never written) and backs off on network " +
			"errors. Run it in the background after asking a human to approve or accept, if your harness wakes you when a " +
			"background command ends; the gate stays human.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFeatureWait(cmd, *dir, args[0], forState, timeout, interval)
		},
	}
	cmd.Flags().StringVar(&forState, "for", "", "state to wait for: approved|in_development|implemented|done|rejected|superseded (required)")
	cmd.Flags().DurationVar(&timeout, "timeout", 24*time.Hour, "give up after this long (exit 3); 0 checks once")
	cmd.Flags().DurationVar(&interval, "interval", 30*time.Second, "how often to refresh the backend (min 5s)")
	return cmd
}

// waitEnv is what the wait loop needs from the world, injectable for tests.
type waitEnv struct {
	refresh func() error
	load    func() (store.Feature, error)
	stale   func(store.Feature) bool
	sleep   func(context.Context, time.Duration) error
	now     func() time.Time
	warn    io.Writer
}

// waitResult is how a wait ended: code 0 reached, 2 unreachable, 3 timed out.
type waitResult struct {
	start, end store.Feature
	startStale bool
	code       int
	waited     time.Duration
}

// waitFeature polls until the feature reaches target (or past it), becomes
// unable to reach it, or timeout runs out. A failed refresh warns and backs
// off (interval doubling, capped at waitMaxBackoff) but still judges the
// clone's current state; a cancelled ctx returns its error at once.
func waitFeature(ctx context.Context, env waitEnv, target domain.FeatureState, timeout, interval time.Duration) (waitResult, error) {
	var r waitResult
	began := env.now()
	backoff := interval
	for first := true; ; first = false {
		rerr := env.refresh()
		if rerr != nil {
			fmt.Fprintf(env.warn, "warning: cannot refresh the backend (%v); judging the local clone and retrying\n", rerr)
		}
		f, err := env.load()
		if err != nil {
			return r, err
		}
		stale := env.stale(f)
		if first {
			r.start, r.startStale = f, stale
		}
		r.end, r.waited = f, env.now().Sub(began)
		switch {
		case domain.FeatureAtOrPast(f.State, target) && !(f.State == domain.StateApproved && target == domain.StateApproved && stale):
			return r, nil
		case !domain.FeatureReachable(f.State, target):
			r.code = waitUnreachable
			return r, nil
		}
		left := timeout - r.waited
		if left <= 0 {
			r.code = waitTimedOut
			return r, nil
		}
		d := interval
		if rerr != nil {
			d = backoff
			backoff = min(backoff*2, waitMaxBackoff)
		} else {
			backoff = interval
		}
		if err := env.sleep(ctx, min(d, left)); err != nil {
			return r, err
		}
	}
}

// signalExitCode is the conventional shell code of a process ended by sig.
func signalExitCode(sig os.Signal) int {
	if sig == sigTERM {
		return 128 + 15
	}
	return 128 + 2
}

func runFeatureWait(cmd *cobra.Command, dir, id, forState string, timeout, interval time.Duration) error {
	id = domain.CanonicalID(id)
	target := domain.FeatureState(forState)
	if !domain.IsFeatureID(id) {
		return fmt.Errorf("%s is not a feature id (F-/E-/T-/B-)", id)
	}
	if target == domain.StateCandidate || !slices.Contains(mainPathAndTerminals, target) {
		return fmt.Errorf("--for %q: want one of approved, in_development, implemented, done, rejected, superseded", forState)
	}
	if interval < 5*time.Second {
		interval = 5 * time.Second
	}
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	if _, err := store.LoadFeature(ctx.repoDir, id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("%s is not a feature in this project", id)
		}
		return err
	}

	sigCtx, cancel := context.WithCancel(cmd.Context())
	defer cancel()
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, sigTERM)
	defer signal.Stop(sigs)
	var got os.Signal
	go func() {
		select {
		case got = <-sigs:
			cancel()
		case <-sigCtx.Done():
		}
	}()

	env := waitEnv{
		refresh: func() error { return gitback.SafePullNow(ctx.repoDir) },
		load:    func() (store.Feature, error) { return store.LoadFeature(ctx.repoDir, id) },
		stale: func(f store.Feature) bool {
			s, _, _ := lint.SpecChangedAfterApproval(ctx.repoDir, f)
			return s
		},
		sleep: func(c context.Context, d time.Duration) error {
			if !sleepCtx(c, d) {
				return c.Err()
			}
			return nil
		},
		now:  time.Now,
		warn: cmd.ErrOrStderr(),
	}
	r, err := waitFeature(sigCtx, env, target, timeout, interval)
	if err != nil {
		if errors.Is(err, context.Canceled) && got != nil {
			cmd.SilenceErrors = true
			fmt.Fprintf(cmd.ErrOrStderr(), "%s: wait interrupted (still %s)\n", id, r.end.State)
			return exitError{code: signalExitCode(got)}
		}
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), waitLine(ctx, id, target, r))
	if r.code != 0 {
		cmd.SilenceErrors = true // the line above is the message
		return exitError{code: r.code}
	}
	return nil
}

var mainPathAndTerminals = []domain.FeatureState{
	domain.StateApproved, domain.StateInDevelopment, domain.StateImplemented,
	domain.StateDone, domain.StateRejected, domain.StateSuperseded,
}

// waitLine is the one line the woken agent reads.
func waitLine(ctx projCtx, id string, target domain.FeatureState, r waitResult) string {
	var b strings.Builder
	b.WriteString(id + ": ")
	switch {
	case r.code == waitTimedOut:
		fmt.Fprintf(&b, "still %s after %s waiting for %s", r.end.State, r.waited.Round(time.Second), target)
		return b.String()
	case r.start.State != r.end.State:
		fmt.Fprintf(&b, "%s → %s", r.start.State, r.end.State)
	case r.startStale:
		fmt.Fprintf(&b, "%s (approval renewed)", r.end.State)
	default:
		b.WriteString(string(r.end.State))
	}
	if ev, ok := lastTransition(ctx.repoDir, id); ok {
		fmt.Fprintf(&b, " by %s at %s", ev.Actor, ev.At.UTC().Format(time.RFC3339))
	}
	if r.code == waitUnreachable {
		fmt.Fprintf(&b, " — %s can no longer be reached", target)
		if r.end.Reason != "" {
			fmt.Fprintf(&b, " (reason: %s)", flatTitle(r.end.Reason))
		}
		return b.String()
	}
	if st, err := app.NextStep(ctx.repoDir, ctx.projDir, id); err == nil {
		fmt.Fprintf(&b, " — next: %s", st.Step)
		if st.Command != "" {
			fmt.Fprintf(&b, " (run: %s)", st.Command)
		}
	}
	return b.String()
}

// lastTransition is the newest state-changing event of id.
func lastTransition(repoDir, id string) (domain.Event, bool) {
	evs, err := store.ReadEvents(repoDir)
	if err != nil {
		return domain.Event{}, false
	}
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i].EntityID == id && slices.Contains(transitionEvents, evs[i].Type) {
			return evs[i], true
		}
	}
	return domain.Event{}, false
}
