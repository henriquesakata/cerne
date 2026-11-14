package lint

import (
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/doccheck"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/spectmpl"
	"github.com/henriquesakata/cerne/internal/store"
)

// TestSpecMissingBarRule (F-0308 crit. 4): a candidate whose written spec has
// no valid bar is flagged before anyone tries the approve; a valid bar, a stub
// spec (incomplete-doc's business) or a feature past candidacy is not.
func TestSpecMissingBarRule(t *testing.T) {
	cases := []struct {
		name       string
		state      domain.FeatureState
		spec       string
		wantDetail string // "" = no finding
	}{
		{"candidate without bar flags", domain.StateCandidate, "# t\n\n## Problema\n\nProblema real.\n", `no "Barra:" line`},
		{"candidate with malformed bar flags", domain.StateCandidate, "# t\n\nProblema real.\n\nBarra: não aplicável\n", "justification"},
		{"candidate with valid bar is clean", domain.StateCandidate, "# t\n\nProblema real.\n\nBarra: referência nomeada — acesso\n", ""},
		{"stub spec is clean", domain.StateCandidate, "---\ncerne_id: F-0001\n---\n\n" + spectmpl.Stub("t"), ""},
		{"approved without bar is clean", domain.StateApproved, "# t\n\n## Problema\n\nProblema real.\n", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			f := store.Feature{ID: "F-0001", Title: "t", State: tc.state}
			if err := store.SaveFeature(dir, f); err != nil {
				t.Fatal(err)
			}
			writeDoc(t, dir, "content/features/F-0001/spec.md", tc.spec)

			got, err := Run(dir)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			var bar []Finding
			for _, fd := range got {
				if fd.Rule == RuleSpecMissingBar {
					bar = append(bar, fd)
				}
			}
			if tc.wantDetail == "" {
				if len(bar) != 0 {
					t.Fatalf("want no %s finding, got %v", RuleSpecMissingBar, bar)
				}
				return
			}
			if len(bar) != 1 || bar[0].Entity != "F-0001" || bar[0].File != "content/features/F-0001/spec.md" {
				t.Fatalf("want one finding on F-0001's spec, got %v", bar)
			}
			for _, want := range []string{tc.wantDetail, "cerne feature approve F-0001 --check"} {
				if !strings.Contains(bar[0].Detail, want) {
					t.Fatalf("detail %q lacks %q", bar[0].Detail, want)
				}
			}
		})
	}
}

// TestBarReadersAgreeOnLineLayouts pins one bar-line recognizer for the
// approve gate (doccheck.SpecBar) and the result reader (parseGauntlet):
// the same line is a bar line to both or to neither (F-0327).
func TestBarReadersAgreeOnLineLayouts(t *testing.T) {
	for line, want := range map[string]bool{
		"Barra: R-0008 — docs/design/x.png":           true,
		"- Barra: R-0008 — docs/design/x.png":         true,
		"**Barra:** R-0008 — docs/design/x.png":       true,
		"- **Barra:** R-0008 — docs/design/x.png":     true,
		"> Barra: R-0008 — docs/design/x.png":         true,
		"1. Barra: R-0008 — docs/design/x.png":        true,
		"### Barra: R-0008 — docs/design/x.png":       true,
		"barra: R-0008 — docs/design/x.png":           true,
		"Barra (da spec): R-0008 — docs/design/x.png": true,
		"Barras: R-0008 — docs/design/x.png":          false,
		"Barra:":                                      false,
		"A Barra: R-0008 — docs/design/x.png":         false,
	} {
		t.Run(line, func(t *testing.T) {
			_, err := doccheck.SpecBar([]byte("# t\n\n" + line + "\n"))
			spec := err == nil
			result := parseGauntlet(line).bar != ""
			if spec != want || result != want {
				t.Fatalf("approve gate %v, result reader %v, want both %v", spec, result, want)
			}
		})
	}
}

// TestBarReadersAgreeOnValues pins one value rule for both readers (F-0327):
// a template slot or nothing written is no bar to either, and the same
// written exemptions are exemptions to both. A malformed exemption is refused
// by the approve gate and is no exemption to the result reader, which still
// keeps its written text as a bar that needs judged rounds.
func TestBarReadersAgreeOnValues(t *testing.T) {
	type want struct{ approve, resultBar, resultExempt bool }
	for line, w := range map[string]want{
		"Barra: R-0008 <recorte do painel>":                               {true, true, false},
		"Barra: <https://kiro.dev/docs/specs>":                            {true, true, false},
		"Barra: não aplicável (refator sem comportamento novo)":           {true, true, true},
		"Barra: não-aplicável (refator sem comportamento novo)":           {true, true, true},
		"Barra: **não aplicável** (refator sem comportamento novo)":       {true, true, true},
		"Barra: n\u00e3o\u00a0aplic\u00e1vel (refator puro)":              {true, true, true},
		"Barra: dispensada por decisão humana (\"pode seguir sem\")":      {true, true, true},
		"Barra: não aplicável (correção pontual de backend, sem artefato": {true, true, true},
		"Barra: <referência + acesso>":                                    {false, false, false},
		"Barra: \"<referência + acesso>\"":                                {false, false, false},
		"Barra: `<referência + acesso>`":                                  {false, false, false},
		"Barra: <referência> — <acesso>":                                  {false, false, false},
		"Barra: **":                                                       {false, false, false},
		"Barra: não aplicável":                                            {false, true, false},
		"Barra: não aplicável ( )":                                        {false, true, false},
		"Barra: não aplicável (<justificativa>)":                          {false, true, false},
		"Barra: **não aplicável** (<justificativa>)":                      {false, true, false},
		"Barra: dispensada por decisão humana (\"<citação>\")":            {false, true, false},
		"Barra: dispensada por decisão humana (“<citação>”)":              {false, true, false},
	} {
		t.Run(line, func(t *testing.T) {
			_, err := doccheck.SpecBar([]byte("# t\n\n" + line + "\n"))
			rec := parseGauntlet(line)
			if got := (want{err == nil, rec.bar != "", rec.exempt}); got != w {
				t.Fatalf("approve, result bar, result exempt = %+v, want %+v", got, w)
			}
		})
	}
}

// TestSpecMissingBar_English (F-0418 crit. 3): in a project documented in
// English the finding names the English label, for a missing line and for a
// malformed one the author wrote in English.
func TestSpecMissingBar_English(t *testing.T) {
	for _, tc := range []struct{ spec, want string }{
		{"# t\n\n## Problem\n\nA real problem.\n", `no "Bar:" line`},
		{"# t\n\n## Problem\n\nA real problem.\n\nBar: <named reference>\n", `malformed "Bar:" line: "Bar:" holds only`},
	} {
		dir := t.TempDir()
		if err := store.SaveProject(dir, store.Project{DocsLanguage: "en"}); err != nil {
			t.Fatal(err)
		}
		if err := store.SaveFeature(dir, store.Feature{ID: "F-0001", Title: "t", State: domain.StateCandidate}); err != nil {
			t.Fatal(err)
		}
		writeDoc(t, dir, "content/features/F-0001/spec.md", tc.spec)
		got, err := Run(dir)
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		found := false
		for _, f := range got {
			if f.Rule == RuleSpecMissingBar {
				found = true
				if !strings.Contains(f.Detail, tc.want) || strings.Contains(f.Detail, "Barra") {
					t.Fatalf("detail = %q, want %q and no Barra", f.Detail, tc.want)
				}
			}
		}
		if !found {
			t.Fatalf("no spec-missing-bar finding for %q: %+v", tc.spec, got)
		}
	}
}
