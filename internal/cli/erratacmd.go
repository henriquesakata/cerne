package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/app"
)

// newErrataCmd builds the shared `errata` subcommand wired under each entity
// command (feature/decision/runbook/note). It appends an additive errata entry
// to a (typically concluded) entity in one shot — text via a positional arg or
// --file/stdin — never rewriting the entity's immutable body.
func newErrataCmd(dir *string) *cobra.Command {
	var file, altera string
	var semPasso bool
	cmd := &cobra.Command{
		Use:   "errata <id> [text]",
		Short: "Append an erratum (additive annotation) to a concluded entity, without rewriting the body",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runErrata(cmd, *dir, args, file, altera, semPasso)
		},
	}
	cmd.Flags().StringVar(&file, "file", "", "read the erratum text from a file (or stdin)")
	cmd.Flags().StringVar(&altera, "altera", "", "the step this erratum changes (e.g. S6.6), declared: 'show --consolidado' places the item next to it; a runbook takes a comma-separated list and writes one item per step")
	cmd.Flags().BoolVar(&semPasso, "sem-passo", false, "runbook only: the erratum changes no step (a general note); without it a runbook erratum must declare --altera")
	return cmd
}

func runErrata(cmd *cobra.Command, dir string, args []string, file, altera string, semPasso bool) error {
	text := ""
	if len(args) == 2 {
		text = args[1]
	} else {
		body, err := readOptionalBody(cmd, file)
		if err != nil {
			return err
		}
		text = body
	}
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("erratum with no text: pass the text as an argument or via --file/stdin")
	}
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	var steps []string
	if strings.HasPrefix(args[0], "RB-") {
		for _, st := range strings.Split(altera, ",") {
			if st = strings.TrimSpace(st); st != "" {
				steps = append(steps, st)
			}
		}
		if len(steps) == 0 && !semPasso {
			return fmt.Errorf("a runbook erratum declares the step it changes: pass --altera <step> (a comma-separated list covers several), or %s for an erratum that changes no step", app.NoStepFlag)
		}
	} else if strings.TrimSpace(altera) != "" {
		steps = []string{altera}
	}
	id := args[0]
	ids, err := app.AddErrataSteps(ctx.repoDir, ctx.projDir, id, text, app.EventActor(), steps, semPasso)
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "✓ erratum appended to %s — already synced. Read it with 'cerne show %s'.\n", id, id)
	for _, item := range ids {
		fmt.Fprintf(cmd.OutOrStdout(), "item %s\n", item)
	}
	return nil
}
