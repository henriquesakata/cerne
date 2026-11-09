package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// withFinalLine makes cmd end with one stable stderr line, the last thing it
// prints (F-0876): `cerne: resultado=OK` or `cerne: resultado=FALHA <cause>`.
// stderr bypasses a `| tail` on stdout, so an agent that pipes push/sync still
// sees the outcome; the exit status stays the command's own (the pipe's exit
// belongs to the shell). The error is printed here, before the line, instead of
// by cobra after it, so the line really is the last one.
func withFinalLine(cmd *cobra.Command) *cobra.Command {
	run := cmd.RunE
	cmd.SilenceErrors = true
	cmd.RunE = func(c *cobra.Command, args []string) error {
		err := run(c, args)
		if err == nil {
			fmt.Fprintln(c.ErrOrStderr(), "cerne: resultado=OK")
			return nil
		}
		fmt.Fprintf(c.ErrOrStderr(), "Error: %v\n", err)
		fmt.Fprintf(c.ErrOrStderr(), "cerne: resultado=FALHA %s\n", oneLine(err.Error()))
		return err
	}
	return cmd
}

// oneLine folds a multi-line error (a conflict diff, the frozen-document
// block) into its first line, so the final line stays one stable line.
func oneLine(s string) string {
	first, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return first
}
