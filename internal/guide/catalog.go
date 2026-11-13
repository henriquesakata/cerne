package guide

import (
	"strings"

	"github.com/spf13/cobra"
)

// Catalog lists what cerne can do, built from the cobra tree so it never goes
// stale (F-0399): one line per first-level command with its Short, and below
// it one line per visible command of that group. It ends with how to ask.
func Catalog(root *cobra.Command) string {
	var b strings.Builder
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		for _, sub := range c.Commands() {
			if sub.Hidden || sub.Name() == "help" {
				continue
			}
			b.WriteString("  " + sub.CommandPath() + "  " + sub.Short + "\n")
			walk(sub)
		}
	}
	for _, g := range root.Commands() {
		if g.Hidden || g.Name() == "help" {
			continue
		}
		b.WriteString(g.Name() + "  " + g.Short + "\n")
		walk(g)
	}
	b.WriteString("ask: cerne guide \"<feature>\" for how to use it\n")
	return b.String()
}
