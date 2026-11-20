package guide_test

import (
	"os"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/cli"
	"github.com/henriquesakata/cerne/internal/guide"
)

// firstWeekSections are the guide's `##` headings, in the order a newcomer
// needs them (E-0004, criterion 8).
var firstWeekSections = []string{
	"## After init and after an import",
	"## Connect your agent",
	"## Ask for the first feature and approve it",
	"## Change your mind and find what went out of force",
	"## Search, and when to compile the wiki",
	"## Create and work the backlog",
	"## The daily routine",
}

func TestFirstWeekGuideIsLinkedAndOrdered(t *testing.T) {
	readme, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, anchor := range []string{"### Already have a project?", "## Quickstart"} {
		i := strings.Index(string(readme), anchor)
		if i < 0 {
			t.Fatalf("README has no %q", anchor)
		}
		rest := string(readme)[i+len(anchor):]
		end := "\n## "
		if anchor == "## Quickstart" {
			end = "\n### Already have a project?" // the subsection's own link does not count
		}
		if j := strings.Index(rest, end); j >= 0 {
			rest = rest[:j]
		}
		if !strings.Contains(rest, "(docs/first-week.md)") {
			t.Errorf("README section %q does not link docs/first-week.md", anchor)
		}
	}
	doc, err := os.ReadFile("../../docs/first-week.md")
	if err != nil {
		t.Fatal(err)
	}
	pos := 0
	for _, h := range firstWeekSections {
		i := strings.Index(string(doc)[pos:], "\n"+h+"\n")
		if i < 0 {
			t.Fatalf("section %q missing or out of order", h)
		}
		pos += i + len(h)
	}
	if !strings.HasSuffix(guide.FirstWeekURL, "/docs/first-week.md") {
		t.Errorf("FirstWeekURL = %q", guide.FirstWeekURL)
	}
}

func TestGuideAnswersWhatToDoAfterInstalling(t *testing.T) {
	found := guide.Search(guide.Corpus(cli.Root()), "what do I do after installing", 3)
	if len(found) == 0 {
		t.Fatal("no answer")
	}
	first := found[0]
	for _, want := range []string{"cerne init", "cerne next", "first-week.md"} {
		if !strings.Contains(first.Text, want) {
			t.Errorf("first section %q lacks %q", first.Title, want)
		}
	}
}
