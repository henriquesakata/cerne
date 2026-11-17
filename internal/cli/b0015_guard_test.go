package cli

import (
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/doccheck"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// ptSurfaceAllowed lists visible cobra text that may carry pt-BR on purpose,
// keyed by "<command path>|<field>". Each entry needs a reason.
var ptSurfaceAllowed = map[string]string{
	"cerne feature complete|Example": "quotes the pt-BR protocol form \"critério N\" a docs_language=pt-BR result must carry (F-0359/F-0418)",
	"cerne runbook merge|Long":       "quotes the stored 'Altera:' label written inside a runbook errata document",
	"cerne runbook report|Long":      "quotes the stored 'Altera:' label written inside a runbook errata document",
}

// TestCommandSurfaceIsEnglish (B-0015) walks the whole visible cobra tree —
// Use, Short, Long, Example, aliases, flag names and flag usages — and fails
// on a pt-BR word or diacritic. Hidden commands and flags are exempt: that is
// where the legacy pt-BR names live as aliases for installed scripts.
func TestCommandSurfaceIsEnglish(t *testing.T) {
	ptNames := []string{"substituir", "altera", "sem-passo", "consolidado"}
	check := func(path, field, text string) {
		if _, ok := ptSurfaceAllowed[path+"|"+field]; ok {
			return
		}
		why := doccheck.PTReason(text)
		for _, n := range ptNames {
			if strings.Contains(strings.ToLower(text), n) {
				why = "pt-BR name " + n
			}
		}
		if why != "" {
			t.Errorf("%s: %s reads as pt-BR (%s): %s", path, field, why, firstLine(text))
		}
	}
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		if c.Hidden {
			return
		}
		p := c.CommandPath()
		check(p, "Use", c.Use)
		check(p, "Short", c.Short)
		check(p, "Long", c.Long)
		check(p, "Example", c.Example)
		check(p, "Aliases", strings.Join(c.Aliases, " "))
		c.LocalFlags().VisitAll(func(f *pflag.Flag) {
			if !f.Hidden {
				check(p, "--"+f.Name, f.Name+" "+f.Usage)
			}
		})
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(Root())
}
