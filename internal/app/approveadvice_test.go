package app

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/cost"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// measuredFeature saves an implemented feature at level whose stamped result
// gives the Gauntlet (critic input) gauntlet of 1000 input tokens.
func measuredFeature(t *testing.T, repoDir, id, level string, gauntlet int64) {
	t.Helper()
	if err := store.SaveFeature(repoDir, store.Feature{ID: id, Title: "Feature " + id, State: domain.StateImplemented}); err != nil {
		t.Fatal(err)
	}
	total := cost.Counters{Input: 1000}
	doc, err := cost.Stamp("---\ncerne_id: "+id+"\n---\n\n# t\n", cost.Report{ID: id, Counters: total, Messages: 1,
		ByModel: map[string]cost.Counters{"m": total}, ByRole: map[string]cost.Counters{"critic": {Input: gauntlet}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(FeatureDocPath(repoDir, id, "spec.md")), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"result.md": doc, "spec.md": "# t\n\n**Nível:** " + level + "\n"} {
		if err := os.WriteFile(FeatureDocPath(repoDir, id, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// adviceSpec is a real spec at level with bar as its bar line.
func adviceSpec(level, bar string) string {
	return "---\ncerne_id: F-0001\n---\n\n# t\n\n**Nível:** " + level + " — porquê\n\n## Objetivo e critérios de aceitação\n\n1. um\n\n## Verificação\n\n" +
		bar + "\nMétodo: comparação cega\nSaída: vencer ou empatar\nPergunta ao juiz: qual decide melhor?\n"
}

const (
	namedBar   = "Barra: o jeito de hoje — docs/x.md"
	notApplBar = "Barra: não aplicável (efeito local, coberto por TestX)"
	waivedBar  = `Barra: dispensada por decisão humana ("pode pular")`
)

func adviceCodes(as []ApproveAdvice) string {
	var codes []string
	for _, a := range as {
		codes = append(codes, a.Code)
	}
	return strings.Join(codes, ",")
}

// TestApproveAdvices (F-0390 crit. 1-3, 6): the measured cost on a named bar,
// the light-with-bar and standard-without-bar advice, each citing the
// project's own measured share and no note of another project — never an
// impediment, never a refused approve.
func TestApproveAdvices(t *testing.T) {
	for _, tc := range []struct {
		name, level, bar, codes string
		wants               []string
	}{
		{"light named bar", "leve", namedBar, "gauntlet-cost,light-with-bar", []string{
			"Gauntlet cost measured in this project (critic + judge): median 250 weighted tokens per feature, 25% of its cost, over 4 delivered features; light: 200 tokens, 20% (3 features)",
			`level light with a named bar: when the tests suffice, a light change can use "Barra: não aplicável (<por que os testes bastam>)" — the Gauntlet took 20% of a light feature's cost here`,
		}},
		{"standard named bar", "padrão", namedBar, "gauntlet-cost", nil},
		{"no level, named bar", "", namedBar, "gauntlet-cost", nil},
		{"standard not applicable", "padrão", notApplBar, "standard-without-bar", []string{
			`level standard with "Barra: não aplicável": a behaviour or contract change usually goes through the Gauntlet — it took 25% of a feature's cost here; approving accepts the declared justification: "efeito local, coberto por TestX"`,
		}},
		{"advanced not applicable", "avançado", notApplBar, "standard-without-bar", nil},
		{"light not applicable", "leve", notApplBar, "", nil},
		{"standard waived", "padrão", waivedBar, "", nil},
		{"no bar", "padrão", "Sem barra.", "", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repoDir, projDir, _ := newFeatureBackend(t)
			seedCandidateSpec(t, repoDir, "F-0001", adviceSpec(tc.level, tc.bar))
			for id, g := range map[string]int64{"F-0002": 200, "F-0003": 100, "F-0004": 300} {
				measuredFeature(t, repoDir, id, "leve", g)
			}
			measuredFeature(t, repoDir, "F-0005", "padrão", 400)

			got := ApproveAdvices(repoDir, projDir, "F-0001")
			if c := adviceCodes(got); c != tc.codes {
				t.Fatalf("codes = %q, want %q (%+v)", c, tc.codes, got)
			}
			for _, a := range got {
				if strings.Contains(a.Message, "N-0056") {
					t.Errorf("%s cites a note of another project (crit. 2): %q", a.Code, a.Message)
				}
			}
			for i, want := range tc.wants {
				if got[i].Message != want {
					t.Errorf("advice %d =\n  %q\nwant\n  %q", i, got[i].Message, want)
				}
			}
			// Never an impediment (crit. 6): the readiness carries none of it.
			imps, err := ApproveReadiness(repoDir, projDir, "F-0001")
			if err != nil {
				t.Fatal(err)
			}
			for _, im := range imps {
				if strings.HasPrefix(im.Code, "gauntlet-cost") || im.Code == AdviceLightWithBar || im.Code == AdviceStandardWithoutBar {
					t.Errorf("advice leaked into the impediments: %+v", im)
				}
			}
		})
	}
}

// TestApproveAdvicesNotEnough (F-0390 crit. 1-2): below 3 measured features
// the advice says so, with no number.
func TestApproveAdvicesNotEnough(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	seedCandidateSpec(t, repoDir, "F-0001", adviceSpec("leve", namedBar))
	measuredFeature(t, repoDir, "F-0002", "leve", 200)
	got := ApproveAdvices(repoDir, projDir, "F-0001")
	want := []ApproveAdvice{
		{AdviceGauntletCost, "Gauntlet cost measured in this project (critic + judge): not enough measured features yet (1 of 3 needed)"},
		{AdviceLightWithBar, `level light with a named bar: when the tests suffice, a light change can use "Barra: não aplicável (<por que os testes bastam>)" — not enough measured features yet to show the Gauntlet's cost here`},
	}
	if len(got) != len(want) {
		t.Fatalf("advice = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("advice %d =\n  %+v\nwant\n  %+v", i, got[i], want[i])
		}
	}
}

// TestApproveAdvicesLightModeAndApprove (F-0390 crit. 6): a project in light
// mode gets no advice — no Gauntlet runs there — and the advice never stops
// the approve itself.
func TestApproveAdvicesLightModeAndApprove(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	seedCandidateSpec(t, repoDir, "F-0001", adviceSpec("leve", namedBar))
	saveGauntlet(t, projDir, "off")
	if got := ApproveAdvices(repoDir, projDir, "F-0001"); len(got) != 0 {
		t.Fatalf("light mode: advice = %+v; want none", got)
	}
	saveGauntlet(t, projDir, "on")
	if got := ApproveAdvices(repoDir, projDir, "F-0001"); len(got) == 0 {
		t.Fatal("gauntlet on: want advice before the approve")
	}
	if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionApprove, "", "", nil); err != nil {
		t.Fatalf("approve with advice: %v", err)
	}
}

// breakMeasurement corrupts what GauntletCostStats reads, one way per name: a
// feature JSON that does not parse (LoadAllFeatures fails) or a measured
// feature's spec.md that cannot be read.
var breakMeasurement = map[string]func(t *testing.T, repoDir string){
	"corrupt feature JSON": func(t *testing.T, repoDir string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(repoDir, "entities", "features", "F-0099.json"), []byte("{"), 0o644); err != nil {
			t.Fatal(err)
		}
	},
	"unreadable spec": func(t *testing.T, repoDir string) {
		t.Helper()
		measuredFeature(t, repoDir, "F-0002", "leve", 200)
		p := FeatureDocPath(repoDir, "F-0002", "spec.md")
		if err := os.Chmod(p, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(p, 0o644) })
		if _, err := os.ReadFile(p); err == nil {
			t.Skip("the spec stays readable (running as root)")
		}
	},
}

// TestApproveAdvicesFailOpen (F-0390, critic round): advice never breaks a
// surface. When the measurement fails, the cost advice is skipped, the level
// advice still runs with no number, and the impediments are the same as
// before.
func TestApproveAdvicesFailOpen(t *testing.T) {
	for name, breakIt := range breakMeasurement {
		t.Run(name, func(t *testing.T) {
			repoDir, projDir, _ := newFeatureBackend(t)
			seedCandidateSpec(t, repoDir, "F-0001", adviceSpec("leve", namedBar))
			before, err := ApproveReadiness(repoDir, projDir, "F-0001")
			if err != nil {
				t.Fatal(err)
			}
			breakIt(t, repoDir)
			got := ApproveAdvices(repoDir, projDir, "F-0001")
			want := []ApproveAdvice{{AdviceLightWithBar, `level light with a named bar: when the tests suffice, a light change can use "Barra: não aplicável (<por que os testes bastam>)" — not enough measured features yet to show the Gauntlet's cost here`}}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("advice = %+v, want %+v", got, want)
			}
			after, err := ApproveReadiness(repoDir, projDir, "F-0001")
			if err != nil || !reflect.DeepEqual(after, before) {
				t.Fatalf("impediments changed: %+v (%v), want %+v", after, err, before)
			}
		})
	}
}
