package skills

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// protocolPairs pairs each pt-BR literal of the protocol with its English
// form (internal/doclang): a block of the skill that names the first must
// name the second too (F-0418 crit. 7).
var protocolPairs = []struct{ pt, en *regexp.Regexp }{
	{regexp.MustCompile(`Barra:`), regexp.MustCompile(`Bar:`)},
	{regexp.MustCompile(`Método:`), regexp.MustCompile(`Method:`)},
	{regexp.MustCompile(`Saída:`), regexp.MustCompile(`Exit:`)},
	{regexp.MustCompile(`Pergunta ao juiz`), regexp.MustCompile(`Question for the judge`)},
	{regexp.MustCompile(`Rodada`), regexp.MustCompile(`Round`)},
	{regexp.MustCompile(`(?i)\bvitória\b`), regexp.MustCompile(`(?i)\bwin\b`)},
	{regexp.MustCompile(`(?i)\bderrota\b`), regexp.MustCompile(`(?i)\bloss\b`)},
	{regexp.MustCompile(`(?i)\bempate\b`), regexp.MustCompile(`(?i)\btie\b`)},
	{regexp.MustCompile(`Achados do crítico`), regexp.MustCompile(`Critic findings`)},
	{regexp.MustCompile(`critério N`), regexp.MustCompile(`criterion N`)},
	{regexp.MustCompile(`\[ESCLARECER`), regexp.MustCompile(`\[CLARIFY`)},
	{regexp.MustCompile(`Verificação`), regexp.MustCompile(`Verification`)},
}

var listItemRe = regexp.MustCompile(`^\s*(?:[-*+]|\d+[.)])\s`)

// blocks splits markdown into paragraphs, list items and fenced blocks.
func blocks(md string) []string {
	var out []string
	var cur []string
	flush := func() {
		if len(cur) > 0 {
			out = append(out, strings.Join(cur, "\n"))
			cur = nil
		}
	}
	inFence := false
	for _, line := range strings.Split(md, "\n") {
		fence := strings.HasPrefix(strings.TrimSpace(line), "```")
		switch {
		case inFence:
			cur = append(cur, line)
			if fence {
				inFence = false
				flush()
			}
		case fence:
			flush()
			inFence = true
			cur = append(cur, line)
		case strings.TrimSpace(line) == "":
			flush()
		case listItemRe.MatchString(line):
			flush()
			cur = append(cur, line)
		default:
			cur = append(cur, line)
		}
	}
	flush()
	return out
}

// TestSkill_ProtocolFormsBilingual (F-0418 crit. 7): the canonical skill is
// installed in projects of either documentation language, so every block
// that names a protocol form in pt-BR names its English form beside it, and
// the Gauntlet reference no longer claims the forms are pt-BR.
func TestSkill_ProtocolFormsBilingual(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("cerne-authoring", "reference", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range append([]string{filepath.Join("cerne-authoring", "SKILL.md")}, files...) {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, b := range blocks(string(data)) {
			for _, p := range protocolPairs {
				if p.pt.MatchString(b) && !p.en.MatchString(b) {
					t.Errorf("%s: block names %q without %q:\n%s\n", f, p.pt, p.en, b)
				}
			}
		}
	}
	g, err := os.ReadFile(filepath.Join("cerne-authoring", "reference", "gauntlet.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(g), "pt-BR, because they are written inside the spec") {
		t.Error("reference/gauntlet.md still says the forms are pt-BR")
	}
}
