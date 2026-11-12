package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
	"github.com/henriquesakata/cerne/internal/verbs"
)

func newPendingCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "pending",
		Short: "Register and triage pendings (P-): known delivery debts of a feature, task or epic",
		Long: "A pending is a known debt left by a concrete delivery (always with an origin, --from): what a " +
			"result.md would otherwise bury in prose. Open pendings reappear in 'cerne next' and age into the " +
			"advisory lint finding stale-pending. An open pending never blocks a lifecycle transition. A pending " +
			"promoted to (or linked with) a feature follows it (F-0885): the accept resolves it, a reject reopens " +
			"it, a 'substituir' moves it to the successor, and the complete refuses a result that does not cite it.",
	}
	cmd.PersistentFlags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.PersistentFlags().MarkHidden("dir")
	cmd.AddCommand(verbs.MarkChat(newPendingNewCmd(&dir)))
	cmd.AddCommand(verbs.MarkChat(newPendingListCmd(&dir)))
	cmd.AddCommand(verbs.MarkChat(newPendingShowCmd(&dir)))
	cmd.AddCommand(verbs.MarkLocal(newPendingResolveCmd(&dir)))
	cmd.AddCommand(verbs.MarkLocal(newPendingDropCmd(&dir)))
	cmd.AddCommand(verbs.MarkLocal(newPendingPromoteCmd(&dir)))
	cmd.AddCommand(verbs.MarkLocal(newPendingLinkCmd(&dir)))
	cmd.AddCommand(verbs.MarkLocal(newPendingUnlinkCmd(&dir)))
	return cmd
}

func newPendingNewCmd(dir *string) *cobra.Command {
	var from, file, facet, component string
	var force bool
	cmd := &cobra.Command{
		Use:   "new <title>",
		Short: "Register an open pending against the delivery that left it (--from required)",
		Long: "Registers a known debt as an open pending. --from (the F-/T-/E- id that left it) is mandatory and " +
			"must exist. The optional body (context) comes from --file or piped stdin. Without --facet/--component " +
			"the pending inherits the origin's tags, per axis. An open pending of the same origin with a similar " +
			"title refuses the creation (F-0147 guard); --force overrides it.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := readOptionalBody(cmd, file)
			if err != nil {
				return err
			}
			ctx, err := resolveProject(*dir)
			if err != nil {
				return err
			}
			p, err := app.CreatePending(ctx.repoDir, ctx.projDir, args[0], body, from, splitInforms(facet), splitInforms(component), force)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s created (open) from %s — %s\n", p.ID, p.From, p.Title)
			return nil
		},
	}
	cmd.Flags().StringVar(&from, "from", "", "the F-/T-/E- id whose delivery left this debt (required)")
	cmd.Flags().StringVar(&file, "file", "", "file with the pending's context (defaults to piped stdin)")
	cmd.Flags().StringVar(&facet, "facet", "", "facets (CSV; closed set: infra,code,data,process,auth); default: the origin's")
	cmd.Flags().StringVar(&component, "component", "", "components (CSV); default: the origin's")
	cmd.Flags().BoolVar(&force, "force", false, "bypass the duplicate-registration guard for a consciously distinct pending")
	return cmd
}

func newPendingListCmd(dir *string) *cobra.Command {
	var state, from, component, module string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List pendings — open by default, one per line: id, age in days, origin, title",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPendingList(cmd, *dir, state, from, component, module, asJSON)
		},
	}
	cmd.Flags().StringVar(&state, "state", string(domain.PendingOpen), "open, resolved, dropped, promoted or all (all adds each line's state before the title)")
	cmd.Flags().StringVar(&from, "from", "", "only pendings of this origin id")
	cmd.Flags().StringVar(&component, "component", "", "only pendings tagged with this component")
	cmd.Flags().StringVar(&module, "module", "", "only pendings whose origin belongs to this module")
	cmd.Flags().BoolVar(&asJSON, "json", false, "machine-readable JSON output")
	return cmd
}

func runPendingList(cmd *cobra.Command, dir, state, from, component, module string, asJSON bool) error {
	if !app.ValidPendingState(state) { // before the project, as always; "" is refused, not read as open
		return fmt.Errorf("--state %q: %w", state, app.ErrPendingStateInvalid)
	}
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	shown, err := app.ListPendings(ctx.repoDir, app.PendingFilter{State: state, From: from, Component: component, Module: module})
	if err != nil {
		return err
	}
	w := cmd.OutOrStdout()
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(struct {
			Pendings []store.Pending `json:"pendings"`
		}{shown})
	}
	now := time.Now().UTC()
	for _, p := range shown {
		title := p.Title
		if state == "all" { // mixed states: each line says its own
			title = string(p.State) + "  " + title
		}
		line := fmt.Sprintf("%s  %s  %s  %s", p.ID, pendingAge(now, p), p.From, title)
		if p.PromotedTo != "" {
			line += "  → " + p.PromotedTo
		}
		fmt.Fprintln(w, line)
	}
	if len(shown) == 0 && state == "all" {
		fmt.Fprintln(w, "No pendings.")
	} else if len(shown) == 0 {
		fmt.Fprintf(w, "No %s pendings.\n", state)
	}
	return nil
}

// pendingAge is the whole days a pending has existed, e.g. "12d".
func pendingAge(now time.Time, p store.Pending) string {
	return fmt.Sprintf("%dd", int(now.Sub(p.CreatedAt).Hours()/24))
}

func newPendingShowCmd(dir *string) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "show <id>",
		Short: "Show one pending (same as 'cerne show P-NNNN')",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !strings.HasPrefix(args[0], "P-") {
				return fmt.Errorf("%q is not a pending id (P-NNNN): use 'cerne show'", args[0])
			}
			return runShow(cmd, *dir, args[0], "", asJSON, false)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "machine-readable JSON output")
	return cmd
}

func newPendingResolveCmd(dir *string) *cobra.Command {
	return &cobra.Command{
		Use:   "resolve <id>",
		Short: "Close an open pending as paid (open -> resolved); a linked one is resolved by its feature's accept",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := resolveProject(*dir)
			if err != nil {
				return err
			}
			p, err := app.ResolvePending(ctx.repoDir, ctx.projDir, args[0])
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s: %s -> %s\n", p.ID, domain.PendingOpen, p.State)
			return nil
		},
	}
}

func newPendingDropCmd(dir *string) *cobra.Command {
	var reason string
	cmd := &cobra.Command{
		Use:   "drop <id>",
		Short: "Discard an open pending (open -> dropped, --reason required)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := resolveProject(*dir)
			if err != nil {
				return err
			}
			p, err := app.DropPending(ctx.repoDir, ctx.projDir, args[0], reason)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s: %s -> %s\n", p.ID, domain.PendingOpen, p.State)
			return nil
		},
	}
	cmd.Flags().StringVar(&reason, "reason", "", "why the debt is discarded (required)")
	return cmd
}

func newPendingPromoteCmd(dir *string) *cobra.Command {
	var as, title, module string
	var force bool
	cmd := &cobra.Command{
		Use:   "promote <id>...",
		Short: "Turn one or more open pendings into the feature or task that pays them (open -> promoted)",
		Long: "Creates a feature (candidate) or a task (under the origin, which must be past candidacy) that pays " +
			"the given pendings, linked both ways: the generated spec opens with '**Origem:**' citing each " +
			"[[P-NNNN]] and its origin, and each pending records promoted_to. One pending: the entity inherits its " +
			"title, tags and context. Several (--title required, feature only when the origins differ): one " +
			"entity with a section per pending, the union of their tags and the origins' common module (or " +
			"--module). Every pending is checked before anything is created. From then on each follows the " +
			"entity: its accept resolves them, a reject reopens them, a 'substituir' moves them to the successor, " +
			"and its complete refuses a result that does not cite them. The new entity enters the regular funnel " +
			"with its own human gates.",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := resolveProject(*dir)
			if err != nil {
				return err
			}
			ps, f, err := app.PromotePendings(ctx.repoDir, ctx.projDir, args, as, title, module, force)
			if err != nil {
				return err
			}
			for _, p := range ps {
				fmt.Fprintf(cmd.OutOrStdout(), "%s: %s -> %s (%s)\n", p.ID, domain.PendingOpen, p.State, f.ID)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&as, "as", "", "feature or task (required)")
	cmd.Flags().StringVar(&title, "title", "", "title of the created entity (required for two or more pendings; default: the pending's)")
	cmd.Flags().StringVar(&module, "module", "", "module of the created entity (default: the origins' common one)")
	cmd.Flags().BoolVar(&force, "force", false, "bypass the created entity's duplicate-registration guard (F-0147)")
	return cmd
}

func newPendingLinkCmd(dir *string) *cobra.Command {
	return &cobra.Command{
		Use:   "link <P-id> <F-id>",
		Short: "Point an open pending at a live feature that pays it (open -> promoted)",
		Long: "Links an open pending to a feature still live (candidate, approved or in_development), as promote " +
			"links the one it creates; an implemented, done, rejected or superseded feature is refused. From then " +
			"on the pending follows the feature: its accept resolves it, a reject reopens it, a 'substituir' moves " +
			"it to the successor, and its complete refuses a result that does not cite it. " +
			"'cerne pending unlink' undoes the link.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := resolveProject(*dir)
			if err != nil {
				return err
			}
			p, err := app.LinkPending(ctx.repoDir, ctx.projDir, args[0], args[1])
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s: %s -> %s (%s)\n", p.ID, domain.PendingOpen, p.State, p.PromotedTo)
			return nil
		},
	}
}

func newPendingUnlinkCmd(dir *string) *cobra.Command {
	return &cobra.Command{
		Use:   "unlink <P-id>",
		Short: "Take a pending back from the feature it points at (promoted -> open)",
		Long: "Returns a promoted pending to open and clears promoted_to: the way out when a live feature " +
			"(candidate, approved or in_development) will not pay it, including a complete refused for not " +
			"citing it, and for a pending promoted before F-0885 to a feature already done, rejected or " +
			"superseded, which nothing moves any more. A pending of an implemented feature waits for the " +
			"accept (or reject) instead.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := resolveProject(*dir)
			if err != nil {
				return err
			}
			p, err := app.UnlinkPending(ctx.repoDir, ctx.projDir, args[0])
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s: %s -> %s\n", p.ID, domain.PendingPromoted, p.State)
			return nil
		},
	}
}

// printMovedPendings reports the pendings a feature transition carried along
// with it (F-0885): resolved on accept, reopened on reject, moved on
// 'substituir'.
func printMovedPendings(w io.Writer, lines []string) {
	for _, l := range lines {
		fmt.Fprintln(w, "  ↳ "+l)
	}
}
