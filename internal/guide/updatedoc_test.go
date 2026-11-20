package guide_test

import (
	"os"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/cli"
	"github.com/henriquesakata/cerne/internal/guide"
)

// section returns the text of a markdown heading up to the next heading of
// the same or a higher level.
func section(t *testing.T, path, heading string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	doc := string(raw)
	i := strings.Index(doc, "\n"+heading+"\n")
	if i < 0 {
		t.Fatalf("%s has no %q", path, heading)
	}
	level := strings.Index(heading, " ")
	var b strings.Builder
	for _, line := range strings.Split(doc[i+len(heading)+2:], "\n") {
		if n := len(line) - len(strings.TrimLeft(line, "#")); n > 0 && n <= level && strings.HasPrefix(line[n:], " ") {
			break
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

// F-0476 crit. 13: INSTALL.md, the first-week guide and cerne guide teach
// cerne update as the update path, and keep the manager's command for a
// version without it.
func TestDocsTeachCerneUpdate(t *testing.T) {
	updating := section(t, "../../INSTALL.md", "### Updating")
	for _, want := range []string{"cerne update", "brew upgrade henriquesakata/tap/cerne", "install script", "--check"} {
		if !strings.Contains(updating, want) {
			t.Errorf("INSTALL.md \"Updating\" lacks %q:\n%s", want, updating)
		}
	}
	routine := section(t, "../../docs/first-week.md", "## The daily routine")
	for _, want := range []string{"cerne update", "brew upgrade henriquesakata/tap/cerne"} {
		if !strings.Contains(routine, want) {
			t.Errorf("first-week \"The daily routine\" lacks %q", want)
		}
	}
	corpus := guide.Corpus(cli.Root())
	for _, q := range []string{"how do I update cerne", "I updated the cerne binary but the project still has the old hooks"} {
		found := guide.Search(corpus, q, 3)
		if len(found) == 0 || !strings.Contains(found[0].Text, "cerne update") || !strings.Contains(found[0].Text, "brew upgrade henriquesakata/tap/cerne") {
			t.Errorf("cerne guide %q does not answer with cerne update and the manager command: %+v", q, found)
		}
	}
	for _, path := range []string{"../../INSTALL.md", "../../docs/first-week.md"} {
		raw, _ := os.ReadFile(path)
		// The quickstart check runs against the latest release, which may not
		// have the command yet.
		if strings.Contains(string(raw), "<!-- quickstart -->\n```bash\ncerne update") {
			t.Errorf("%s marks a cerne update block as quickstart", path)
		}
	}
}

// F-0476 crit. 11: the README says cerne update is the only version check,
// and its data table has the row.
func TestReadmeNamesTheOnlyVersionCheck(t *testing.T) {
	data := section(t, "../../README.md", "## Where your data goes")
	if strings.Contains(data, "crash reporting or update check") {
		t.Error("README still promises no update check at all")
	}
	if !strings.Contains(data, "The only version check is `cerne update`, and only when you run it") {
		t.Error("README lacks the sentence naming cerne update as the only version check")
	}
	if !strings.Contains(data, "\n| The latest release tag") || !strings.Contains(data, "Only when you run `cerne update`") {
		t.Error("README data table lacks the cerne update row")
	}
}
