package guide_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/henriquesakata/cerne/internal/cli"
	"github.com/henriquesakata/cerne/internal/guide"
)

var codeSpan = regexp.MustCompile("`([^`\n]+)`")

// faqProblems lists every `cerne <verb> …` span of text whose verb is not in
// the cobra tree, or that uses a --flag the verb does not have (F-0398).
func faqProblems(root *cobra.Command, text string) []string {
	var out []string
	for _, m := range codeSpan.FindAllStringSubmatch(text, -1) {
		words := strings.Fields(m[1])
		if len(words) < 2 || words[0] != "cerne" {
			continue
		}
		cmd, rest := root, words[1:]
		for len(rest) > 0 {
			next := childNamed(cmd, rest[0])
			if next == nil {
				break
			}
			cmd, rest = next, rest[1:]
		}
		if cmd == root {
			out = append(out, "no such command: "+m[1])
			continue
		}
		for _, w := range rest {
			if !strings.HasPrefix(w, "--") {
				continue
			}
			name := strings.SplitN(strings.TrimPrefix(w, "--"), "=", 2)[0]
			if !hasFlag(cmd, name) {
				out = append(out, cmd.CommandPath()+" has no flag --"+name+": "+m[1])
			}
		}
	}
	return out
}

func childNamed(c *cobra.Command, name string) *cobra.Command {
	for _, sub := range c.Commands() {
		if sub.Name() == name || sub.HasAlias(name) {
			return sub
		}
	}
	return nil
}

func hasFlag(c *cobra.Command, name string) bool {
	found := false
	visit := func(f *pflag.Flag) { found = found || f.Name == name }
	c.LocalFlags().VisitAll(visit)
	c.InheritedFlags().VisitAll(visit)
	return found
}

func TestFAQProblemsDetector(t *testing.T) {
	root := cli.Root()
	bad := "Run `cerne settings set a b --scope project` then `cerne settings set a b --scoop project`, `cerne nonesuch x` and `cerne feature approve F-1 --nope`."
	got := faqProblems(root, bad)
	if len(got) != 3 {
		t.Fatalf("problems = %q, want 3 (flag --scoop, verb nonesuch, flag --nope)", got)
	}
}

func TestFAQCitesOnlyExistingCommandsAndFlags(t *testing.T) {
	root := cli.Root()
	n := 0
	for _, s := range guide.Corpus(root) {
		if s.Source != "faq" {
			continue
		}
		n++
		for _, p := range faqProblems(root, s.Title+"\n"+s.Text) {
			t.Errorf("FAQ %q: %s", s.Title, p)
		}
	}
	if n < 8 {
		t.Errorf("FAQ has %d entries, want at least 8 (5 friction cases + 3 concepts)", n)
	}
}
