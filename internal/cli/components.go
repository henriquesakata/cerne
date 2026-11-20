package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/domain"
)

// forceComponentHelp is the --force text of a command that writes only tags.
const forceComponentHelp = "accept a component near an existing one (same root or a few characters apart; E-0005) — check 'cerne tags' first"

// componentNotice reads, before a write, which of the components in raw (CSV)
// the backend does not have yet, and returns the func that announces them on
// stderr once the write succeeded (E-0005 criterion 21). It fails open: a
// vocabulary it cannot read announces nothing.
func componentNotice(cmd *cobra.Command, repoDir, raw string) func() {
	fresh, _ := app.NewComponents(repoDir, domain.NormalizeComponents(splitInforms(raw)))
	return func() {
		for _, c := range fresh {
			fmt.Fprintf(cmd.ErrOrStderr(), "cerne: new component %q — 'cerne tags' lists the ones in use\n", c)
		}
	}
}
