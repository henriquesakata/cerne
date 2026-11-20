package skills

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// ptNoise matches a Portuguese diacritic or a common pt-BR function word.
var ptNoise = regexp.MustCompile(`(?i)[áàâãéêíóôõúç]|(?:^|[^\p{L}])(nota|fonte|referência|orientação|não|para|uma|com)(?:[^\p{L}]|$)`)

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// F-0483 crit. 1, 3, 5: one short English line in "Knowledge routing" routes
// guidance to a note, the source to a reference, and points to the detail.
func TestSkillRoutesGuidanceToNoteAndSourceToReference(t *testing.T) {
	doc := readFile(t, "cerne-authoring/SKILL.md")
	i := strings.Index(doc, "## Knowledge routing")
	if i < 0 {
		t.Fatal("no Knowledge routing section")
	}
	sec := doc[i:]
	if j := strings.Index(sec[3:], "\n## "); j >= 0 {
		sec = sec[:j+3]
	}
	var line string
	for _, l := range strings.Split(sec, "\n") {
		if strings.Contains(l, "reference/notes-and-knowledge.md") {
			line = l
		}
	}
	if line == "" {
		t.Fatal("no routing line pointing to reference/notes-and-knowledge.md")
	}
	for _, want := range []string{"note", "reference", "--informs N-NNNN"} {
		if !strings.Contains(line, want) {
			t.Errorf("routing line lacks %q: %s", want, line)
		}
	}
	if len(line) > 300 {
		t.Errorf("routing line is %d bytes, max 300", len(line))
	}
	if m := ptNoise.FindString(line); m != "" {
		t.Errorf("routing line has Portuguese %q", m)
	}
}
