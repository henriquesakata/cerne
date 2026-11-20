package guide_test

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// quickstartBlockRe is a block the quickstart check runs with the latest
// release: a command that release lacks must stay out of it.
var quickstartBlockRe = regexp.MustCompile("(?s)<!-- quickstart -->\n```[a-z]*\n(.*?)```")

// E-0005 criterion 13 (T-0017): the README and the first-week guide describe
// the map and the task package as how an agent starts a task — and keep
// `cerne context` out of the quickstart blocks until a release ships it.
func TestDocsDescribeTheMapAndThePackage(t *testing.T) {
	for _, tc := range []struct {
		file string
		want []string
	}{
		{"../../README.md", []string{"`cerne context <id>`", "the map of what is live", "`cerne next`"}},
		{"../../docs/first-week.md", []string{"`cerne context <id>`", "the map of what is live", "`cerne show <id>`"}},
	} {
		raw, err := os.ReadFile(tc.file)
		if err != nil {
			t.Fatal(err)
		}
		doc := string(raw)
		for _, w := range tc.want {
			if !strings.Contains(doc, w) {
				t.Errorf("%s lacks %q", tc.file, w)
			}
		}
		for _, m := range quickstartBlockRe.FindAllStringSubmatch(doc, -1) {
			if strings.Contains(m[1], "cerne context") {
				t.Errorf("%s: a quickstart block runs cerne context, which the latest release lacks:\n%s", tc.file, m[1])
			}
		}
	}
}
