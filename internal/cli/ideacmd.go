package cli

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
	"github.com/henriquesakata/cerne/internal/verbs"
)

func newIdeaCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "idea",
		Short: "Capture and triage ideas (I-): raw material outside the demand funnel",
		Long: "An idea is raw material nobody has evaluated yet: a title, an optional body and optional tags, no spec, " +
			"no gates, no origin. It never appears in lint, next, status, stats, the wiki or the board — a candidate " +
			"is a demand already judged worth evaluating, an idea is not. Like a pending it has a lifecycle: resolve " +
			"it when something else already took care of it, drop it (kept, with an optional reason), or promote or " +
			"link it to a feature — from then on it follows that feature (its accept resolves the idea, a reject " +
			"reopens it, a 'substituir' moves it to the successor).",
	}
	cmd.PersistentFlags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.PersistentFlags().MarkHidden("dir")
	cmd.AddCommand(verbs.MarkChat(newIdeaNewCmd(&dir)))
	cmd.AddCommand(verbs.MarkChat(newIdeaListCmd(&dir)))
	cmd.AddCommand(verbs.MarkChat(newIdeaShowCmd(&dir)))
	cmd.AddCommand(verbs.MarkLocal(newIdeaResolveCmd(&dir)))
	cmd.AddCommand(verbs.MarkLocal(newIdeaDropCmd(&dir)))
	cmd.AddCommand(verbs.MarkLocal(newIdeaLinkCmd(&dir)))
	cmd.AddCommand(verbs.MarkLocal(newIdeaUnlinkCmd(&dir)))
	cmd.AddCommand(verbs.MarkLocal(newIdeaPromoteCmd(&dir)))
	return cmd
}

func newIdeaNewCmd(dir *string) *cobra.Command {
	var facet, component, file string
	cmd := &cobra.Command{
		Use:   "new <title…>",
		Short: "Capture an idea: the title is all it takes (body and tags optional)",
		Long: "Captures the words given as the idea's one-line title (quoting is optional). The optional body " +
			"(markdown) comes from --file or piped stdin. A title that starts with a dash goes after '--': " +
			"cerne idea new -- \"-x is an idea\".",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := readOptionalBody(cmd, file)
			if err != nil {
				return err
			}
			ctx, err := resolveProject(*dir)
			if err != nil {
				return err
			}
			// One token per invocation, reused by the one internal retry: a
			// failure after the push lands (snapshot, unconfirmed head) is
			// retried without minting a twin (C2).
			token := rand.Text()
			create := func() (store.Idea, error) {
				return app.CreateIdea(ctx.repoDir, ctx.projDir, strings.Join(args, " "), body, splitInforms(facet), splitInforms(component), token)
			}
			i, err := create()
			if err != nil && !errors.Is(err, app.ErrIdeaTitleRequired) && !errors.Is(err, app.ErrIdeaTitleMultiline) && !errors.Is(err, domain.ErrUnknownFacet) {
				i, err = create()
			}
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s created (%s) — %s\n", i.ID, i.State, ideaFirstLine(i.Title))
			return nil
		},
	}
	cmd.Flags().StringVar(&facet, "facet", "", "facets (CSV; closed set: infra,code,data,process,auth)")
	cmd.Flags().StringVar(&component, "component", "", "components (CSV)")
	cmd.Flags().StringVar(&file, "file", "", "file with the idea's body (defaults to piped stdin)")
	// A dash-leading title parses as a flag: say how to pass it (C15).
	cmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return fmt.Errorf("%w — a title that starts with a dash goes after '--': cerne idea new -- \"<title>\"", err)
	})
	return cmd
}

func newIdeaListCmd(dir *string) *cobra.Command {
	var all, asJSON bool
	var state, component, search string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List ideas — active by default, newest first: id, date, state, title",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if all {
				state = "all"
			}
			if !app.ValidIdeaState(state) { // before the project, as always
				return fmt.Errorf("--state %q: %w", state, app.ErrIdeaStateInvalid)
			}
			ctx, err := resolveProject(*dir)
			if err != nil {
				return err
			}
			ideas, err := app.ListIdeas(ctx.repoDir, app.IdeaFilter{State: state, Component: component, Search: search})
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
				line := fmt.Sprintf("%s  %s  %s  %s", i.ID, i.CreatedAt.Format(time.DateOnly), i.State, ideaFirstLine(i.Title))
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
	cmd.Flags().StringVar(&state, "state", string(domain.IdeaActive), "active, resolved, dropped, promoted or all (each line shows its own state)")
	cmd.Flags().BoolVar(&all, "all", false, "same as --state all")
	cmd.Flags().StringVar(&component, "component", "", "only ideas tagged with this component")
	cmd.Flags().StringVar(&search, "search", "", "only ideas whose title or body contains every word of this term, in any order (case- and accent-insensitive)")
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
			return runShow(cmd, *dir, id, "", asJSON, false)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "machine-readable JSON output")
	return cmd
}

func newIdeaResolveCmd(dir *string) *cobra.Command {
	var reason string
	cmd := &cobra.Command{
		Use:   "resolve <id>",
		Short: "Close an active idea that something else already took care of (active -> resolved, --reason optional)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := resolveProject(*dir)
			if err != nil {
				return err
			}
			i, err := app.ResolveIdea(ctx.repoDir, ctx.projDir, args[0], reason)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s: %s -> %s\n", i.ID, domain.IdeaActive, i.State)
			return nil
		},
	}
	cmd.Flags().StringVar(&reason, "reason", "", "what took care of it (optional)")
	return cmd
}

func newIdeaLinkCmd(dir *string) *cobra.Command {
	return &cobra.Command{
		Use:   "link <I-id> <F-id>",
		Short: "Point an active idea at a live feature that carries it (active -> promoted)",
		Long: "Links an active idea to a feature still live (candidate, approved or in_development), as promote " +
			"links the one it creates; an implemented, done, rejected or superseded feature is refused. From then " +
			"on the idea follows the feature: its accept resolves it, a reject reopens it, a 'substituir' moves " +
			"it to the successor. 'cerne idea unlink' undoes the link.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := resolveProject(*dir)
			if err != nil {
				return err
			}
			i, err := app.LinkIdea(ctx.repoDir, ctx.projDir, args[0], args[1])
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s: %s -> %s (%s)\n", i.ID, domain.IdeaActive, i.State, i.PromotedTo)
			return nil
		},
	}
}

func newIdeaUnlinkCmd(dir *string) *cobra.Command {
	return &cobra.Command{
		Use:   "unlink <I-id>",
		Short: "Take an idea back from the feature it points at (promoted -> active)",
		Long: "Returns a promoted idea to active and clears promoted_to: the way out when a live feature " +
			"will not carry it. An idea of an implemented feature waits for the accept (or reject) instead.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := resolveProject(*dir)
			if err != nil {
				return err
			}
			i, err := app.UnlinkIdea(ctx.repoDir, ctx.projDir, args[0])
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s: %s -> %s\n", i.ID, domain.IdeaPromoted, i.State)
			return nil
		},
	}
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
		Long: "Creates a candidate feature titled by the idea's title, with the idea's tags and its body as the " +
			"spec's context; the spec links back with '**Origem:** ideia [[I-NNNN]]' and the idea records " +
			"promoted_to. From there the candidate follows the regular funnel: fill its spec and tags. The idea follows the candidate (its accept resolves the idea).",
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
			// The candidate is a stub (N-0028): same nudge as feature new.
			fmt.Fprintf(cmd.OutOrStdout(), "Next step: complete the spec and tags of %s ('cerne pull' materializes the draft; 'cerne feature retag %s' sets the tags).\n", f.ID, f.ID)
			printApproveReadiness(cmd.OutOrStdout(), ctx.repoDir, ctx.projDir, f.ID)
			return nil
		},
	}
	cmd.Flags().StringVar(&title, "title", "", "the feature's title (default: the idea's title) — for an idea that reads exactly like a live feature")
	cmd.Flags().StringVar(&module, "module", "", "module of the created feature (required when the project has 2+ modules)")
	cmd.Flags().BoolVar(&force, "force", false, "bypass the created feature's duplicate-registration guard (F-0147)")
	return cmd
}
