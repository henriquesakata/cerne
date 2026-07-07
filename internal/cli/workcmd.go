package cli

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

func newWorkCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "work",
		Short: "Register and manage agent activity (transient work-in-progress) in the backend",
	}
	cmd.PersistentFlags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.PersistentFlags().MarkHidden("dir")
	cmd.AddCommand(newWorkStartCmd(&dir))
	cmd.AddCommand(newWorkDoneCmd(&dir))
	cmd.AddCommand(newWorkAbandonCmd(&dir))
	cmd.AddCommand(newWorkListCmd(&dir))
	cmd.AddCommand(newWorkCurrentCmd(&dir))
	return cmd
}

func newWorkStartCmd(dir *string) *cobra.Command {
	var ref, agent, paths string
	var force bool
	cmd := &cobra.Command{
		Use:   "start <description>",
		Short: "Register a new active agent activity (work-in-progress)",
		Long: "Registers a transient activity (not a feature) — investigating an error, reviewing an MR — as " +
			"active in the backend, so another agent (this machine, another, or a teammate) can see it's already " +
			"being worked and not duplicate it. Actor defaults to '<user>@<hostname>'; --agent overrides it. " +
			"--ref links the activity to existing features/decisions (F-/D- ids; unknown id aborts, nothing written). " +
			"--paths declares the path globs the activity intends to touch (e.g. internal/server/**); an overlap " +
			"with another active work prints a warning (F-0151) — the conflict check becomes part of registering.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkStart(cmd, *dir, args[0], ref, agent, paths, force)
		},
	}
	cmd.Flags().StringVar(&ref, "ref", "", "comma-separated F-/D- ids this activity relates to")
	cmd.Flags().StringVar(&agent, "agent", "", "actor name (defaults to '<user>@<hostname>')")
	cmd.Flags().StringVar(&paths, "paths", "", "comma-separated path globs this activity intends to touch")
	cmd.Flags().BoolVar(&force, "force", false, "bypass the duplicate-registration guard (F-0147) for a consciously distinct problem")
	return cmd
}

func runWorkStart(cmd *cobra.Command, dir, title, ref, agent, paths string, force bool) error {
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	declared := splitInforms(paths)
	// Path-conflict check (F-0151 crit. 6): computed against the board BEFORE
	// this work lands, so it never matches itself. Warning only — shared files
	// are often legitimate; the title guard (F-0147) is the blocking one.
	overlaps, err := app.FindPathOverlap(ctx.repoDir, declared)
	if err != nil {
		return err
	}
	w, err := app.StartWork(ctx.repoDir, ctx.projDir, title, splitInforms(ref), agent, force, declared)
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s criada (active) — %s (ator: %s)\n", w.ID, w.Title, w.Actor)
	for _, m := range overlaps {
		fmt.Fprintf(cmd.OutOrStdout(), "warning: paths overlap active %s (%s) on %s — coordinate before touching shared files\n",
			m.ID, m.Title, joinRefs(m.Globs))
	}
	return nil
}

func newWorkDoneCmd(dir *string) *cobra.Command {
	var note string
	cmd := &cobra.Command{
		Use:   "done <id>",
		Short: "Close an active activity as done",
		Long:  "Moves an active work to done (terminal) — the record stays as queryable history. --note captures the outcome.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkFinish(cmd, *dir, args[0], note, false, "")
		},
	}
	cmd.Flags().StringVar(&note, "note", "", "closing note (the outcome)")
	return cmd
}

func newWorkAbandonCmd(dir *string) *cobra.Command {
	var reason string
	cmd := &cobra.Command{
		Use:   "abandon <id>",
		Short: "Close an active activity as abandoned (--reason required)",
		Long:  "Moves an active work to abandoned (terminal) — the record stays as queryable history. --reason is required.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkFinish(cmd, *dir, args[0], "", true, reason)
		},
	}
	cmd.Flags().StringVar(&reason, "reason", "", "why the activity was abandoned (required)")
	return cmd
}

func runWorkFinish(cmd *cobra.Command, dir, id, note string, abandon bool, reason string) error {
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	out, err := app.FinishWork(ctx.repoDir, ctx.projDir, id, note, abandon, reason)
	if err != nil {
		return err
	}
	// Session bindings pointing at a closed work are dead (F-0151 crit. 5);
	// best-effort — CurrentBinding self-heals stale files anyway.
	if err := app.ClearBindings(filepath.Dir(ctx.repoDir), out.ID); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: clearing session bindings: %v\n", err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s: %s -> %s\n", out.ID, out.From, out.To)
	return nil
}

func newWorkCurrentCmd(dir *string) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "current <session-id>",
		Short: "Show the live work/feature bound to a Claude Code session (F-0151)",
		Long: "Resolves the session binding written by the work-bind hook: the W-/F- id the session registered " +
			"via 'cerne work start'/'cerne feature start'. A binding whose entity is no longer live (done, " +
			"abandoned, implemented) reads as absent. Local-only — never touches the network.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkCurrent(cmd, *dir, args[0], asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "machine-readable JSON output")
	return cmd
}

func runWorkCurrent(cmd *cobra.Command, dir, sess string, asJSON bool) error {
	ctx, err := resolvePaths(dir) // no origin pull: the gate path must stay fast and offline
	if err != nil {
		return err
	}
	id, bound, err := app.CurrentBinding(filepath.Dir(ctx.repoDir), ctx.repoDir, sess)
	if err != nil {
		return err
	}
	w := cmd.OutOrStdout()
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(struct {
			Session string `json:"session"`
			Bound   bool   `json:"bound"`
			ID      string `json:"id,omitempty"`
		}{sess, bound, id})
	}
	if bound {
		fmt.Fprintf(w, "%s -> %s\n", sess, id)
	} else {
		fmt.Fprintf(w, "no live binding for session %s\n", sess)
	}
	return nil
}

func newWorkListCmd(dir *string) *cobra.Command {
	var all, asJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List agent activity — active by default; --all includes recently terminated",
		Long: "Lists work read straight from the backend store — the 'is someone already on this?' check before " +
			"opening another agent. Active by default; --all also shows done/abandoned. --json for machine output.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkList(cmd, *dir, all, asJSON)
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "include terminated (done/abandoned) activity")
	cmd.Flags().BoolVar(&asJSON, "json", false, "machine-readable JSON output")
	return cmd
}

func runWorkList(cmd *cobra.Command, dir string, all, asJSON bool) error {
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	works, err := store.LoadAllWorks(ctx.repoDir)
	if err != nil {
		return err
	}
	w := cmd.OutOrStdout()
	shown := make([]store.Work, 0, len(works))
	for _, wk := range works {
		if !all && wk.State != domain.WorkActive {
			continue
		}
		shown = append(shown, wk)
	}

	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(struct {
			Works []store.Work `json:"works"`
		}{shown})
	}

	now := time.Now().UTC()
	for _, wk := range shown {
		line := fmt.Sprintf("%s  [%s]  %s  ator: %s  idade: %s", wk.ID, wk.State, wk.Title, wk.Actor, formatAge(now.Sub(wk.UpdatedAt)))
		if len(wk.Refs) > 0 {
			line += "  refs: " + joinRefs(wk.Refs)
		}
		fmt.Fprintln(w, line)
	}
	if len(shown) == 0 {
		if all {
			fmt.Fprintln(w, "Nenhuma atividade registrada. Use 'cerne work start \"<descrição>\"' para registrar.")
		} else {
			fmt.Fprintln(w, "Nenhuma atividade ativa. Use 'cerne work start \"<descrição>\"' para registrar, ou 'cerne work list --all' para ver história.")
		}
	}
	return nil
}

// formatAge renders a duration compactly (e.g. "3h", "2d") for the list table.
func formatAge(d time.Duration) string {
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

func joinRefs(refs []string) string {
	out := ""
	for i, r := range refs {
		if i > 0 {
			out += ","
		}
		out += r
	}
	return out
}
