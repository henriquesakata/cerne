package lint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

func writeResultFixture(t *testing.T, state domain.FeatureState, spec, result string) (string, store.Feature) {
	t.Helper()
	dir := t.TempDir()
	f := store.Feature{ID: "F-0001", Title: "demo", State: state, CreatedAt: time.Unix(0, 0).UTC()}
	if err := store.SaveFeature(dir, f); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}
	docDir := filepath.Join(dir, "content", "features", f.ID)
	if err := os.MkdirAll(docDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"spec.md": spec, "result.md": result} {
		if err := os.WriteFile(filepath.Join(docDir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir, f
}

const checkSpec = "## Objetivo e critérios de aceitação\n\n1. a\n2. b\n"

func TestCheckResultIgnoresStateAndCutoffs(t *testing.T) {
	dir, f := writeResultFixture(t, domain.StateInDevelopment, checkSpec, "critério 1 ok\n")
	got := CheckResult(dir, f)
	if len(got) != 2 || got[0].Rule != RuleResultMissingEvidence || got[1].Rule != RuleResultMissingGauntlet {
		t.Fatalf("want evidence then gauntlet findings on an in_development feature, got %+v", got)
	}
}

func TestCheckResultCleanRecord(t *testing.T) {
	result := "critério 1 e critério 2 provados\n\n## Gauntlet\n\nBarra: não aplicável (fixture)\n"
	dir, f := writeResultFixture(t, domain.StateInDevelopment, checkSpec, result)
	if got := CheckResult(dir, f); len(got) != 0 {
		t.Fatalf("want no findings, got %+v", got)
	}
}

// An HTML comment is template scaffold, never record: a Barra line written
// above the template's leftover Gauntlet guide (whose literal forms carry a
// verdict word and a quote) must not pass as a judged round, and a citation
// inside a comment is not evidence.
func TestCheckResultIgnoresHTMLComments(t *testing.T) {
	guide := "<!-- Linha de cada rodada: Rodada N — <vitória|derrota|empate>: \"<citação curta do veredito do juiz>\" -->\n"
	cases := map[string]struct {
		result string
		want   []string
	}{
		"bar over leftover guide": {
			result: "critério 1 e critério 2\n\n## Gauntlet\n" + guide + "Barra: x\n",
			want:   []string{RuleResultMissingGauntlet},
		},
		"whole round inside a comment": {
			result: "critério 1 e critério 2\n\n## Gauntlet\nBarra: x\n<!--\nRodada 1 — vitória: \"ok\"\n-->\n",
			want:   []string{RuleResultMissingGauntlet},
		},
		"citation inside a comment": {
			result: "critério 1 <!-- critério 2 -->\n\n## Gauntlet\nBarra: x\nRodada 1 — vitória: \"ok\"\n",
			want:   []string{RuleResultMissingEvidence},
		},
		"exemption inside a comment": {
			result: "critério 1 e critério 2\n\n## Gauntlet\n<!-- Barra: não aplicável (x) -->\n",
			want:   []string{RuleResultMissingGauntlet},
		},
		"record outside, guide left in place": {
			result: "critério 1 e critério 2\n\n## Gauntlet\n" + guide + "Barra: x\nRodada 1 — vitória: \"ok\"\n",
			want:   nil,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			dir, f := writeResultFixture(t, domain.StateInDevelopment, checkSpec, tc.result)
			var got []string
			for _, fi := range CheckResult(dir, f) {
				got = append(got, fi.Rule)
			}
			if len(got) != len(tc.want) || (len(got) > 0 && got[0] != tc.want[0]) {
				t.Fatalf("rules = %v, want %v", got, tc.want)
			}
		})
	}
}

func checkRules(t *testing.T, result string) []string {
	t.Helper()
	dir, f := writeResultFixture(t, domain.StateInDevelopment, checkSpec, result)
	var got []string
	for _, fi := range CheckResult(dir, f) {
		got = append(got, fi.Rule)
	}
	return got
}

// A template placeholder "<…>" left unfilled is not a record: not a bar, not
// an exemption's justification, not the judge's citation (critic F-0303 A1).
func TestCheckResultRefusesPlaceholders(t *testing.T) {
	const cites = "critério 1 e critério 2\n\n"
	refused := map[string]string{
		"template forms":       "## Gauntlet\nBarra: <referência nomeada + acesso (URL, comando ou arquivo)>\nRodada 1 — vitória: \"<citação curta do veredito do juiz>\"\n",
		"skill example":        "## Gauntlet\nBarra: <referência + acesso>\nRodada <n> — <vitória|derrota|empate>: \"<citação curta do veredito do juiz>\"\nAchados do crítico: <n> — corrigidos <…>\n",
		"placeholder bar":      "## Gauntlet\nBarra: <referência>\nRodada 1 — vitória: \"o juiz preferiu A\"\n",
		"placeholder quote":    "## Gauntlet\nBarra: R-0008\nRodada 1 — vitória: “<citação>”\n",
		"exemption not filled": "## Gauntlet\nBarra: não aplicável (<justificativa>)\n",
		"draft clarify marker": "## Gauntlet\n[ESCLARECER: registro do Gauntlet: barra, rodadas com o veredito citado do juiz e disposição dos achados]\n",
		"dispensa not filled":  "## Gauntlet\nBarra: dispensada por decisão humana ( <citação> )\n",
	}
	for name, sec := range refused {
		t.Run(name, func(t *testing.T) {
			if got := checkRules(t, cites+sec); len(got) != 1 || got[0] != RuleResultMissingGauntlet {
				t.Fatalf("rules = %v, want [%s]", got, RuleResultMissingGauntlet)
			}
		})
	}
	for name, sec := range map[string]string{
		"filled":           "## Gauntlet\nBarra: R-0008 <recorte do painel>\nRodada 1 — vitória: \"<b>A</b> lê melhor\"\n",
		"exemption filled": "## Gauntlet\nBarra: não aplicável (refator sem comportamento novo)\n",
		"dispensa filled":  "## Gauntlet\nBarra: dispensada por decisão humana (\"pode seguir sem\")\n",
	} {
		t.Run(name, func(t *testing.T) {
			if got := checkRules(t, cites+sec); len(got) != 0 {
				t.Fatalf("rules = %v, want none", got)
			}
		})
	}
}

// Every "## Gauntlet" section is read and one complete record is enough: a
// template copy or a narrative section above the real record must not hide it
// (critic F-0303 A2). With none complete, the last section's problem is told:
// the template puts the record last, and copies or narrative sit above it.
func TestCheckResultReadsEveryGauntletSection(t *testing.T) {
	const cites = "critério 1 e critério 2\n\n"
	codeCopy := "## O que mudou\n\n```markdown\n## Gauntlet\n<!-- guia -->\n```\n\n"
	for name, result := range map[string]string{
		"template copy in a code block": cites + codeCopy + "## Gauntlet\nBarra: R-0008\nRodada 1 — vitória: \"A lê melhor\"\n",
		"exemption below a code copy":   cites + codeCopy + "## Gauntlet\nBarra: não aplicável (refator puro)\n",
		"narrative then record":         cites + "## Gauntlet\nO loop rodou duas vezes.\n\n## Gauntlet\nBarra: R-0008\nRodada 1 — vitória: \"A lê melhor\"\n",
	} {
		t.Run(name, func(t *testing.T) {
			if got := checkRules(t, result); len(got) != 0 {
				t.Fatalf("rules = %v, want none", got)
			}
		})
	}
	dir, f := writeResultFixture(t, domain.StateInDevelopment, checkSpec, cites+"## O que mudou\n\n```\n## Gauntlet\n```\n\n## Gauntlet\nBarra: R-0008\nRodada 1 — vitória\n")
	got := CheckResult(dir, f)
	if len(got) != 1 || !strings.Contains(got[0].Detail, "rodada 1 sem citação do juiz") {
		t.Fatalf("want the last section's problem (the record, not the copy above it), got %+v", got)
	}
}

// A comment opener written as code ("`<!--`") is not a comment: the text up
// to a "`-->`" further down stays readable (critic F-0303 A4).
func TestCheckResultCommentMarkersInCode(t *testing.T) {
	result := "critério 1 usa `<!--`\n\ncritério 2 fecha com `-->`\n\n## Gauntlet\nBarra: R-0008\nRodada 1 — vitória: \"ok\"\n"
	if got := checkRules(t, result); len(got) != 0 {
		t.Fatalf("rules = %v, want none", got)
	}
}

func addErrata(t *testing.T, dir string, entries ...string) {
	t.Helper()
	for _, e := range entries {
		if err := store.AppendErrata(dir, "F-0001", e, "ator", time.Unix(0, 0).UTC()); err != nil {
			t.Fatal(err)
		}
	}
}

const goodGauntlet = "## Gauntlet\n\nBarra: x\n\nRodada 1 — vitória: \"ok\"\n"
const badGauntlet = "## Gauntlet\n\nBarra: x\n\nRodada 1 — vitória\n"

func TestResultEvidenceReadsErrata(t *testing.T) {
	spec := "## Objetivo e critérios de aceitação\n\n1. a\n2. b\n3. c\n"
	dir, f := writeResultFixture(t, domain.StateImplemented, spec, "critérios 1 e 2\n")
	if got := evidenceFinding(dir, f); len(got) != 1 {
		t.Fatalf("want a finding before errata, got %+v", got)
	}
	addErrata(t, dir, "critério 3 provado\n")
	if got := evidenceFinding(dir, f); len(got) != 0 {
		t.Fatalf("want no finding, got %+v", got)
	}
}

func TestResultGauntletReadsErrata(t *testing.T) {
	for name, entries := range map[string][]string{
		"complete":         {goodGauntlet},
		"incomplete first": {badGauntlet, goodGauntlet},
		"complete first":   {goodGauntlet, badGauntlet},
	} {
		t.Run(name, func(t *testing.T) {
			dir, f := writeResultFixture(t, domain.StateImplemented, checkSpec, "critério 1 e critério 2\n\n"+badGauntlet)
			if got := gauntletFinding(dir, f, false); len(got) != 1 {
				t.Fatalf("want a finding before errata, got %+v", got)
			}
			addErrata(t, dir, entries...)
			if got := gauntletFinding(dir, f, false); len(got) != 0 {
				t.Fatalf("want no finding, got %+v", got)
			}
		})
	}
}

func TestResultRulesErrataWithoutEvidence(t *testing.T) {
	dir, f := writeResultFixture(t, domain.StateImplemented, checkSpec, "critério 1\n")
	addErrata(t, dir, "outro assunto\n", badGauntlet, "<!-- critério 2 -->\n"+"<!--\n"+goodGauntlet+"-->\n")
	got := CheckResult(dir, f)
	if len(got) != 2 || got[0].Rule != RuleResultMissingEvidence || got[1].Rule != RuleResultMissingGauntlet {
		t.Fatalf("want both findings, got %+v", got)
	}
}

// TestResultRulesErrataOnlyOnceClosed: while the feature is live its result is
// still the document to fix, so an erratum does not settle the record — the
// complete gate must not be passed by `feature errata` (F-0305 crit. 1-2 scope
// the errata to the closed states). The same errata on implemented clears.
func TestResultRulesErrataOnlyOnceClosed(t *testing.T) {
	for _, tc := range []struct {
		state domain.FeatureState
		want  int
	}{
		{domain.StateInDevelopment, 2},
		{domain.StateImplemented, 0},
		{domain.StateDone, 0},
	} {
		t.Run(string(tc.state), func(t *testing.T) {
			dir, f := writeResultFixture(t, tc.state, checkSpec, "critério 1\n")
			addErrata(t, dir, "critério 2 provado\n\n"+goodGauntlet)
			if got := CheckResult(dir, f); len(got) != tc.want {
				t.Fatalf("want %d findings, got %+v", tc.want, got)
			}
		})
	}
}

// TestResultGauntletDetailNamesTheResult: the finding's File is result.md, so
// its detail is the result's own problem — an incomplete errata section can
// vouch (when complete) but never author the detail (F-0305). With no section
// in the result, the detail says so.
func TestResultGauntletDetailNamesTheResult(t *testing.T) {
	noBar := "## Gauntlet\n\nRodada 1 — vitória: \"ok\"\n"
	for _, tc := range []struct{ name, result, want string }{
		{"result section", "critério 1 e critério 2\n\n" + badGauntlet, "rodada 1 sem citação do juiz"},
		{"no result section", "critério 1 e critério 2\n", `sem seção "## Gauntlet"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, f := writeResultFixture(t, domain.StateImplemented, checkSpec, tc.result)
			addErrata(t, dir, noBar)
			got := gauntletFinding(dir, f, false)
			if len(got) != 1 || !strings.Contains(got[0].Detail, "result.md "+tc.want) {
				t.Fatalf("want one finding with detail %q, got %+v", tc.want, got)
			}
		})
	}
}

// TestResultGauntletErrataHeldToStrictReader: the legacy predicate vouches only
// for the result's own sections — an erratum is written now, so a legacy-only
// record in it does not clear the finding, while the same record in the result
// of a pre-cutoff feature still does (judge finding, F-0305).
func TestResultGauntletErrataHeldToStrictReader(t *testing.T) {
	legacyOnly := "## Gauntlet\n\nRodada 1 — vitória: \"ok\"\n"
	dir, f := writeResultFixture(t, domain.StateImplemented, checkSpec, "critério 1 e critério 2\n")
	addErrata(t, dir, legacyOnly)
	if got := gauntletFinding(dir, f, true); len(got) != 1 {
		t.Fatalf("legacy-only erratum: want one finding, got %+v", got)
	}
	dir, f = writeResultFixture(t, domain.StateImplemented, checkSpec, "critério 1 e critério 2\n\n"+legacyOnly)
	if got := gauntletFinding(dir, f, true); len(got) != 0 {
		t.Fatalf("legacy-only result before the cutoff: want no finding, got %+v", got)
	}
}

// TestResultGauntletErrataEntriesDoNotJoin: an errata entry ends at the next
// entry's "## errata" heading, so a Barra in one entry and a cited round in the
// next never add up to one record (judge finding, F-0305).
func TestResultGauntletErrataEntriesDoNotJoin(t *testing.T) {
	dir, f := writeResultFixture(t, domain.StateImplemented, checkSpec, "critério 1 e critério 2\n")
	addErrata(t, dir, "## Gauntlet\n\nBarra: x\n", "Rodada 1 — vitória: \"ok\" — VEREDITO: A\n")
	if got := gauntletFinding(dir, f, false); len(got) != 1 {
		t.Fatalf("want one finding, got %+v", got)
	}
}

// F-0307 crit. 1: both result findings say the feature's state and the command
// that corrects the result in that state — read from Correction, not repeated.
func TestResultFindingsNameStateAndPath(t *testing.T) {
	for _, state := range []domain.FeatureState{domain.StateInDevelopment, domain.StateImplemented, domain.StateDone} {
		dir, f := writeResultFixture(t, state, checkSpec, "texto sem prova\n")
		got := CheckResult(dir, f)
		if len(got) != 2 {
			t.Fatalf("%s: want evidence + gauntlet findings, got %+v", state, got)
		}
		_, cmd := Correction(state, f.ID, "result")
		for _, fd := range got {
			if !strings.Contains(fd.Detail, "feature "+string(state)+":") {
				t.Errorf("%s %s: detail lacks the state: %q", state, fd.Rule, fd.Detail)
			}
			if !strings.Contains(fd.Detail, "'"+cmd+"'") {
				t.Errorf("%s %s: detail lacks the command %q: %q", state, fd.Rule, cmd, fd.Detail)
			}
			if !strings.Contains(fd.Detail, ". Forma: ") {
				t.Errorf("%s %s: detail lacks the form: %q", state, fd.Rule, fd.Detail)
			}
		}
	}
}
