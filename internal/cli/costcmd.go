package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/cost"
	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
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
			"apart. Without <id>, the project view per entity. Tokens only — no monetary value. Not " +
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
	return cmd
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
	if f.State != domain.StateImplemented {
		return fmt.Errorf("%w: %s is %s — run 'cerne feature complete %s' first; the stamp records the closed cost, before the human accept",
			cost.ErrStampNeedsImplemented, id, f.State, id)
	}
	cacheDir := filepath.Dir(ctx.repoDir)
	r, err := cost.Compute(cost.Inputs{CacheDir: cacheDir, RepoDir: ctx.repoDir, Roots: claudeRoots()}, id)
	if err != nil {
		return err
	}
	if r.Sessions == 0 && (r.Children == nil || len(r.Children.Stamped) == 0) {
		return fmt.Errorf("%w for %s", cost.ErrNothingToStamp, id)
	}
	raw, err := os.ReadFile(app.FeatureDocPath(ctx.repoDir, id, "result.md"))
	if err != nil {
		return err
	}
	body, err := cost.Stamp(string(raw), r)
	if errors.Is(err, cost.ErrStampWouldLower) {
		fmt.Fprintf(cmd.OutOrStdout(), "%s cost unchanged — %v\n", id, err)
		return nil
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
	fmt.Fprintf(out, "%s: tokens from Claude Code transcripts%s (no monetary value)\n", name, scope)
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
	fmt.Fprintf(out, "  of which sidechain (subagents): %s\n", fmtCounters(r.Sidechain))
	fmt.Fprintln(out, "By model:")
	for _, m := range sortedKeys(r.ByModel) {
		fmt.Fprintf(out, "  %s  %s\n", m, fmtCounters(r.ByModel[m]))
	}
	if r.ID == "" {
		fmt.Fprintln(out, "By entity:")
		for _, e := range sortedKeys(r.Entities) {
			fmt.Fprintf(out, "  %s  %s\n", e, fmtCounters(r.Entities[e]))
		}
	}
	fmt.Fprintf(out, "Sessions: %d (%d without transcript) · messages: %d\n", r.Sessions, r.SessionsWithoutTranscript, r.Messages)
	if r.First != "" {
		fmt.Fprintf(out, "Window: %s → %s\n", r.First, r.Last)
	} else {
		fmt.Fprintln(out, "Window: — (no attributed message)")
	}
	fmt.Fprintf(out, "Unattributed (messages in these sessions outside any binding): %s\n", fmtCounters(r.Unattributed))
	if r.ID == "" {
		a, u := r.Counters, r.Unattributed
		fmt.Fprintf(out, "Total (attributed + unattributed): %s\n", fmtCounters(cost.Counters{Input: a.Input + u.Input,
			CacheCreation: a.CacheCreation + u.CacheCreation, CacheRead: a.CacheRead + u.CacheRead, Output: a.Output + u.Output}))
	}
	renderRollup(out, r)
}

// renderRollup prints an epic's children share and its total.
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
	fmt.Fprintf(out, "Children (sum of their stamped anchors): %s\n", fmtCounters(r.Children.Counters))
	fmt.Fprintf(out, "  stamped: %s\n", list(r.Children.Stamped))
	fmt.Fprintf(out, "  without anchor: %s\n", list(r.Children.Missing))
	fmt.Fprintf(out, "Total (own + children): %s\n", fmtCounters(*r.Total))
}
