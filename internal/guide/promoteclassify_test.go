package guide_test

import (
	"os"
	"strings"
	"testing"
)

// F-0510 crit. 9: the FAQ and the first-week guide show idea promote with the
// classification flags, so a promote copied from them is not refused for a
// missing tag (the FAQ also names --capability for a project with a taxonomy).
func TestDocsPromoteWithClassificationFlags(t *testing.T) {
	for path, wants := range map[string][]string{
		"faq.md":                   {"--facet", "--component", "--capability", "--allow-incomplete"},
		"../../docs/first-week.md": {"--facet", "--component"},
	} {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var line string
		for _, l := range strings.Split(string(b), "\n") {
			if strings.Contains(l, "cerne idea promote") {
				line = l
				break
			}
		}
		for _, w := range wants {
			if !strings.Contains(line, w) {
				t.Errorf("%s: the idea promote line lacks %q:\n%s", path, w, line)
			}
		}
	}
}
