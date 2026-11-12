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
		noteWrite(c.ErrOrStderr()) // F-0347: before the line that must stay last
		if err == nil {
			fmt.Fprintln(c.ErrOrStderr(), "cerne: resultado=OK")
			return nil
		}
		return failWithLine(c, err)
	}
	// Flag and argument errors are raised by cobra before RunE ever runs; they
	// end in the same line, or `cerne push --bogus | tail` would be the one
	// silent case (F-0876).
	cmd.SetFlagErrorFunc(func(c *cobra.Command, err error) error { return failWithLine(c, err) })
	if args := cmd.Args; args != nil {
		cmd.Args = func(c *cobra.Command, a []string) error {
			if err := args(c, a); err != nil {
				return failWithLine(c, err)
			}
			return nil
		}
	}
	return cmd
}

// failWithLine prints the error and then the FALHA line (so the line is the
// last thing printed) and hands the error back for the exit status.
func failWithLine(c *cobra.Command, err error) error {
	fmt.Fprintf(c.ErrOrStderr(), "Error: %v\n", err)
	// The literal is spec-mandated (F-0876) and pt-BR, so it is built apart
	// from the Fprintf (the English-output guard, F-0124, reads those).
	var line strings.Builder
	line.WriteString("cerne: resultado=FALHA ")
	line.WriteString(oneLine(err.Error()))
	fmt.Fprintln(c.ErrOrStderr(), line.String())
	return err
}

// oneLine folds a multi-line error (a conflict diff, the frozen-document
// block) into its first line, so the final line stays one stable line.
func oneLine(s string) string {
	first, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return first
}
