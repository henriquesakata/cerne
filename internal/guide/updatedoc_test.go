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

// F-0476 crit. 11, as F-0480 criterion 7 changes it: the README names the
// two version checks (cerne update when you run it, the panel at start and
// every 24 hours) and how to turn the panel's off, and the data table has a
// row for each.
func TestReadmeNamesTheVersionChecks(t *testing.T) {
	data := section(t, "../../README.md", "## Where your data goes")
	if strings.Contains(data, "crash reporting or update check") {
		t.Error("README still promises no update check at all")
	}
	if !strings.Contains(data, "There are three version checks: `cerne update`, only when you run it, and the `cerne ui` panel") {
		t.Error("README lacks the sentence naming the three version checks")
	}
	if !strings.Contains(data, "cerne settings set ui.update_check false --scope host") {
		t.Error("README does not say how to turn the panel's check off")
	}
	if !strings.Contains(data, "\n| The latest release tag") || !strings.Contains(data, "Only when you run `cerne update`") {
		t.Error("README data table lacks the cerne update row")
	}
	if !strings.Contains(data, "no agent's session start asks until you turn it on with `cerne settings set update.notify true --scope host`") ||
		!strings.Contains(data, "`CERNE_NO_UPDATE_NOTIFIER`") {
		t.Error("README does not say the session notice is opt-in and how to turn it off")
	}
	if !strings.Contains(data, "\n| The latest release tag, asked by an agent's session start") || !strings.Contains(data, "About once every 24 hours per machine, only with `update.notify` on") {
		t.Error("README data table lacks the session notice's row")
	}
	if !strings.Contains(data, "\n| The latest release tag, read by the panel") || !strings.Contains(data, "every 24 hours while it is open") {
		t.Error("README data table lacks the panel's row")
	}
}

// F-0477 crit. 11: the three places that tell how the project files catch up
// say when the renewal is automatic, when it is only a warning, and how to
// turn it off.
func TestDocsSayWhenTheFilesRenewThemselves(t *testing.T) {
	for _, path := range []string{"../../INSTALL.md", "faq.md", "../../docs/first-week.md"} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		doc := string(raw)
		for _, want := range []string{
			"renew by themselves",
			"edited",
			"only warns",
			"cerne settings set hooks.auto_refresh false",
		} {
			if !strings.Contains(doc, want) {
				t.Errorf("%s does not say %q", path, want)
			}
		}
	}
}

// F-0479 crit. 7: the FAQ answer about updating says the session notice is
// opt-in, who hears it and how to turn it off.
func TestFAQSaysHowTheNewReleaseNoticeWorks(t *testing.T) {
	raw, err := os.ReadFile("faq.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"cerne settings set update.notify true --scope host",
		"one agent per machine",
		"CERNE_NO_UPDATE_NOTIFIER",
	} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("faq.md does not say %q", want)
		}
	}
}
