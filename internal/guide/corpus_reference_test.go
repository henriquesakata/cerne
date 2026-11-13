package guide_test

import (
	"os"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/cli"
	"github.com/henriquesakata/cerne/internal/guide"
)

// TestEveryOldSkillSectionIsStillAnswerable pins F-0399 criterion 4: for the
// title of each section of the 2026-11-13 skill, `cerne guide "<title>"`
// returns a section with that title among the first 3, wherever it moved.
func TestEveryOldSkillSectionIsStillAnswerable(t *testing.T) {
	old, err := os.ReadFile("../skills/testdata/skill-before-f0399.md")
	if err != nil {
		t.Fatal(err)
	}
	corpus := guide.Corpus(cli.Root())
	fence, n := false, 0
	for _, l := range strings.Split(string(old), "\n") {
		if strings.HasPrefix(l, "```") {
			fence = !fence
		}
		if fence || !strings.HasPrefix(l, "# ") && !strings.HasPrefix(l, "## ") && !strings.HasPrefix(l, "### ") {
			continue
		}
		title := strings.TrimSpace(strings.TrimLeft(l, "# "))
		n++
		found := false
		for _, s := range guide.Search(corpus, title, 3) {
			found = found || s.Title == title
		}
		if !found {
			t.Errorf("guide %q: the section is not among the first 3", title)
		}
	}
	if n != 35 {
		t.Errorf("read %d old sections, want 35", n)
	}
}

// TestEveryCommandGroupFindsItsDetail pins F-0399 adjustment 4: for each
// first-level command group, `cerne guide "<group>"` brings a reference
// section or that group's help among the first 3.
func TestEveryCommandGroupFindsItsDetail(t *testing.T) {
	root := cli.Root()
	corpus := guide.Corpus(root)
	for _, g := range root.Commands() {
		if g.Hidden || g.Name() == "help" {
			continue
		}
		ok := false
		for _, s := range guide.Search(corpus, g.Name(), 3) {
			ok = ok || strings.HasPrefix(s.Source, "reference: ") || strings.HasPrefix(s.Source, "help: cerne "+g.Name())
		}
		if !ok {
			t.Errorf("guide %q: no reference or help of the group among the first 3", g.Name())
		}
	}
}

// TestCorpusReadsTheReferenceFiles: reference sections carry their own source.
func TestCorpusReadsTheReferenceFiles(t *testing.T) {
	for _, s := range guide.Corpus(cli.Root()) {
		if strings.HasPrefix(s.Source, "reference: ") && strings.Contains(s.Source, "gauntlet.md") {
			return
		}
	}
	t.Fatal("no section sourced from reference/gauntlet.md")
}
