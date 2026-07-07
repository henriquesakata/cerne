package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/app"
)

func newPromptCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "prompt",
		Short: "Compose dispatch prompts carrying the live board context",
	}
	cmd.AddCommand(newPromptComposeCmd())
	return cmd
}

// newPromptComposeCmd is F-0152: a prompt leaving the harness (another
// session, another dev, a cloud agent) is born with the board state only
// cerne has — and, with --register, with its work already registered.
func newPromptComposeCmd() *cobra.Command {
	var dir, typ, text string
	var register, list bool
	cmd := &cobra.Command{
		Use:   "compose",
		Short: "Compose a prompt: intent + type template + live board snapshot",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := resolveProject(dir)
			if err != nil {
				return err
			}
			if list {
				types, err := app.ListPromptTypes(ctx.repoDir)
				if err != nil {
					return err
				}
				if len(types) == 0 {
					fmt.Fprintln(cmd.OutOrStdout(), "No prompt templates yet — create one with: cerne note new \"<title>\" --component prompt-template")
					return nil
				}
				for _, pt := range types {
					fmt.Fprintf(cmd.OutOrStdout(), "%s — %s\n", pt.Slug, pt.Title)
				}
				return nil
			}
			if typ == "" || text == "" {
				return fmt.Errorf("--type and --text are required (or use --list to see the available types)")
			}
			if register {
				prompt, workID, err := app.ComposePromptRegister(ctx.repoDir, ctx.projDir, typ, text)
				if err != nil {
					return err
				}
				fmt.Fprint(cmd.OutOrStdout(), prompt)
				fmt.Fprintf(cmd.ErrOrStderr(), "Registered %s for this dispatch.\n", workID)
				return nil
			}
			prompt, err := app.ComposePrompt(ctx.repoDir, ctx.projDir, typ, text)
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), prompt)
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.Flags().MarkHidden("dir")
	cmd.Flags().StringVar(&typ, "type", "", "template type (a live note tagged prompt-template; see --list)")
	cmd.Flags().StringVar(&text, "text", "", "the intent to dispatch")
	cmd.Flags().BoolVar(&register, "register", false, "register the work (W-) at composition time and embed its id")
	cmd.Flags().BoolVar(&list, "list", false, "list the available template types")
	return cmd
}
