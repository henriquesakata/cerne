package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/app"
)

func newClarifyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "clarify",
		Short: "List and answer open [ESCLARECER]/[CLARIFY] markers from the terminal",
	}
	cmd.AddCommand(newClarifyListCmd())
	cmd.AddCommand(newClarifyAnswerCmd())
	return cmd
}

func newClarifyListCmd() *cobra.Command {
	var dir string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "list [<id>]",
		Short: "List open clarifications across live entities (or one entity)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := resolveProject(dir)
			if err != nil {
				return err
			}
			id := ""
			if len(args) == 1 {
				id = args[0]
			}
			items, err := app.ListClarifications(ctx.repoDir, id)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if asJSON {
				if items == nil {
					items = []app.OpenClarification{}
				}
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(items)
			}
			if len(items) == 0 {
				fmt.Fprintln(out, "no open clarifications")
				return nil
			}
			lastDoc := ""
			for _, c := range items {
				if c.Doc != lastDoc {
					fmt.Fprintf(out, "%s — %s\n", c.Entity, c.Doc)
					lastDoc = c.Doc
				}
				q := c.Question
				if q == "" {
					q = "(no question)"
				}
				fmt.Fprintf(out, "  #%d  %s\n", c.Index, q)
				if len(c.Options) > 0 {
					fmt.Fprintf(out, "      options: %s\n", strings.Join(c.Options, " | "))
				}
			}
			fmt.Fprintf(out, "\nanswer one with: cerne clarify answer <id> <n> --answer \"...\" (or --pick <k>)\n")
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "machine-readable output")
	cmd.Flags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.Flags().MarkHidden("dir")
	return cmd
}

func newClarifyAnswerCmd() *cobra.Command {
	var dir, answer string
	var pick int
	cmd := &cobra.Command{
		Use:   "answer <id> <n>",
		Short: "Answer the n-th open clarification of an entity",
		Long: "Rewrites the n-th open [ESCLARECER]/[CLARIFY] marker of the entity to its\n" +
			"resolved form ([ESCLARECIDO: question → answer]) and pushes through the\n" +
			"standard pipeline. Use --answer for free text or --pick to choose one of the\n" +
			"marker's proposed options (see 'cerne clarify list').",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			n, err := strconv.Atoi(args[1])
			if err != nil || n < 1 {
				return fmt.Errorf("invalid clarification index %q: expected a 1-based number from 'cerne clarify list %s'", args[1], id)
			}
			if answer != "" && pick > 0 {
				return errors.New("--answer and --pick are mutually exclusive")
			}
			if answer == "" && pick <= 0 {
				return errors.New("provide the answer with --answer \"...\" or --pick <k>")
			}
			ctx, err := resolveProject(dir)
			if err != nil {
				return err
			}
			if pick > 0 {
				items, err := app.ListClarifications(ctx.repoDir, id)
				if err != nil {
					return err
				}
				var target *app.OpenClarification
				for i := range items {
					if items[i].Index == n {
						target = &items[i]
						break
					}
				}
				if target == nil {
					return fmt.Errorf("%w: %s has no open clarification #%d", app.ErrClarificationGone, id, n)
				}
				if len(target.Options) == 0 {
					return fmt.Errorf("clarification #%d has no proposed options — use --answer", n)
				}
				if pick > len(target.Options) {
					return fmt.Errorf("--pick %d out of range: clarification #%d has %d option(s)", pick, n, len(target.Options))
				}
				answer = target.Options[pick-1]
			}
			ans, err := app.AnswerClarification(ctx.repoDir, ctx.projDir, id, n, answer)
			if err != nil {
				if errors.Is(err, app.ErrClarificationGone) {
					return fmt.Errorf("%w — the document may have advanced; re-run 'cerne clarify list %s'", err, id)
				}
				return err
			}
			q := ans.Question
			if q == "" {
				q = "(no question)"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "answered %s #%d: %s → %s\n", ans.Entity, n, q, ans.Answer)
			return nil
		},
	}
	cmd.Flags().StringVar(&answer, "answer", "", "free-text answer to the question")
	cmd.Flags().IntVar(&pick, "pick", 0, "choose the k-th proposed option as the answer")
	cmd.Flags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.Flags().MarkHidden("dir")
	return cmd
}
