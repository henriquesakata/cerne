package cli

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/slug"
	"gitlab.com/rockt/cerne/internal/store"
	"gitlab.com/rockt/cerne/internal/verbs"
)

func newPendingCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "pending",
		Short: "Register and triage pendings (P-): known delivery debts of a feature, task or epic",
		Long: "A pending is a known debt left by a concrete delivery (always with an origin, --from): what a " +
			"result.md would otherwise bury in prose. Open pendings reappear in 'cerne next' and age into the " +
			"advisory lint finding stale-pending. A pending never blocks a lifecycle transition.",
	}
	cmd.PersistentFlags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.PersistentFlags().MarkHidden("dir")
	cmd.AddCommand(verbs.MarkChat(newPendingNewCmd(&dir)))
	cmd.AddCommand(verbs.MarkChat(newPendingListCmd(&dir)))
	cmd.AddCommand(verbs.MarkChat(newPendingShowCmd(&dir)))
	cmd.AddCommand(verbs.MarkLocal(newPendingResolveCmd(&dir)))
	cmd.AddCommand(verbs.MarkLocal(newPendingDropCmd(&dir)))
	cmd.AddCommand(verbs.MarkLocal(newPendingPromoteCmd(&dir)))
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
	cmd.Flags().StringVar(&state, "state", string(domain.PendingOpen), "open, resolved, dropped or promoted")
	cmd.Flags().StringVar(&from, "from", "", "only pendings of this origin id")
	cmd.Flags().StringVar(&component, "component", "", "only pendings tagged with this component")
	cmd.Flags().StringVar(&module, "module", "", "only pendings whose origin belongs to this module")
	cmd.Flags().BoolVar(&asJSON, "json", false, "machine-readable JSON output")
	return cmd
}

func runPendingList(cmd *cobra.Command, dir, state, from, component, module string, asJSON bool) error {
	if states, _ := domain.ValidStates("pending"); !slices.Contains(states, state) {
		return fmt.Errorf("--state %q: want one of %s", state, strings.Join(states, ", "))
	}
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	pendings, err := store.LoadAllPendings(ctx.repoDir)
	if err != nil {
		return err
	}
	moduleOf := map[string]string{}
	if module != "" {
		features, err := store.LoadAllFeatures(ctx.repoDir)
		if err != nil {
			return err
		}
		for _, f := range features {
			moduleOf[f.ID] = f.Module
		}
	}
	comp := slug.Make(component)
	from = strings.ToUpper(strings.TrimSpace(from)) // origins are stored canonical
	shown := []store.Pending{}
	for _, p := range pendings {
		if string(p.State) != state || (from != "" && p.From != from) ||
			(comp != "" && !slices.Contains(p.Components, comp)) || (module != "" && moduleOf[p.From] != module) {
			continue
		}
		shown = append(shown, p)
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
		line := fmt.Sprintf("%s  %s  %s  %s", p.ID, pendingAge(now, p), p.From, p.Title)
		if p.PromotedTo != "" {
			line += "  → " + p.PromotedTo
		}
		fmt.Fprintln(w, line)
	}
	if len(shown) == 0 {
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
			return runShow(cmd, *dir, args[0], "", asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "machine-readable JSON output")
	return cmd
}

func newPendingResolveCmd(dir *string) *cobra.Command {
	return &cobra.Command{
		Use:   "resolve <id>",
		Short: "Close an open pending as paid (open -> resolved)",
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
	var as, module string
	var force bool
	cmd := &cobra.Command{
		Use:   "promote <id>",
		Short: "Turn an open pending into a feature or task (open -> promoted), linked both ways",
		Long: "Creates a feature (candidate) or a task (under the origin, which must be past candidacy) with the " +
			"pending's title, tags and context; the generated spec links back with '**Origem:** [[P-NNNN]]' and " +
			"the pending records promoted_to. The new entity enters the regular funnel with its own human gates.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := resolveProject(*dir)
			if err != nil {
				return err
			}
			p, f, err := app.PromotePending(ctx.repoDir, ctx.projDir, args[0], as, module, force)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s: %s -> %s (%s)\n", p.ID, domain.PendingOpen, p.State, f.ID)
			return nil
		},
	}
	cmd.Flags().StringVar(&as, "as", "", "feature or task (required)")
	cmd.Flags().StringVar(&module, "module", "", "module of the created entity (default: the origin's)")
	cmd.Flags().BoolVar(&force, "force", false, "bypass the created entity's duplicate-registration guard (F-0147)")
	return cmd
}
