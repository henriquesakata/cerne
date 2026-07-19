package cli

import (
	"strings"
	"testing"
	"unicode"

	"github.com/spf13/cobra"
)

// TestCommandChromeIsEnglish is the F-0184 lock: CLI chrome is English for
// agents (F-0124, N-0013), and the `work` command proved that without a
// tripwire new commands drift back to pt-BR. Cobra metadata is the guarded
// surface — authoring prompts are runtime strings, not command metadata, so
// the separation has no false positives by construction. Example is exempt:
// it may carry user-typed pt content (a --reason someone would write).
func TestCommandChromeIsEnglish(t *testing.T) {
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		for name, text := range map[string]string{
			"Use":   c.Use,
			"Short": c.Short,
			"Long":  c.Long,
		} {
			for _, r := range text {
				if unicode.Is(unicode.Latin, r) && r > 127 {
					t.Errorf("%s: %s carries a diacritic (%q) — CLI chrome is English (F-0124/F-0184): %s",
						c.CommandPath(), name, r, firstLine(text))
					break
				}
			}
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(Root())
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i] + " …"
	}
	return s
}
