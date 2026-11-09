package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/store"
)

// F-0872: the consolidated reading of a runbook (body + errata items), the
// decision of an item and the merge that folds decided items into a new body
// revision.

func newRunbookDecideCmd(dir *string) *cobra.Command {
	var by, quote string
	cmd := &cobra.Command{
		Use:   "decide <RB-id> <item> --by <nickname> --quote \"<decision>\"",
		Short: "Decide an errata item of a runbook, in place (the item changes state; no new erratum)",
		Long: "Moves an errata item (E1, E2, … as printed by 'cerne errata' and 'cerne show <RB> --consolidado') from " +
			"proposta to decidida, quoting the decision as said and who said it (a nickname only). The item is edited " +
			"in place; the runbook body is untouched until 'cerne runbook merge'.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := resolveProject(*dir)
			if err != nil {
				return err
			}
			id := strings.ToUpper(strings.TrimSpace(args[0]))
			if err := app.DecideErrata(ctx.repoDir, ctx.projDir, id, args[1], by, quote); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s: decided (%s). Fold it into the body with 'cerne runbook merge %s'.\n",
				id, strings.ToUpper(args[1]), by, id)
			return nil
		},
	}
	cmd.Flags().StringVar(&by, "by", "", "who decided (a nickname)")
	cmd.Flags().StringVar(&quote, "quote", "", "the decision, quoted as said")
	return cmd
}

func newRunbookMergeCmd(dir *string) *cobra.Command {
	return &cobra.Command{
		Use:   "merge <RB-id>",
		Short: "Fold the decided errata items into the runbook body as a new revision",
		Long: "Places each decided, not yet merged errata item under the step it declares ('Altera:') in the body and " +
			"makes the result a new revision. The body it replaces is kept: read it with 'cerne show <RB> --rev N'. " +
			"Proposals, and decided items whose step the body does not have, are left out and listed. The merge " +
			"inserts the erratum text as a quoted block under the step; rewording the step itself stays a human edit.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := resolveProject(*dir)
			if err != nil {
				return err
			}
			id := strings.ToUpper(strings.TrimSpace(args[0]))
			res, err := app.MergeErrata(ctx.repoDir, ctx.projDir, id, eventActor())
			w := cmd.OutOrStdout()
			if errors.Is(err, app.ErrNothingToMerge) {
				fmt.Fprintf(w, "%s: nothing to merge (needs a decided item whose 'Altera:' step is in the body).\n", id)
				if len(res.Skipped) > 0 {
					fmt.Fprintf(w, "decided but without a step in the body: %s\n", strings.Join(res.Skipped, ", "))
				}
				return nil
			}
			if err != nil {
				return err
			}
			fmt.Fprintf(w, "%s: merged %s into revision %d. Earlier body: 'cerne show %s --rev %d'.\n",
				id, strings.Join(res.Merged, ", "), res.Revision, id, res.Revision-1)
			if len(res.Skipped) > 0 {
				fmt.Fprintf(w, "left out (no step in the body): %s\n", strings.Join(res.Skipped, ", "))
			}
			return nil
		},
	}
}

// runShowRunbookView prints the consolidated body of a runbook, or one of its
// revisions. Read-only.
func runShowRunbookView(cmd *cobra.Command, dir, id string, consolidated bool, rev int) error {
	id = strings.ToUpper(strings.TrimSpace(id))
	if !strings.HasPrefix(id, "RB-") {
		return fmt.Errorf("--consolidado and --rev are for runbooks (RB- ids), got %s", id)
	}
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	if _, err := store.LoadRunbook(ctx.repoDir, id); err != nil {
		return err
	}
	w := cmd.OutOrStdout()
	if rev != 0 {
		body, err := store.LoadRevision(ctx.repoDir, id, rev)
		if err != nil {
			return err
		}
		fmt.Fprint(w, body)
		return nil
	}
	body, err := store.LoadRunbookContent(ctx.repoDir, id)
	if err != nil {
		return err
	}
	doc, err := store.LoadErrata(ctx.repoDir, id)
	if err != nil {
		return err
	}
	var open []store.ErrataItem // not yet in the body
	for _, it := range store.ParseErrataItems(doc) {
		if it.Merged == "" {
			open = append(open, it)
		}
	}
	out, orphans := store.InsertAtSteps(body, open, func(it store.ErrataItem) string { return it.StateLabel() })
	fmt.Fprint(w, out)
	if !strings.HasSuffix(out, "\n") {
		fmt.Fprintln(w)
	}
	if len(orphans) > 0 {
		fmt.Fprintln(w, "\n## Errata without a reference step")
		for _, it := range orphans {
			first, _, _ := strings.Cut(it.Text, "\n")
			why := "no 'Altera:' declared"
			if it.Target != "" {
				why = "step " + it.Target + " not found in the body"
			}
			fmt.Fprintf(w, "\n- %s (%s, %s; %s): %s\n", it.ID, it.Date, it.StateLabel(), why, first)
		}
	}
	return nil
}
