package skills

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/doclang"
)

// protocolPairs pairs each pt-BR literal of the protocol with its English
// form (internal/doclang): a block of the skill that names the first must
// name the second too (F-0418 crit. 7).
var protocolPairs = []pair{
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
	{regexp.MustCompile(`(^|[^\p{L}])QUANDO($|[^\p{L}])`), regexp.MustCompile(`(^|[^\p{L}])WHEN($|[^\p{L}])`)},
	{regexp.MustCompile(`Verificação`), regexp.MustCompile(`Verification`)},
}

// word matches s as a whole word; Go's \b is ASCII-only, so a letter on
// either side (accented included) means it is part of a longer word.
func word(s string) *regexp.Regexp {
	return regexp.MustCompile(`(^|[^\p{L}])` + regexp.QuoteMeta(s) + `($|[^\p{L}])`)
}

type pair struct{ pt, en *regexp.Regexp }

// sectionPairs pairs every section name of every templated document in pt-BR
// with its English name, straight from internal/doclang (F-0475).
func sectionPairs() []pair {
	var out []pair
	for _, k := range []doclang.Kind{doclang.Spec, doclang.Plan, doclang.Result, doclang.Decision, doclang.Runbook} {
		pt, en := doclang.Sections(k, doclang.PT), doclang.Sections(k, doclang.EN)
		for i := range pt {
			if pt[i] != en[i] {
				out = append(out, pair{word(pt[i]), word(en[i])})
			}
		}
	}
	return out
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
			if strings.HasPrefix(b, "#") {
				continue // headings are lookup keys the guide contract pins as they were
			}
			for _, p := range allPairs() {
				pt, en := p.pt.FindStringIndex(b), p.en.FindStringIndex(b)
				switch {
				case pt != nil && en == nil:
					t.Errorf("%s: block names %q without %q:\n%s\n", f, p.pt, p.en, b)
				case pt != nil && en[0] > pt[0]:
					t.Errorf("%s: block names %q before %q (English first):\n%s\n", f, p.pt, p.en, b)
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

func allPairs() []pair {
	return append(append(append([]pair{}, protocolPairs...), sectionPairs()...), proseFormPairs()...)
}

// proseFormPairs pairs the pt-BR free-text forms the templates teach with
// their English ones, taken from doclang.Examples where it has them (F-0475).
func proseFormPairs() []pair {
	pt, en := doclang.Examples(doclang.PT), doclang.Examples(doclang.EN)
	lit := func(s string) *regexp.Regexp {
		s = strings.TrimSpace(strings.SplitN(strings.Trim(s, "`\""), " / ", 2)[0])
		return regexp.MustCompile(regexp.QuoteMeta(strings.Trim(s, "\"")))
	}
	return []pair{
		{lit(pt.ExpectedOutput), lit(en.ExpectedOutput)},
		{lit(pt.DecisionVoice), lit(en.DecisionVoice)},
		{lit(pt.Parallel), lit(en.Parallel)},
		{regexp.MustCompile(`Nível:`), regexp.MustCompile(`Level:`)},
		{regexp.MustCompile(`Em produção:`), regexp.MustCompile(`In production:`)},
		{regexp.MustCompile(`PENDENTE`), regexp.MustCompile(`UNMET`)},
	}
}

// TestSkill_PtBRAsidesClose (F-0475): every "(pt-BR:" aside opened in the
// guide closes on the same line, so the rule that follows stays outside it.
func TestSkill_PtBRAsidesClose(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join("cerne-authoring", "reference", "*.md"))
	for _, f := range append([]string{filepath.Join("cerne-authoring", "SKILL.md")}, files...) {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for n, line := range strings.Split(string(data), "\n") {
			for rest := line; ; {
				i := strings.Index(rest, "(pt-BR:")
				if i < 0 {
					break
				}
				depth, closed := 0, false
				for _, r := range rest[i:] {
					if r == '(' {
						depth++
					} else if r == ')' {
						depth--
						if depth == 0 {
							closed = true
							break
						}
					}
				}
				if !closed {
					t.Errorf("%s:%d: a (pt-BR: aside never closes: %.100s", f, n+1, rest[i:])
				}
				rest = rest[i+len("(pt-BR:"):]
			}
		}
	}
}

// TestSkill_TopSaysHowToFindTheLanguage (F-0475 crit. 2): before any protocol
// form, the skill says how the agent learns the project's documentation
// language.
func TestSkill_TopSaysHowToFindTheLanguage(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("cerne-authoring", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	if i := strings.Index(body, "\n## "); i >= 0 {
		body = body[:i]
	}
	for _, want := range []string{"DOCS LANGUAGE", "headings of the stub"} {
		if !strings.Contains(body, want) {
			t.Errorf("top of SKILL.md lacks %q", want)
		}
	}
}
