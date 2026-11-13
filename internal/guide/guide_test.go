package guide

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func testTree() *cobra.Command {
	root := &cobra.Command{Use: "cerne"}
	grp := &cobra.Command{Use: "widget", Short: "widget group"}
	leaf := &cobra.Command{Use: "frob <x>", Short: "frobnicate a widget", Long: "Frobs the widget thoroughly.", Run: func(*cobra.Command, []string) {}}
	leaf.Flags().String("scope", "", "where to write")
	hidden := &cobra.Command{Use: "secret", Short: "internal", Hidden: true, Run: func(*cobra.Command, []string) {}}
	grp.AddCommand(leaf)
	root.AddCommand(grp, hidden)
	return root
}

func bySource(secs []Section, prefix string) []Section {
	var out []Section
	for _, s := range secs {
		if strings.HasPrefix(s.Source, prefix) {
			out = append(out, s)
		}
	}
	return out
}

func TestCorpusHasThreeSources(t *testing.T) {
	secs := Corpus(testTree())
	for _, p := range []string{"skill: ", "help: cerne ", "faq"} {
		if len(bySource(secs, p)) == 0 {
			t.Errorf("no section with source %q", p)
		}
	}
	for _, s := range secs {
		if s.Title == "" || s.Source == "" || strings.TrimSpace(s.Text) == "" {
			t.Errorf("malformed section %+v", s)
		}
	}
}

func TestCorpusCommandSections(t *testing.T) {
	var frob *Section
	for _, s := range Corpus(testTree()) {
		if s.Source == "help: cerne widget frob" {
			c := s
			frob = &c
		}
		if strings.Contains(s.Source, "secret") {
			t.Errorf("hidden command leaked: %+v", s)
		}
	}
	if frob == nil {
		t.Fatal("no section for cerne widget frob")
	}
	for _, want := range []string{"frob <x>", "frobnicate a widget", "Frobs the widget", "--scope"} {
		if !strings.Contains(frob.Text, want) {
			t.Errorf("frob text lacks %q: %q", want, frob.Text)
		}
	}
}

func TestSkillSectionsSplitOnHeadings(t *testing.T) {
	secs := skillSections("x/SKILL.md", "---\nname: x\n---\n# T\n\n## One\nbody one\n```\n## not a heading\n```\n### Sub\nbody sub\n## Two\nbody two\n")
	var titles []string
	for _, s := range secs {
		titles = append(titles, s.Title)
	}
	if got := strings.Join(titles, "|"); got != "One|Sub|Two" {
		t.Fatalf("titles = %s", got)
	}
	if !strings.Contains(secs[0].Text, "## not a heading") || secs[0].Source != "skill: One" {
		t.Errorf("section One = %+v", secs[0])
	}
}
