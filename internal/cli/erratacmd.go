package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/app"
)

// newErrataCmd builds the shared `errata` subcommand wired under each entity
// command (feature/decision/runbook/note). It appends an additive errata entry
// to a (typically concluded) entity in one shot — text via a positional arg or
// --file/stdin — never rewriting the entity's immutable body.
func newErrataCmd(dir *string) *cobra.Command {
	var file string
	cmd := &cobra.Command{
		Use:   "errata <id> [text]",
		Short: "Append an erratum (additive annotation) to a concluded entity, without rewriting the body",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runErrata(cmd, *dir, args, file)
		},
	}
	cmd.Flags().StringVar(&file, "file", "", "read the erratum text from a file (or stdin)")
	return cmd
}

func runErrata(cmd *cobra.Command, dir string, args []string, file string) error {
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
	id, err := app.AddErrata(ctx.repoDir, ctx.projDir, args[0], text, eventActor())
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "✓ erratum appended to %s — already synced. Run 'cerne pull' to materialize.\n", id)
	return nil
}
