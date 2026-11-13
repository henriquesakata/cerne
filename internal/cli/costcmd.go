package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
		Short: "Tokens an entity consumed, read from the Claude Code session transcripts (F-0309)",
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
		return fmt.Errorf("cost attribute takes a feature, epic or task id (F-/E-/T-), got %q", id)
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
	f, err := store.LoadFeature(ctx.repoDir, id)
	if err != nil {
		return err
	}
	cacheDir := filepath.Dir(ctx.repoDir)
	r, err := cost.PrecheckStamp(cost.Inputs{CacheDir: cacheDir, RepoDir: ctx.repoDir, Roots: claudeRoots()}, f.State, id)
	if err != nil {
		return err
	}
	// The anchor already holds more than the transcripts: keep it, and drop the
	// snapshot — it can only hold less (C12).
	if r.Source == "stamp" {
		fmt.Fprintf(cmd.OutOrStdout(), "%s cost unchanged — its stamp already covers more messages (%d) than the transcripts still hold.\n", id, r.Messages)
		return cost.DropSnapshot(cacheDir, id)
	}
	raw, err := os.ReadFile(app.FeatureDocPath(ctx.repoDir, id, "result.md"))
	if err != nil {
		return err
	}
	body, err := cost.Stamp(string(raw), r)
	if errors.Is(err, cost.ErrStampWouldLower) {
		fmt.Fprintf(cmd.OutOrStdout(), "%s cost unchanged — %v\n", id, err)
		return cost.DropSnapshot(cacheDir, id)
	}
	if err != nil {
		return err
	}
	_, changed, err := app.AmendFeatureResult(ctx.repoDir, ctx.projDir, id, body, "cost stamp (F-0309)")
	if err != nil {
		return err
	}
	if err := cost.DropSnapshot(cacheDir, id); err != nil {
		return err
	}
	if changed {
		fmt.Fprintf(cmd.OutOrStdout(), "✓ %s cost stamped — '## Custo' in its result (cerne show %s --doc result).\n", id, id)
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "%s cost unchanged — the result already carries this stamp.\n", id)
	}
	return nil
}

func runCost(cmd *cobra.Command, dir, id string, asJSON bool) error {
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	if id != "" {
		if !domain.IsFeatureID(id) {
			return fmt.Errorf("cost takes a feature, epic or task id (F-/E-/T-), got %q", id)
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
			"the feature its parent session was bound to — never by what its prompt says. Without --apply it only " +
			"reports: one line per feature with the agents and weighted tokens per role, then what was left out " +
			"(unattributed, ambiguous, already with roles, no anchor, inconsistent). --apply records a retro bind " +
			"with the role per subagent and completes the anchor: an implemented feature's '## Custo' in its " +
			"result, a done feature's as an erratum carrying the same block. The anchor's totals never go down; " +
			"critic plus judge above them skips the feature as inconsistent. A second run writes nothing. " +
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
	for _, f := range r.Features {
		var critics, judges int
		for _, a := range f.Agents {
			if a.Role == "critic" {
				critics++
			} else {
				judges++
			}
		}
		fmt.Fprintf(out, "%s  %s  critic %s, %.0f tokens · judge %s, %.0f tokens\n", f.ID, f.State,
			plural(critics, "agent"), cost.Weighted(f.Critic), plural(judges, "agent"), cost.Weighted(f.Judge))
	}
	for _, a := range r.Ambiguous {
		fmt.Fprintf(out, "ambiguous: %s/%s (%s) falls on %s\n", a.Session, a.Agent, a.Role, strings.Join(a.Entities, ", "))
	}
	fmt.Fprintf(out, "Left out: unattributed %d · ambiguous %d · already with roles %d · no anchor %d · inconsistent %d\n",
		r.Unattributed, len(r.Ambiguous), len(r.HasRoles), len(r.NoAnchor), len(r.Inconsistent))
	if len(r.Features) == 0 {
		fmt.Fprintln(out, "nothing to backfill")
		return nil
	}
	if !apply {
		fmt.Fprintln(out, "Nothing written. Run with --apply to record these roles and complete the anchors.")
		return nil
	}
	for _, f := range r.Features {
		text, asResult, err := cost.RolesEdit(ctx.repoDir, f.ID, f.State == domain.StateDone, f.Critic, f.Judge)
		if err != nil {
			return err
		}
		// The document first: if a bind fails afterwards, a re-run finds the
		// roles in the anchor and leaves the feature alone.
		where := "erratum"
		if asResult {
			where = "result"
			_, _, err = app.AmendFeatureResult(ctx.repoDir, ctx.projDir, f.ID, text, "cost roles backfill (F-0396)")
		} else {
			_, err = app.AddErrata(ctx.repoDir, ctx.projDir, f.ID, text, app.EventActor())
		}
		if err != nil {
			return fmt.Errorf("%s: %w", f.ID, err)
		}
		now := time.Now().UTC()
		for _, a := range f.Agents {
			if err := cost.AppendBind(cacheDir, cost.Bind{Session: a.Session, Agent: a.Agent, Entity: f.ID, Harness: cost.HarnessClaudeCode, Role: a.Role, Retro: true, TS: now}); err != nil {
				return err
			}
		}
		fmt.Fprintf(out, "✓ %s: %s bound, anchor completed in the %s\n", f.ID, plural(len(f.Agents), "agent"), where)
	}
	return nil
}

func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
