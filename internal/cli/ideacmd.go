package cli

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
	"gitlab.com/rockt/cerne/internal/verbs"
)

func newIdeaCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "idea",
		Short: "Capture and triage ideas (I-): raw material outside the demand funnel",
		Long: "An idea is raw material nobody has evaluated yet: text and optional tags, no spec, no gates. It never " +
			"appears in lint, next, status, stats, the wiki or the board — a candidate is a demand already judged " +
			"worth evaluating, an idea is not. Drop it (kept, with an optional reason) or promote it to a candidate.",
	}
	cmd.PersistentFlags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.PersistentFlags().MarkHidden("dir")
	cmd.AddCommand(verbs.MarkChat(newIdeaNewCmd(&dir)))
	cmd.AddCommand(verbs.MarkChat(newIdeaListCmd(&dir)))
	cmd.AddCommand(verbs.MarkChat(newIdeaShowCmd(&dir)))
	cmd.AddCommand(verbs.MarkLocal(newIdeaDropCmd(&dir)))
	cmd.AddCommand(verbs.MarkLocal(newIdeaPromoteCmd(&dir)))
	return cmd
}

func newIdeaNewCmd(dir *string) *cobra.Command {
	var facet, component string
	cmd := &cobra.Command{
		Use:   "new <text…>",
		Short: "Capture an idea: the text is all it takes (tags optional)",
		Long: "Captures the words given as one text (quoting is optional). A text that starts with a dash " +
			"goes after '--': cerne idea new -- \"-x is an idea\".",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := resolveProject(*dir)
			if err != nil {
				return err
			}
			// One token per invocation, reused by the one internal retry: a
			// failure after the push lands (snapshot, unconfirmed head) is
			// retried without minting a twin (C2).
			token := rand.Text()
			create := func() (store.Idea, error) {
				return app.CreateIdea(ctx.repoDir, ctx.projDir, strings.Join(args, " "), splitInforms(facet), splitInforms(component), token)
			}
			i, err := create()
			if err != nil && !errors.Is(err, app.ErrIdeaTextRequired) && !errors.Is(err, domain.ErrUnknownFacet) {
				i, err = create()
			}
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s created (%s) — %s\n", i.ID, i.State, ideaFirstLine(i.Text))
			return nil
		},
	}
	cmd.Flags().StringVar(&facet, "facet", "", "facets (CSV; closed set: infra,code,data,process,auth)")
	cmd.Flags().StringVar(&component, "component", "", "components (CSV)")
	// A dash-leading text parses as a flag: say how to pass it (C15).
	cmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return fmt.Errorf("%w — a text that starts with a dash goes after '--': cerne idea new -- \"<text>\"", err)
	})
	return cmd
}

func newIdeaListCmd(dir *string) *cobra.Command {
	var all, asJSON bool
	var search string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List ideas — active by default, newest first: id, date, state, first line",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := resolveProject(*dir)
			if err != nil {
				return err
			}
			ideas, err := app.ListIdeas(ctx.repoDir, all, search)
			if err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			if asJSON {
				enc := json.NewEncoder(w)
				enc.SetIndent("", "  ")
				return enc.Encode(struct {
					Ideas []store.Idea `json:"ideas"`
				}{ideas})
			}
			for _, i := range ideas {
				line := fmt.Sprintf("%s  %s  %s  %s", i.ID, i.CreatedAt.Format(time.DateOnly), i.State, ideaFirstLine(i.Text))
				switch {
				case i.PromotedTo != "":
					line += "  → " + i.PromotedTo
				case i.Reason != "":
					line += "  — " + i.Reason
				}
				fmt.Fprintln(w, line)
			}
			if len(ideas) == 0 {
				fmt.Fprintln(w, "No ideas.")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "include dropped and promoted ideas")
	cmd.Flags().StringVar(&search, "search", "", "only ideas whose text contains every word of this term, in any order (case- and accent-insensitive)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "machine-readable JSON output")
	return cmd
}

// ideaFirstLine is the first non-blank line of an idea's text, capped at 100
// runes — what one list line shows.
func ideaFirstLine(text string) string {
	for _, l := range strings.Split(text, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			if r := []rune(l); len(r) > 100 {
				return string(r[:100]) + "…"
			}
			return l
		}
	}
	return ""
}

func newIdeaShowCmd(dir *string) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "show <id>",
		Short: "Show one idea (same as 'cerne show I-NNNN')",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := app.CanonicalIdeaID(args[0])
			if err != nil {
				return err
			}
			return runShow(cmd, *dir, id, "", asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "machine-readable JSON output")
	return cmd
}

func newIdeaDropCmd(dir *string) *cobra.Command {
	var reason string
	cmd := &cobra.Command{
		Use:   "drop <id>",
		Short: "Discard an active idea without deleting it (active -> dropped, --reason optional)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := resolveProject(*dir)
			if err != nil {
				return err
			}
			i, err := app.DropIdea(ctx.repoDir, ctx.projDir, args[0], reason)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s: %s -> %s\n", i.ID, domain.IdeaActive, i.State)
			return nil
		},
	}
	cmd.Flags().StringVar(&reason, "reason", "", "why the idea is discarded (optional)")
	return cmd
}

func newIdeaPromoteCmd(dir *string) *cobra.Command {
	var title, module string
	var force bool
	cmd := &cobra.Command{
		Use:   "promote <id>",
		Short: "Turn an active idea into a candidate feature (active -> promoted), linked both ways",
		Long: "Creates a candidate feature titled by the idea's first line, with the idea's tags and its text as the " +
			"spec's context; the spec links back with '**Origem:** ideia [[I-NNNN]]' and the idea records " +
			"promoted_to. From there the candidate follows the regular funnel: fill its spec and tags.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := resolveProject(*dir)
			if err != nil {
				return err
			}
			i, f, err := app.PromoteIdea(ctx.repoDir, ctx.projDir, args[0], title, module, force)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s: %s -> %s (%s)\n", i.ID, domain.IdeaActive, i.State, f.ID)
			return nil
		},
	}
	cmd.Flags().StringVar(&title, "title", "", "the feature's title (default: the idea's first line) — for an idea that reads exactly like a live feature")
	cmd.Flags().StringVar(&module, "module", "", "module of the created feature (required when the project has 2+ modules)")
	cmd.Flags().BoolVar(&force, "force", false, "bypass the created feature's duplicate-registration guard (F-0147)")
	return cmd
}
