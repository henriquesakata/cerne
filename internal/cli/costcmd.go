package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/cost"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// claudeRoots locates the Claude Code transcripts; TestMain pins it to
// CLAUDE_CONFIG_DIR alone so no test reads the real ~/.claude.
var claudeRoots = cost.ClaudeRoots

func newCostCmd() *cobra.Command {
	var dir string
	var asJSON, stamp bool
	cmd := &cobra.Command{
		Use:   "cost [<id>]",
		Short: "Tokens an entity consumed, read from the Claude Code session transcripts",
		Long: "Reads the four real token counters (input, cache_creation, cache_read, output) of every " +
			"assistant message in the Claude Code transcripts of the sessions bound to <id>, and attributes " +
			"each message to the binding in force at its timestamp (the bind hook records one ledger line " +
			"per 'cerne work start' / 'cerne feature start'). Messages of those sessions outside any binding " +
			"are reported as unattributed, never dropped; subagent (sidechain) tokens are included and shown " +
			"apart; a subagent message follows the subagent's own binding when it has one, otherwise the " +
			"main thread's binding in force at the subagent's first message, unless 'cerne cost attribute' " +
			"moved it. A binding ends when its work or feature closes. With a role other than the main " +
			"thread's, a By role block (builder, critic, judge, other, main) follows the per-model one. Without <id>, the project view per entity. Tokens only — no monetary value. Not " +
			"'cerne savings', which measures the reading an agent is spared. An id with children (an epic) also " +
			"sums their stamped anchors and names the children without one. --json for structured output. " +
			"--stamp, after 'cerne feature complete' and before the human accept, writes the cost into the " +
			"feature's result as a '## Custo' block with a machine-readable anchor (re-stamping replaces it).",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := ""
			if len(args) == 1 {
				id = args[0]
			}
			if stamp {
				return runCostStamp(cmd, dir, id)
			}
			return runCost(cmd, dir, id, asJSON)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.Flags().MarkHidden("dir")
	cmd.Flags().BoolVar(&asJSON, "json", false, "machine-readable JSON output")
	cmd.Flags().BoolVar(&stamp, "stamp", false, "write the cost into the implemented feature's result (## Custo)")
	cmd.AddCommand(newCostAttributeCmd())
	cmd.AddCommand(newCostBackfillCmd())
	cmd.AddCommand(newCostBackfillEffortCmd())
	return cmd
}

func newCostAttributeCmd() *cobra.Command {
	var dir, role, session string
	cmd := &cobra.Command{
		Use:   "attribute <agent-id> <id>",
		Short: "Count a subagent's whole cost on a feature, under a role (F-0384)",
		Long: "Records in the cost ledger that subagent <agent-id> of the session worked for <id> (F-/E-/T-): " +
			"every message of that subagent, before the record too, counts on <id> instead of the binding it " +
			"inherited, so nothing is counted twice. For agents that run no cerne command — the Gauntlet's " +
			"critic and judge: run it after launching them. --role names what it did (critic, judge, builder, " +
			"other); without it, the subagent's agentType decides (dev is builder, code-reviewer is critic). " +
			"Refused, writing nothing, when the session's transcripts hold no subagents/agent-<agent-id>.jsonl.",
		Example: "  cerne cost attribute a1b2c3d4e5f6a7b8c F-0042 --role critic",
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCostAttribute(cmd, dir, msgSession(session), args[0], args[1], role)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.Flags().MarkHidden("dir")
	cmd.Flags().StringVar(&role, "role", "", "critic, judge, builder or other (default: from the subagent's agentType)")
	cmd.Flags().StringVar(&session, "session", "", "Claude Code session id (defaults to $CERNE_SESSION, exported by the SessionStart hook)")
	return cmd
}

func runCostAttribute(cmd *cobra.Command, dir, sess, agent, id, role string) error {
	if sess == "" {
		return errors.New("no session: pass --session or set CERNE_SESSION (the SessionStart hook exports it — re-run `cerne hook install` if it is missing)")
	}
	if !domain.IsFeatureID(id) {
		return fmt.Errorf("cost attribute takes a feature, epic, task or bug id (F-/E-/T-/B-), got %q", id)
	}
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	if _, err := store.LoadFeature(ctx.repoDir, id); err != nil {
		return err
	}
	in := cost.Inputs{CacheDir: filepath.Dir(ctx.repoDir), RepoDir: ctx.repoDir, Roots: claudeRoots()}
	if err := cost.Attribute(in, sess, agent, id, role, time.Now().UTC()); err != nil {
		return err
	}
	as := " as " + role
	if role == "" {
		as = ", role from its agentType"
	}
	fmt.Fprintf(cmd.OutOrStdout(), "✓ subagent %s of session %s attributed to %s%s — all its messages count there (cerne cost %s).\n", agent, sess, id, as, id)
	return nil
}

// runCostStamp writes id's cost into its result through the amend path of an
// implemented feature (F-0305), then drops the cost snapshot.
func runCostStamp(cmd *cobra.Command, dir, id string) error {
	if id == "" {
		return errors.New("--stamp needs a feature id: cerne cost <id> --stamp")
	}
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	o, err := stampCost(ctx, id, false)
	if err != nil {
		return err
	}
	w := cmd.OutOrStdout()
	switch o.kind {
	case stampKeptHigher:
		fmt.Fprintf(w, "%s cost unchanged — its stamp already covers more messages (%d) than the transcripts still hold.\n", id, o.report.Messages)
	case stampLowerRefused:
		fmt.Fprintf(w, "%s cost unchanged — %v\n", id, cost.ErrStampWouldLower)
	case stampWritten:
		fmt.Fprintf(w, "✓ %s cost stamped — '## Custo' in its result (cerne show %s --doc result).\n", id, id)
	default:
		fmt.Fprintf(w, "%s cost unchanged — the result already carries this stamp.\n", id)
	}
	return nil
}

// stampCost is the one stamp path: `cerne cost --stamp` and the complete
// transition (F-0402) both run it, with no pull or push of their own.
//
// justCompleted (B-0090) is the complete path: the automatic accept may have
// carried the card to done in the same transition, and the stamp then still
// runs, judged as the implemented card it was a moment ago.
func stampCost(ctx projCtx, id string, justCompleted bool) (stampOutcome, error) {
	f, err := store.LoadFeature(ctx.repoDir, id)
	if err != nil {
		return stampOutcome{}, err
	}
	cacheDir := filepath.Dir(ctx.repoDir)
	state, amend := f.State, app.AmendFeatureResult
	if justCompleted && state == domain.StateDone {
		state, amend = domain.StateImplemented, app.AmendCompletedResult
	}
	r, err := cost.PrecheckStamp(cost.Inputs{CacheDir: cacheDir, RepoDir: ctx.repoDir, Roots: claudeRoots()}, state, id)
	if err != nil {
		return stampOutcome{}, err
	}
	// The anchor already holds more than the transcripts: keep it, and drop the
	// snapshot — it can only hold less (C12).
	if r.Source == "stamp" {
		return stampOutcome{report: r, kind: stampKeptHigher}, cost.DropSnapshot(cacheDir, id)
	}
	raw, err := os.ReadFile(app.FeatureDocPath(ctx.repoDir, id, "result.md"))
	if err != nil {
		return stampOutcome{}, err
	}
	body, err := cost.Stamp(string(raw), r)
	if errors.Is(err, cost.ErrStampWouldLower) {
		return stampOutcome{report: r, kind: stampLowerRefused}, cost.DropSnapshot(cacheDir, id)
	}
	if err != nil {
		return stampOutcome{}, err
	}
	_, changed, err := amend(ctx.repoDir, ctx.projDir, id, body, "cost stamp (F-0309)")
	if err != nil {
		return stampOutcome{}, err
	}
	if err := cost.DropSnapshot(cacheDir, id); err != nil {
		return stampOutcome{}, err
	}
	if changed {
		return stampOutcome{report: r, kind: stampWritten}, nil
	}
	return stampOutcome{report: r, kind: stampUnchanged}, nil
}

func runCost(cmd *cobra.Command, dir, id string, asJSON bool) error {
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	if id != "" {
		if !domain.IsFeatureID(id) {
			return fmt.Errorf("cost takes a feature, epic, task or bug id (F-/E-/T-/B-), got %q", id)
		}
		if _, err := store.LoadFeature(ctx.repoDir, id); err != nil {
			return err
		}
	}
	r, err := cost.Compute(cost.Inputs{CacheDir: filepath.Dir(ctx.repoDir), RepoDir: ctx.repoDir, Roots: claudeRoots()}, id)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	if asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(r)
	}
	renderCost(out, r)
	return nil
}

func fmtCounters(c cost.Counters) string {
	return fmt.Sprintf("input %d · cache_creation %d · cache_read %d · output %d", c.Input, c.CacheCreation, c.CacheRead, c.Output)
}

func sortedKeys(m map[string]cost.Counters) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func renderCost(out io.Writer, r cost.Report) {
	name := r.ID
	if name == "" {
		name = "Project"
	}
	if r.Reason != "" {
		fmt.Fprintf(out, "%s: 0 tokens — %s. Bindings are recorded when an agent runs 'cerne work start' or 'cerne feature start' under the bind hook.\n", name, r.Reason)
		renderRollup(out, r)
		return
	}
	scope := ""
	if r.ID == "" {
		scope = ", attributed to entities"
	}
	fmt.Fprintf(out, "%s: tokens from the Claude Code transcripts of the sessions recorded in the ledger%s (no monetary value)\n", name, scope)
	for _, row := range []struct {
		label string
		n     int64
		what  string
	}{
		{"input", r.Counters.Input, "new input tokens, billed at the full input rate"},
		{"cache_creation", r.Counters.CacheCreation, "tokens written to the prompt cache"},
		{"cache_read", r.Counters.CacheRead, "tokens read back from the prompt cache"},
		{"output", r.Counters.Output, "tokens generated by the model"},
	} {
		fmt.Fprintf(out, "  %-15s %-12d %s\n", row.label, row.n, row.what)
	}
	if r.Source == "stamp" {
		fmt.Fprintln(out, "  (read from the stamp: the '## Custo' anchor in its result covers more messages than the transcripts and snapshot still hold)")
	}
	fmt.Fprintf(out, "  of which sidechain (subagents): %s\n", fmtCounters(r.Sidechain))
	fmt.Fprintln(out, "By model:")
	for _, m := range sortedKeys(r.ByModel) {
		fmt.Fprintf(out, "  %s  %s\n", m, fmtCounters(r.ByModel[m]))
	}
	if roles := r.Roles(); roles != nil {
		fmt.Fprintln(out, "By role:")
		for _, role := range roles {
			fmt.Fprintf(out, "  %s  %s\n", role, fmtCounters(r.ByRole[role]))
		}
	}
	if r.ID == "" {
		fmt.Fprintln(out, "By entity:")
		for _, e := range sortedKeys(r.Entities) {
			from := ""
			if slices.Contains(r.FromStamp, e) {
				from = "  (from the stamp)"
			}
			fmt.Fprintf(out, "  %s  %s%s\n", e, fmtCounters(r.Entities[e]), from)
		}
	}
	fmt.Fprintf(out, "Sessions: %d (%d without transcript, %d recovered from snapshot) · messages: %d\n",
		r.Sessions, r.SessionsWithoutTranscript, r.SessionsRecovered, r.Messages)
	if r.First != "" {
		fmt.Fprintf(out, "Window: %s → %s\n", r.First, r.Last)
	} else {
		fmt.Fprintln(out, "Window: — (no attributed message)")
	}
	fmt.Fprintf(out, "Unattributed (messages of these ledger sessions outside any binding): %s\n", fmtCounters(r.Unattributed))
	if len(r.OtherHarness) > 0 {
		fmt.Fprintf(out, "Left out (bound under another harness; cost reads Claude Code transcripts only): %s\n", strings.Join(r.OtherHarness, ", "))
	}
	if r.ID == "" {
		a, u := r.Counters, r.Unattributed
		fmt.Fprintf(out, "Total (attributed + unattributed): %s\n", fmtCounters(cost.Counters{Input: a.Input + u.Input,
			CacheCreation: a.CacheCreation + u.CacheCreation, CacheRead: a.CacheRead + u.CacheRead, Output: a.Output + u.Output}))
	}
	renderRollup(out, r)
}

// renderRollup prints an epic's descendants share and its total.
func renderRollup(out io.Writer, r cost.Report) {
	if r.Children == nil || r.Total == nil {
		return
	}
	list := func(ids []string) string {
		if len(ids) == 0 {
			return "(none)"
		}
		return strings.Join(ids, ", ")
	}
	fmt.Fprintf(out, "Children, at every depth (sum of their stamped anchors): %s\n", fmtCounters(r.Children.Counters))
	fmt.Fprintf(out, "  stamped: %s\n", list(r.Children.Stamped))
	fmt.Fprintf(out, "  without anchor: %s\n", list(r.Children.Missing))
	fmt.Fprintf(out, "Total (own + children): %s\n", fmtCounters(*r.Total))
}

func newCostBackfillCmd() *cobra.Command {
	var dir, since string
	var apply, asJSON bool
	cmd := &cobra.Command{
		Use:   "backfill-roles",
		Short: "Recover the critic and judge cost of past Gauntlets from the transcripts (F-0396)",
		Long: "Classifies every subagent of the project's ledger sessions by its transcript (a judge ends in the " +
			"verdict line or was asked for it; a critic is a code-reviewer or read the diff fence) and counts it on " +
			"the feature its parent session was bound to — never by what its prompt says; a prompt can only take a subagent out, to ambiguous, when it cites another feature than the binding's (the line names the reason). Without --apply it only " +
			"reports: one line per feature with the agents and weighted tokens per role, then what was left out " +
			"(unattributed, ambiguous, already with roles, no anchor, inconsistent). --apply records a retro bind " +
			"with the role per subagent and completes the anchor: an implemented feature's '## Custo' in its " +
			"result, a done feature's as an erratum carrying the same block. The anchor's totals never go down; " +
			"critic plus judge above them skips the feature as inconsistent. A second run writes nothing, a run after a " +
			"failure finishes what is missing, and --apply --json prints the applied list and the errors. Review the " +
			"agent ids the report lists before applying; an id wrongly classified is undone with " +
			"'cerne cost attribute <agent-id> <id> --role other' (a later bind wins). The code-reviewer rule also " +
			"counts a non-Gauntlet review (a spec critic) as a critic. " +
			"--since limits it to subagents that started on or after the date (YYYY-MM-DD).",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var from time.Time
			if since != "" {
				var err error
				if from, err = time.Parse("2006-01-02", since); err != nil {
					return fmt.Errorf("--since takes a date as YYYY-MM-DD, got %q", since)
				}
			}
			return runCostBackfill(cmd, dir, from, apply, asJSON)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.Flags().MarkHidden("dir")
	cmd.Flags().StringVar(&since, "since", "", "only subagents that started on or after this date (YYYY-MM-DD)")
	cmd.Flags().BoolVar(&apply, "apply", false, "record the roles (retro binds) and complete the anchors; without it, only report")
	cmd.Flags().BoolVar(&asJSON, "json", false, "machine-readable JSON output")
	return cmd
}

// appendBind is the ledger write of the backfill; a test swaps it to make a bind fail.
var appendBind = cost.AppendBind

// backfillOutcome is what `backfill-roles --apply --json` prints.
type backfillOutcome struct {
	Applied []backfillApplied `json:"applied"`
	Errors  []string          `json:"errors"`
}

type backfillApplied struct {
	ID    string `json:"id"`
	Bound int    `json:"bound"`
	Wrote string `json:"wrote,omitempty"` // "result" or "erratum"; empty when only binds were missing
}

func runCostBackfill(cmd *cobra.Command, dir string, since time.Time, apply, asJSON bool) error {
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	cacheDir := filepath.Dir(ctx.repoDir)
	r, err := cost.BackfillRoles(cost.Inputs{CacheDir: cacheDir, RepoDir: ctx.repoDir, Roots: claudeRoots()}, since)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	if asJSON && !apply {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(r)
	}
	text := out
	if asJSON {
		text = io.Discard
	}
	for _, f := range r.Features {
		var critics, judges int
		for _, a := range f.Agents {
			if a.Role == "critic" {
				critics++
			} else {
				judges++
			}
		}
		fmt.Fprintf(text, "%s  %s  critic %s, %.0f tokens · judge %s, %.0f tokens\n", f.ID, f.State,
			plural(critics, "agent"), cost.Weighted(f.Critic), plural(judges, "agent"), cost.Weighted(f.Judge))
		for _, a := range f.Agents {
			note := ""
			if a.Bound {
				note = " (already bound)"
			}
			fmt.Fprintf(text, "  %s/%s %s%s\n", a.Session, a.Agent, a.Role, note)
		}
	}
	for _, a := range r.Ambiguous {
		fmt.Fprintf(text, "ambiguous: %s/%s (%s) falls on %s (%s)\n", a.Session, a.Agent, a.Role, strings.Join(a.Entities, ", "), a.Reason)
	}
	for _, a := range r.Partial {
		fmt.Fprintf(text, "partial: %s/%s (%s) counts only the messages a binding governs\n", a.Session, a.Agent, a.Role)
	}
	fmt.Fprintf(text, "Left out: unattributed %d · ambiguous %d · already with roles %d · no anchor %d · inconsistent %d · partial %d (counted)\n",
		r.Unattributed, len(r.Ambiguous), len(r.HasRoles), len(r.NoAnchor), len(r.Inconsistent), len(r.Partial))
	if len(r.Features) == 0 && !asJSON {
		fmt.Fprintln(out, "nothing to backfill")
		return nil
	}
	if !apply {
		fmt.Fprintln(out, "Nothing written. Check the agent ids above, then run with --apply to record these roles and complete the anchors. A wrong role is undone by 'cerne cost attribute <agent-id> <id> --role other'.")
		return nil
	}
	// Binds first (the ledger write is the cheap, repeatable one), then the
	// document; a failure is collected and the next feature still runs. A
	// re-run finds what is missing: unbound agents, or an anchor without roles.
	outcome := backfillOutcome{Applied: []backfillApplied{}, Errors: []string{}}
	for _, f := range r.Features {
		ap := backfillApplied{ID: f.ID}
		now := time.Now().UTC()
		var failed error
		for _, a := range f.Agents {
			if a.Bound {
				continue
			}
			if failed = appendBind(cacheDir, cost.Bind{Session: a.Session, Agent: a.Agent, Entity: f.ID, Harness: cost.HarnessClaudeCode, Role: a.Role, Retro: true, TS: now}); failed != nil {
				break
			}
			ap.Bound++
		}
		if failed == nil && f.NeedsAnchor {
			var body string
			var asResult bool
			if body, asResult, failed = cost.RolesEdit(ctx.repoDir, f.ID, f.State == domain.StateDone, f.Critic, f.Judge); failed == nil {
				if asResult {
					ap.Wrote = "result"
					_, _, failed = app.AmendFeatureResult(ctx.repoDir, ctx.projDir, f.ID, body, "cost roles backfill (F-0396)")
				} else {
					ap.Wrote = "erratum"
					_, failed = app.AddErrata(ctx.repoDir, ctx.projDir, f.ID, body, app.EventActor())
				}
			}
		}
		if failed != nil {
			outcome.Errors = append(outcome.Errors, fmt.Sprintf("%s: %v", f.ID, failed))
			fmt.Fprintf(text, "✗ %s: %v\n", f.ID, failed)
			continue
		}
		outcome.Applied = append(outcome.Applied, ap)
		fmt.Fprintf(text, "✓ %s: %s bound%s\n", f.ID, plural(ap.Bound, "agent"), map[bool]string{true: ", anchor completed in the " + ap.Wrote, false: ""}[ap.Wrote != ""])
	}
	if asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		if err := enc.Encode(outcome); err != nil {
			return err
		}
	}
	if len(outcome.Errors) > 0 {
		return fmt.Errorf("%d of %d feature(s) failed; re-run --apply to finish them", len(outcome.Errors), len(r.Features))
	}
	return nil
}

func newCostBackfillEffortCmd() *cobra.Command {
	var dir string
	var apply, asJSON bool
	cmd := &cobra.Command{
		Use:   "backfill-effort",
		Short: "Recover the measured model effort of anchors stamped before B-0024 (F-0448)",
		Long: "For every implemented or done feature whose newest cost anchor (the last erratum carrying one, else " +
			"the result's) has no by_effort, recomputes the cost from the transcripts still on disk, as " +
			"'cerne cost <id> --stamp' does. A feature counts only when the recomputed counters equal the " +
			"anchor's: the effort is a partition of the stamped tokens. Without --apply it only reports: one line " +
			"per feature with each model's measured effort, then what was left out (already with effort, drifted " +
			"counters, no transcript, no effort recorded). --apply adds by_effort, model_efforts and the per-effort " +
			"line to the anchor, keeping everything else: an implemented feature's '## Custo' in its " +
			"result, a done feature's as an erratum carrying the same block. A second run writes nothing; --apply " +
			"--json prints the applied list and the errors.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runCostBackfillEffort(cmd, dir, apply, asJSON)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.Flags().MarkHidden("dir")
	cmd.Flags().BoolVar(&apply, "apply", false, "write the measured effort into the anchors; without it, only report")
	cmd.Flags().BoolVar(&asJSON, "json", false, "machine-readable JSON output")
	return cmd
}

// effortEdit is the anchor edit of the effort backfill; a test swaps it to make one fail.
var effortEdit = cost.EffortEdit

// effortOutcome is what `backfill-effort --apply --json` prints.
type effortOutcome struct {
	Applied []effortApplied `json:"applied"`
	Errors  []string        `json:"errors"`
}

type effortApplied struct {
	ID    string `json:"id"`
	Wrote string `json:"wrote"` // "result" or "erratum"
}

func runCostBackfillEffort(cmd *cobra.Command, dir string, apply, asJSON bool) error {
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	r, err := cost.BackfillEffort(cost.Inputs{CacheDir: filepath.Dir(ctx.repoDir), RepoDir: ctx.repoDir, Roots: claudeRoots()})
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	if asJSON && !apply {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(r)
	}
	text := out
	if asJSON {
		text = io.Discard
	}
	for _, f := range r.Features {
		pairs := make([]string, 0, len(f.ModelEfforts))
		for _, m := range slices.Sorted(maps.Keys(f.ModelEfforts)) {
			pairs = append(pairs, m+" "+strings.Join(f.ModelEfforts[m], "/"))
		}
		fmt.Fprintf(text, "%s  %s  %s\n", f.ID, f.State, strings.Join(pairs, " · "))
	}
	if len(r.Drifted) > 0 {
		fmt.Fprintf(text, "drifted: %s\n", strings.Join(r.Drifted, ", "))
	}
	fmt.Fprintf(text, "Left out: already with effort %d · drifted %d · no transcript %d · unmeasured %d\n",
		len(r.HasEffort), len(r.Drifted), len(r.NoTranscript), len(r.Unmeasured))
	if len(r.Features) == 0 && !asJSON {
		fmt.Fprintln(out, "nothing to backfill")
		return nil
	}
	if !apply {
		fmt.Fprintln(out, "Nothing written. Run with --apply to write the measured effort into these anchors.")
		return nil
	}
	outcome := effortOutcome{Applied: []effortApplied{}, Errors: []string{}}
	for _, f := range r.Features {
		ap := effortApplied{ID: f.ID, Wrote: "erratum"}
		body, asResult, failed := effortEdit(ctx.repoDir, f.ID, f.State == domain.StateDone, f.ByEffort, f.ModelEfforts)
		switch {
		case failed != nil:
		case asResult:
			ap.Wrote = "result"
			_, _, failed = app.AmendFeatureResult(ctx.repoDir, ctx.projDir, f.ID, body, "cost effort backfill (F-0448)")
		default:
			_, failed = app.AddErrata(ctx.repoDir, ctx.projDir, f.ID, body, app.EventActor())
		}
		if failed != nil {
			outcome.Errors = append(outcome.Errors, fmt.Sprintf("%s: %v", f.ID, failed))
			fmt.Fprintf(text, "✗ %s: %v\n", f.ID, failed)
			continue
		}
		outcome.Applied = append(outcome.Applied, ap)
		where := map[bool]string{true: "the result", false: "an erratum"}[ap.Wrote == "result"]
		fmt.Fprintf(text, "✓ %s: effort written in %s\n", f.ID, where)
	}
	if asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		if err := enc.Encode(outcome); err != nil {
			return err
		}
	}
	if len(outcome.Errors) > 0 {
		return fmt.Errorf("%d of %d feature(s) failed; re-run --apply to finish them", len(outcome.Errors), len(r.Features))
	}
	return nil
}

func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// stampOutcome is what stampCost did, for the caller to word (F-0402).
type stampOutcome struct {
	report cost.Report
	kind   stampKind
}

type stampKind int

const (
	stampWritten      stampKind = iota // a new or changed '## Custo' went to the backend
	stampKeptHigher                    // the anchor already covers more than the transcripts
	stampLowerRefused                  // cost.Stamp refused to lower the anchor
	stampUnchanged                     // the result already carried this stamp
)

// completeStampLine words a stamp outcome after a complete: the line that is
// printed, always followed by exit 0 (F-0402 crit. 3).
func completeStampLine(id string, o stampOutcome, err error, done bool) string {
	// A done card refuses `cerne cost --stamp` (B-0090): no advice to run it.
	rerun := fmt.Sprintf("; run 'cerne cost %s --stamp'", id)
	elsewhere := "Work done on another machine is stamped there: cerne cost " + id + " --stamp"
	if done {
		rerun = ""
		elsewhere = "Nothing can count it later: this card's cost can no longer be stamped" // the other machine's stamp is refused too
	}
	switch {
	case errors.Is(err, cost.ErrNothingToStamp):
		// B-0070: "another machine" alone sent the agent away from an unbound
		// session that ran right here. Attribution is temporal, so a bind made
		// now counts nothing already done: say so, and how to bind next time.
		return fmt.Sprintf("warning: cost not stamped: no session on this machine is bound to %s, so the work done here is not counted. A session binds when it starts: 'cerne feature start %s', or 'cerne work start \"%s: <what>\" --ref %s' when it continues another session's work. %s", id, id, id, id, elsewhere)
	case errors.Is(err, cost.ErrUnsupportedHarness):
		// --stamp would fail the same way (B-0055): advise no command.
		return "warning: cost not stamped: this session ran under a harness cerne cost cannot read (Claude Code transcripts only); nothing to run"
	case err != nil:
		return fmt.Sprintf("warning: cost not stamped: %s%s", strings.Join(strings.Fields(err.Error()), " "), rerun)
	case o.kind == stampLowerRefused:
		return "warning: cost not stamped: the stamped total is higher than this machine's sessions; nothing written"
	case o.kind != stampWritten:
		return "cost already stamped"
	}
	total := o.report.Counters
	if o.report.Total != nil {
		total = *o.report.Total
	}
	line := fmt.Sprintf("cost stamped: %.0f weighted tokens", cost.Weighted(total))
	// An epic's headline is own plus children, the role shares are own only:
	// no shares rather than shares of another base.
	if whole := cost.Weighted(o.report.Counters); o.report.Total == nil && len(o.report.ByRole) > 0 && whole > 0 {
		shares := make([]string, 0, len(o.report.ByRole))
		for _, role := range slices.Sorted(maps.Keys(o.report.ByRole)) {
			shares = append(shares, fmt.Sprintf("%s %.0f%%", role, 100*cost.Weighted(o.report.ByRole[role])/whole))
		}
		line += " (" + strings.Join(shares, ", ") + ")"
	}
	return line
}
