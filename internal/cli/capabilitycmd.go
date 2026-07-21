package cli

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/llm"
	"gitlab.com/rockt/cerne/internal/store"
)

func newCapabilityCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "capability",
		Short: "Manage the product-capability taxonomy (the functional tree of the product)",
	}
	cmd.PersistentFlags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.PersistentFlags().MarkHidden("dir")
	cmd.AddCommand(newCapabilityNewCmd(&dir))
	cmd.AddCommand(newCapabilityListCmd(&dir))
	cmd.AddCommand(newCapabilityRetireCmd(&dir))
	cmd.AddCommand(newCapabilityBootstrapCmd(&dir))
	return cmd
}

func newCapabilityBootstrapCmd(dir *string) *cobra.Command {
	var apply bool
	cmd := &cobra.Command{
		Use:   "bootstrap",
		Short: "Propose the capability tree from the feature specs via the [llm] provider (--apply imports the reviewed proposal)",
		Long: "Reads every live/delivered feature (title + spec excerpt) and asks the configured [llm] provider to " +
			"propose the product-capability tree, written as a REVIEWABLE artifact under .docs/.capability-proposal/ " +
			"(json + md) — the backend is not touched. Review/edit the proposal, then run with --apply to import it " +
			"in one commit (idempotent by normalized title; actor 'capability-bootstrap'). Requires a callable [llm] " +
			"mode in .cernerc (claude-cli or byo-key) for the proposal step; --apply needs no provider.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := resolveProject(*dir)
			if err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			if apply {
				res, err := app.ApplyCapabilityProposal(ctx.repoDir, ctx.projDir)
				if err != nil {
					return err
				}
				fmt.Fprintf(w, "Applied: %d capability(ies) created, %d feature link(s).\n", res.Created, res.Linked)
				return nil
			}
			provider, err := llm.NewProvider(ctx.cfg.LLM)
			if err != nil {
				return fmt.Errorf("capability bootstrap needs a callable [llm] mode in .cernerc (claude-cli or byo-key): %w", err)
			}
			prop, err := app.GenerateCapabilityProposal(cmd.Context(), ctx.repoDir, ctx.projDir, provider)
			if err != nil {
				return err
			}
			fmt.Fprintf(w, "Proposal written to .docs/.capability-proposal/ (%d root node(s)).\nReview/edit proposal.json, then run 'cerne capability bootstrap --apply'.\n", len(prop.Tree))
			return nil
		},
	}
	cmd.Flags().BoolVar(&apply, "apply", false, "import the reviewed proposal into the backend")
	return cmd
}

func newCapabilityNewCmd(dir *string) *cobra.Command {
	var parent string
	cmd := &cobra.Command{
		Use:   "new <title>",
		Short: "Create a capability node (optionally under --parent C-NNNN)",
		Long: "Mints the next C- id as an active node of the product-capability tree (F-0188) — the functional " +
			"axis features attach to via 'cerne feature capability'. --parent must exist and keep the tree acyclic; " +
			"a validation error writes nothing.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := resolveProject(*dir)
			if err != nil {
				return err
			}
			c, err := app.CreateCapability(ctx.repoDir, ctx.projDir, args[0], parent)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s created (active) — %s\n", c.ID, c.Title)
			return nil
		},
	}
	cmd.Flags().StringVar(&parent, "parent", "", "parent capability (C-NNNN) this node refines")
	return cmd
}

func newCapabilityRetireCmd(dir *string) *cobra.Command {
	var reason string
	cmd := &cobra.Command{
		Use:   "retire <id>",
		Short: "Retire a capability (--reason required; refuses with active children or linked features)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := resolveProject(*dir)
			if err != nil {
				return err
			}
			c, err := app.RetireCapability(ctx.repoDir, ctx.projDir, args[0], reason)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s: active -> superseded\n", c.ID)
			return nil
		},
	}
	cmd.Flags().StringVar(&reason, "reason", "", "why the capability is being retired (required)")
	return cmd
}

// newFeatureCapabilityCmd is the feature-side link: 'cerne feature capability
// F-NNNN C-A[,C-B]' sets the validated list; '--clear' removes it.
func newFeatureCapabilityCmd(dir *string) *cobra.Command {
	var clear bool
	cmd := &cobra.Command{
		Use:   "capability <feature-id> [C-ids]",
		Short: "Link a feature to capability nodes (comma-separated C- ids; --clear removes)",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := resolveProject(*dir)
			if err != nil {
				return err
			}
			if !clear && len(args) < 2 {
				return fmt.Errorf("provide the capability ids (C-A,C-B) or --clear")
			}
			var ids []string
			if !clear {
				ids = splitInforms(args[1])
			}
			f, err := app.LinkFeatureCapabilities(ctx.repoDir, ctx.projDir, args[0], ids, clear)
			if err != nil {
				return err
			}
			if clear {
				fmt.Fprintf(cmd.OutOrStdout(), "%s: capabilities cleared\n", f.ID)
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "%s: capabilities set (%d)\n", f.ID, len(f.Capabilities))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&clear, "clear", false, "remove every capability link from the feature")
	return cmd
}

func newCapabilityListCmd(dir *string) *cobra.Command {
	var tree, all, asJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the capability taxonomy (--tree for hierarchy, --json for machines)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCapabilityList(cmd, *dir, tree, all, asJSON)
		},
	}
	cmd.Flags().BoolVar(&tree, "tree", false, "render the hierarchy indented")
	cmd.Flags().BoolVar(&all, "all", false, "include retired (superseded) capabilities")
	cmd.Flags().BoolVar(&asJSON, "json", false, "machine-readable JSON output")
	return cmd
}

type capabilityRow struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Parent   string `json:"parent,omitempty"`
	State    string `json:"state"`
	Features int    `json:"features"`
}

func runCapabilityList(cmd *cobra.Command, dir string, tree, all, asJSON bool) error {
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	caps, err := store.LoadAllCapabilities(ctx.repoDir)
	if err != nil {
		return err
	}
	features, err := store.LoadAllFeatures(ctx.repoDir)
	if err != nil {
		return err
	}
	counts := app.CountFeaturesByCapability(features)

	shown := make([]store.Capability, 0, len(caps))
	for _, c := range caps {
		if !all && c.State != domain.CapabilityActive {
			continue
		}
		shown = append(shown, c)
	}
	w := cmd.OutOrStdout()

	if asJSON {
		rows := make([]capabilityRow, 0, len(shown))
		for _, c := range shown {
			rows = append(rows, capabilityRow{c.ID, c.Title, c.Parent, string(c.State), counts[c.ID]})
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(struct {
			Capabilities []capabilityRow `json:"capabilities"`
		}{rows})
	}

	if len(shown) == 0 {
		fmt.Fprintln(w, "No capabilities yet. Use 'cerne capability new \"<title>\"' (or 'cerne capability bootstrap') to start the tree.")
		return nil
	}

	line := func(c store.Capability, depth int) {
		indent := ""
		for range depth {
			indent += "  "
		}
		fmt.Fprintf(w, "%s%s  [%s]  %s  · %d feature(s)\n", indent, c.ID, c.State, c.Title, counts[c.ID])
	}
	if !tree {
		for _, c := range shown {
			line(c, 0)
		}
		return nil
	}
	children := map[string][]store.Capability{}
	roots := []store.Capability{}
	inShown := map[string]bool{}
	for _, c := range shown {
		inShown[c.ID] = true
	}
	for _, c := range shown {
		if c.Parent != "" && inShown[c.Parent] {
			children[c.Parent] = append(children[c.Parent], c)
		} else {
			roots = append(roots, c)
		}
	}
	sortRows := func(rows []store.Capability) {
		sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	}
	sortRows(roots)
	var walk func(c store.Capability, depth int)
	walk = func(c store.Capability, depth int) {
		line(c, depth)
		kids := children[c.ID]
		sortRows(kids)
		for _, k := range kids {
			walk(k, depth+1)
		}
	}
	for _, r := range roots {
		walk(r, 0)
	}
	return nil
}
