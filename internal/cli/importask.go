package cli

import (
	"bufio"
	"fmt"
	"github.com/henriquesakata/cerne/internal/importer"
	"io"
	"os"
	"strings"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
)

// importInteractive reports whether the import may ask a question; a variable
// so tests can force either answer.
var importInteractive = stdinIsInteractive

// stdinIsInteractive is true only for the process's own stdin when it is a
// terminal: a pipe, a file, /dev/null or a test reader never is.
func stdinIsInteractive(in io.Reader) bool {
	f, ok := in.(*os.File)
	return ok && (isatty.IsTerminal(f.Fd()) || isatty.IsCygwinTerminal(f.Fd()))
}

// askDeliveredState asks once which state features imported as delivered enter
// with. It asks only when n features are delivered, no choice was given on the
// command line, the import is not applying (--yes) and stdin is a terminal;
// otherwise, and on an empty or unknown answer, it returns current. Without a
// terminal, a dry-run states the default and the flag in one line instead.
func askDeliveredState(cmd *cobra.Command, chosen string, yes bool, n int, current string) string {
	in := cmd.InOrStdin()
	if chosen != "" || yes || n == 0 {
		return current
	}
	out := cmd.OutOrStdout()
	if !importInteractive(in) {
		fmt.Fprintf(out, "%d delivered feature(s) will enter as implemented; add --delivered-state done to enter them as done.\n", n)
		return current
	}
	fmt.Fprint(out, "Features imported as delivered enter as implemented (awaiting your accept) or done. Choose [implemented/done] (default implemented): ")
	line, _ := bufio.NewReader(in).ReadString('\n')
	if strings.TrimSpace(strings.ToLower(line)) != "done" {
		fmt.Fprintln(out)
		return current
	}
	fmt.Fprintln(out, "To enter them as done, add --delivered-state done when you apply.")
	return "done"
}

// countDelivered counts the feature candidates mapped as delivered.
func countDelivered(cands []importer.Candidate) int {
	n := 0
	for _, c := range cands {
		if c.Kind == "feature" && c.State == "implemented" {
			n++
		}
	}
	return n
}
