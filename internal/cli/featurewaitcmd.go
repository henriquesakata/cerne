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
	"github.com/henriquesakata/cerne/internal/project"
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
	waitMessage     = 4 // B-0065: a channel message addressed to the session
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
			"then prints one line — the transition, who made it and when, and the next step — and exits 0; the wait is what wakes a stopped session. " +
			"Without it the move still reaches you: a human's approve, accept, reject, revert or supersede leaves that line as a card notice " +
			"the card's sessions see once, at their next turn or tool call (F-0504). A state at or past " +
			"--for on the forward path counts as reached; an approved feature whose approval went stale (its spec changed) " +
			"is not approved until a human re-approves it. Exit 2: the state became unreachable (e.g. rejected while waiting " +
			"for approved). Exit 3: --timeout ran out (--timeout 0 checks once). It refreshes the backend clone every " +
			"--interval (fetch + fast-forward, no pull: .docs/ and docs.lock are never written) and backs off on network " +
			"errors. Exit 4: a channel message addressed to your session ($CERNE_SESSION), not yet shown to it, arrived first — e.g. the human's, " +
			"from the card's page; it prints the message and how to answer, then wait again. Run it in the background after " +
			"asking a human to approve or accept, if your harness wakes you when a background command ends; the gate stays human. " +
			"Under [gates] accept = \"merge\" (F-0488), --for done also asks the forge each round and, once the card's PR merged " +
			"into the default branch, records that merge as the human's acceptance (the one write it makes).",
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
	refresh func(context.Context) error // must return once its ctx ends
	load    func() (store.Feature, error)
	stale   func(store.Feature) bool
	sleep   func(context.Context, time.Duration) error
	now     func() time.Time
	warn    io.Writer
	inbox   func() (string, error) // B-0065: addressed messages; nil: no session
}

// waitResult is how a wait ended: code 0 reached, 2 unreachable, 3 timed out.
type waitResult struct {
	start, end store.Feature
	startStale bool
	code       int
	waited     time.Duration
	inbox      string // the addressed messages that ended the wait (code 4)
}

// waitRefreshCap bounds the one refresh of a --timeout 0 check.
const waitRefreshCap = time.Minute

// waitFeature polls until the feature reaches target (or past it), becomes
// unable to reach it, or timeout runs out. A failed refresh — or a failed read
// of the clone after the first one (a file torn by a concurrent push) — warns
// and backs off (interval doubling, capped at waitMaxBackoff, never below the
// interval) but keeps waiting; a refresh never outlives the time left, and a
// cancelled ctx returns its error at once, even mid-fetch.
func waitFeature(ctx context.Context, env waitEnv, target domain.FeatureState, timeout, interval time.Duration) (waitResult, error) {
	var r waitResult
	began := env.now()
	backoff := interval
	for first := true; ; first = false {
		limit := timeout - env.now().Sub(began)
		if limit <= 0 {
			limit = waitRefreshCap
		}
		rctx, rcancel := context.WithTimeout(ctx, limit)
		failed := env.refresh(rctx)
		rcancel()
		if ctx.Err() != nil {
			return r, ctx.Err()
		}
		if failed != nil {
			fmt.Fprintf(env.warn, "warning: cannot refresh the backend (%v); judging the local clone and retrying\n", failed)
		}
		if f, err := env.load(); err != nil {
			if first {
				return r, err
			}
			fmt.Fprintf(env.warn, "warning: cannot read the feature from the local clone (%v); retrying\n", err)
			failed = err
			r.waited = env.now().Sub(began)
		} else {
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
		}
		if env.inbox != nil {
			if msg, err := env.inbox(); err != nil {
				fmt.Fprintf(env.warn, "warning: cannot read the channel (%v)\n", err)
			} else if msg != "" {
				r.code, r.inbox = waitMessage, msg
				return r, nil
			}
		}
		left := timeout - r.waited
		if left <= 0 {
			r.code = waitTimedOut
			return r, nil
		}
		d := interval
		if failed != nil {
			d = backoff
			backoff = max(interval, min(backoff*2, waitMaxBackoff))
		} else {
			backoff = interval
		}
		if err := env.sleep(ctx, min(d, left)); err != nil {
			return r, err
		}
	}
}

// notifyWaitSignals routes SIGINT and SIGTERM to c; a package var so a test
// can deliver one without signalling its own process.
var notifyWaitSignals = func(c chan<- os.Signal) { signal.Notify(c, os.Interrupt, sigTERM) }

// refreshInBackground wraps a blocking refresh (git has no context) so the
// wait can stop waiting for it — on a signal or when time runs out — and never
// stacks a second fetch behind a hung one: while the previous call is still
// running, a new one fails at once.
func refreshInBackground(refresh func() error) func(context.Context) error {
	var running chan struct{}
	return func(c context.Context) error {
		if running != nil {
			select {
			case <-running:
			default:
				return errors.New("the previous refresh is still running")
			}
		}
		done := make(chan struct{})
		running = done
		var err error
		go func() {
			err = refresh()
			close(done)
		}()
		select {
		case <-done:
			return err
		case <-c.Done():
			return c.Err()
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
	if timeout < 0 {
		return fmt.Errorf("--timeout %s: want a duration of 0 or more (0 checks once)", timeout)
	}
	if interval < 5*time.Second {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: --interval raised to 5s (was %s)\n", interval)
		interval = 5 * time.Second
	}
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	refresh := refreshInBackground(func() error { return gitback.SafePullNow(ctx.repoDir) })
	// One refresh before the existence check: a feature created on another
	// machine is found instead of refused. Its error is the loop's to report.
	_ = refresh(cmd.Context())
	pre, err := store.LoadFeature(ctx.repoDir, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("%s is not a feature in this project", id)
		}
		return err
	}

	sigCtx, cancel := context.WithCancel(cmd.Context())
	defer cancel()
	sigs := make(chan os.Signal, 1)
	notifyWaitSignals(sigs)
	defer signal.Stop(sigs)
	got := make(chan os.Signal, 1)
	go func() {
		select {
		case s := <-sigs:
			got <- s
			cancel()
		case <-sigCtx.Done():
		}
	}()

	load := func() (store.Feature, error) { return store.LoadFeature(ctx.repoDir, id) }
	if target == domain.StateDone && ctx.cfg.Gates.AcceptsOnMerge() {
		load = mergeAwareLoad(ctx, id, load, cmd.OutOrStdout(), cmd.ErrOrStderr())
	}
	env := waitEnv{
		refresh: refresh,
		load:    load,
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
	if sess := os.Getenv("CERNE_SESSION"); sess != "" {
		cacheDir, err := project.CacheDir(ctx.cfg.ProjectID)
		if err != nil {
			return err
		}
		env.inbox = func() (string, error) { return app.ChannelWake(cacheDir, ctx.repoDir, sess, time.Now().UTC()) }
	}
	r, err := waitFeature(sigCtx, env, target, timeout, interval)
	if err != nil {
		select {
		case s := <-got:
			cmd.SilenceErrors = true
			state := r.end.State
			if state == "" { // interrupted before the loop's first read
				state = pre.State
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "%s: wait interrupted (still %s)\n", id, state)
			return exitError{code: signalExitCode(s)}
		default:
		}
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), waitLine(ctx, id, target, r))
	if r.code == waitMessage {
		fmt.Fprintf(cmd.OutOrStdout(), "%s↳ then wait again: cerne feature wait %s --for %s\n", r.inbox, id, target)
	}
	if r.code != 0 {
		cmd.SilenceErrors = true // the line above is the message
		return exitError{code: r.code}
	}
	return nil
}

// mergeAwareLoad is the wait's load under the merge gate (F-0488): before the
// state is judged, an implemented card with a delivery PR asks the forge and
// records the merge, printing its line on out. Anything but the open PR (the
// normal wait) is a warning, once per distinct line; the wait goes on.
func mergeAwareLoad(ctx projCtx, id string, load func() (store.Feature, error), out, warn io.Writer) func() (store.Feature, error) {
	last := ""
	return func() (store.Feature, error) {
		f, err := load()
		if err != nil || f.State != domain.StateImplemented || f.DeliveryMRURL == "" {
			return f, err
		}
		checks, err := app.AcceptMerged(ctx.repoDir, ctx.projDir, ctx.cfg, []string{id})
		line := ""
		switch {
		case err != nil:
			line = err.Error()
		case checks[0].Accepted:
			fmt.Fprintln(out, checks[0].Line)
			return load()
		case !checks[0].Waiting:
			line = checks[0].Line
		}
		if line != "" && line != last {
			fmt.Fprintf(warn, "warning: %s\n", line)
		}
		last = line
		return f, nil
	}
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
	case r.code == waitMessage:
		fmt.Fprintf(&b, "still %s — a message addressed to you arrived while waiting for %s:", r.end.State, target)
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
	b.WriteString(app.NextStepTail(ctx.repoDir, ctx.projDir, id))
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
