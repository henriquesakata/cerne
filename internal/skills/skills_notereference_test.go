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
		if strings.Contains(l, "Guidance → note") {
			line = l
		}
	}
	if line == "" {
		t.Fatal("no routing line starting with Guidance -> note")
	}
	for _, want := range []string{"Guidance → note, source → reference", "a reference the note points to", "--informs N-NNNN", "reference/notes-and-knowledge.md"} {
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

// F-0483 crit. 2, 5: the reference files teach what each type keeps, the two
// commands that link them, and the two mistakes.
func TestReferenceFilesTeachNoteVsReferenceSplit(t *testing.T) {
	notes := readFile(t, referenceDir+"/notes-and-knowledge.md")
	for _, want := range []string{
		"### Note or reference: guidance vs. source",
		"keeps the source",
		"keeps the guidance",
		"cerne reference new",
		"--informs N-NNNN",
		"cerne reference link R-NNNN --informs N-NNNN",
		"Mistake: source pasted into a note",
		"Mistake: guidance written in a reference",
	} {
		if !strings.Contains(notes, want) {
			t.Errorf("notes-and-knowledge.md lacks %q", want)
		}
	}
	ext := readFile(t, referenceDir+"/external-sources.md")
	for _, want := range []string{"notes-and-knowledge.md", "Mistake: guidance written in a reference"} {
		if !strings.Contains(ext, want) {
			t.Errorf("external-sources.md lacks %q", want)
		}
	}
	i := strings.Index(notes, "### Note or reference")
	if i >= 0 {
		sub := notes[i:]
		if j := strings.Index(sub[4:], "\n#"); j >= 0 {
			sub = sub[:j+4]
		}
		if m := ptNoise.FindString(sub); m != "" {
			t.Errorf("new subsection has Portuguese %q", m)
		}
	}
}
