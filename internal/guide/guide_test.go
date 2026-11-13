package guide

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/skills"
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
	if got := strings.Join(titles, "|"); got != "T|One|Sub|Two" {
		t.Fatalf("titles = %s", got)
	}
	if !strings.Contains(secs[1].Text, "## not a heading") || secs[1].Source != "skill: One" {
		t.Errorf("section One = %+v", secs[0])
	}
}

func TestSkillIntroAndEmptySectionsKeepTheirTitles(t *testing.T) {
	secs := skillSections("x/SKILL.md", "---\nname: x\n---\n# Title\n\nintro hint line\n\n## Empty\n### Child\nchild body\n~~~\n## fenced tilde\n~~~\n## Last\nbody\n")
	var got []string
	for _, s := range secs {
		got = append(got, s.Title)
	}
	if want := "Title|Empty|Child|Last"; strings.Join(got, "|") != want {
		t.Fatalf("titles = %v, want %s", got, want)
	}
	if !strings.Contains(secs[0].Text, "intro hint line") || strings.Contains(secs[0].Text, "name: x") {
		t.Errorf("intro = %q", secs[0].Text)
	}
	if strings.TrimSpace(secs[1].Text) == "" {
		t.Errorf("an empty section needs a non-empty text")
	}
}

// TestSkillSectionCountMatchesHeadings pins that no heading of the real skill
// is dropped: 35 headings once gave 34 sections because an empty one vanished.
func TestSkillSectionCountMatchesHeadings(t *testing.T) {
	content, ok := skills.Read("cerne-authoring/SKILL.md")
	if !ok {
		t.Fatal("skill not embedded")
	}
	want, fence := 0, ""
	for _, l := range strings.Split(content, "\n") {
		if m := strings.TrimLeft(l, " "); strings.HasPrefix(m, "```") || strings.HasPrefix(m, "~~~") {
			if fence == "" {
				fence = m[:3]
			} else if strings.HasPrefix(m, fence) {
				fence = ""
			}
			continue
		}
		if fence == "" && (strings.HasPrefix(l, "# ") || strings.HasPrefix(l, "## ") || strings.HasPrefix(l, "### ")) {
			want++
		}
	}
	if got := len(skillSections("cerne-authoring/SKILL.md", content)); got != want {
		t.Fatalf("%d sections for %d headings", got, want)
	}
	if intro := bySource(skillSections("x", content), "skill: Cerne-Native Authoring"); len(intro) != 1 || !strings.Contains(intro[0].Text, "cerne guide") {
		t.Errorf("the opening line is not findable: %+v", intro)
	}
}
