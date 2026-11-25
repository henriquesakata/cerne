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

// B-0072: walking the guide with the 0.15.2 release hit what it left out —
// the .claude/ folder init writes, the plan and result the gates ask for, a
// backlog example that leaves lint red, the test_roots warning — and a check
// it described as reading the spec.
func TestFirstWeekCoversWhatTheReleaseAsks(t *testing.T) {
	raw, err := os.ReadFile("../../docs/first-week.md")
	if err != nil {
		t.Fatal(err)
	}
	doc := string(raw)
	for _, want := range []string{
		"commit `.claude/settings.json` and `.claude/hooks/`",
		"`cerne feature start` refuses without one",
		"`cerne feature complete` refuses without one",
		"The Gauntlet is an optional quality loop",
		"the check can pass while the goal and the criteria are still empty",
		"`CRITERION-TESTS-CONFIG`",
		"`test_roots`",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("guide lacks %q", want)
		}
	}
	for _, bad := range []string{
		"--parent F-0001",
		"A spec must be written and tagged before it can be approved",
	} {
		if strings.Contains(doc, bad) {
			t.Errorf("guide still has %q", bad)
		}
	}
	found := guide.Search(guide.Corpus(cli.Root()), "do I commit the .claude folder", 3)
	if len(found) == 0 || !strings.Contains(found[0].Text, "`.claude/settings.json` and `.claude/hooks/`") {
		t.Errorf("cerne guide does not answer which .claude files to commit: %+v", found)
	}
}

// T-0013: the guide and the FAQ describe what the code prints, not what the
// first draft of the epic said it would.
func TestFirstWeekTextMatchesWhatTheCodePrints(t *testing.T) {
	raw, err := os.ReadFile("../../docs/first-week.md")
	if err != nil {
		t.Fatal(err)
	}
	doc := string(raw)
	for _, bad := range []string{
		"same three steps",
		"On a terminal, an import that brings delivered features asks once",
	} {
		if strings.Contains(doc, bad) {
			t.Errorf("guide still claims %q", bad)
		}
	}
	for _, want := range []string{
		"the import asks on a terminal dry-run, never with `--yes`",
		"delivered feature(s) will enter as implemented; add --delivered-state done",
		"First-week guide: " + guide.FirstWeekURL,
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("guide lacks %q", want)
		}
	}

	found := guide.Search(guide.Corpus(cli.Root()), "what do I do after installing", 3)
	if len(found) == 0 {
		t.Fatal("no answer")
	}
	for _, bad := range []string{"approve the spec, and accept the delivery", "shows the same three steps"} {
		if strings.Contains(found[0].Text, bad) {
			t.Errorf("FAQ answer claims init prints an accept step: %q", bad)
		}
	}
	faq := guide.Search(guide.Corpus(cli.Root()), "which agents did cerne init connect", 3)
	if len(faq) == 0 || strings.Contains(faq[0].Text, "and every other agent whose folder") {
		t.Errorf("FAQ says init connects every detected agent")
	}
}
