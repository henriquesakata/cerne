package app

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/lint"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
)

const (
	gateSpecNumbered = "# Spec\n\n## Objetivo e critérios de aceitação\n\n1. um\n2. dois\n3. três\n4. quatro\n" + testBarTail
	gateSpecPlain    = "# Spec\n\nConteúdo real da spec, sem critérios numerados.\n" + testBarTail
	gateGauntletOK   = testGauntletTail
	gateGauntletNA   = "\n## Gauntlet\n\nBarra: não aplicável (fixture de teste)\n"
	gateCite1234     = "Provas: critério 1, critério 2, critério 3, critério 4.\n"
)

// seedCompletable seeds an in_development feature with the given spec/result
// bodies, pushed so the transition's rebase finds a clean clone.
func seedCompletable(t *testing.T, repoDir, spec, result string) {
	t.Helper()
	seedFeatureInState(t, repoDir, "F-0001", domain.StateInDevelopment, "spec.md", "plan.md", "result.md")
	for doc, body := range map[string]string{"spec": spec, "result": result} {
		if err := os.WriteFile(FeatureDocPath(repoDir, "F-0001", doc+".md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := gitback.CommitAndPush(repoDir, "seed docs"); err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}
}

func resultBody(cites, tail string) string {
	return "# Result\n\nResultado real da entrega.\n\n" + cites + tail
}

func TestCompleteRefusesResultWithoutGauntletRecord(t *testing.T) {
	cases := map[string]string{
		"no section":               resultBody(gateCite1234, ""),
		"section without citation": resultBody(gateCite1234, "\n## Gauntlet\n\nBarra: fixture de teste — referência nomeada\nRodada 1 — vitória\n"),
		"section without verdict":  resultBody(gateCite1234, "\n## Gauntlet\n\nBarra: fixture de teste — referência nomeada\nRodada 1: \"fixture\"\n"),
	}
	for name, result := range cases {
		t.Run(name, func(t *testing.T) {
			repoDir, projDir, _ := newFeatureBackend(t)
			seedCompletable(t, repoDir, gateSpecNumbered, result)
			headBefore, _ := gitback.HeadSHA(repoDir)
			evBefore, _ := store.ReadEvents(repoDir)

			_, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionComplete, "", "", nil)
			if !errors.Is(err, ErrResultIncomplete) {
				t.Fatalf("err = %v, want ErrResultIncomplete", err)
			}
			if got := reload(t, repoDir, "F-0001").State; got != domain.StateInDevelopment {
				t.Fatalf("state = %s, want in_development", got)
			}
			evAfter, _ := store.ReadEvents(repoDir)
			if len(evAfter) != len(evBefore) {
				t.Fatalf("events grew %d -> %d on a refused complete", len(evBefore), len(evAfter))
			}
			if headAfter, _ := gitback.HeadSHA(repoDir); headAfter != headBefore {
				t.Fatalf("HEAD moved %s -> %s on a refused complete", headBefore, headAfter)
			}
		})
	}
}

func TestCompleteRefusesResultMissingCriteria(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	seedCompletable(t, repoDir, gateSpecNumbered, resultBody("Provas: critério 1 e critério 3.\n", gateGauntletOK))

	_, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionComplete, "", "", nil)
	if !errors.Is(err, ErrResultIncomplete) {
		t.Fatalf("err = %v, want ErrResultIncomplete", err)
	}
	if !strings.Contains(err.Error(), "2, 4") {
		t.Fatalf("error %q does not list the missing criteria 2, 4", err)
	}
	if got := reload(t, repoDir, "F-0001").State; got != domain.StateInDevelopment {
		t.Fatalf("state = %s, want in_development", got)
	}
}

func TestCompleteAcceptsRecordedResult(t *testing.T) {
	cases := []struct {
		name, spec, result string
		auto               bool
		want               domain.FeatureState
	}{
		{"full record", gateSpecNumbered, resultBody(gateCite1234, gateGauntletOK), false, domain.StateImplemented},
		{"literal exemption", gateSpecNumbered, resultBody(gateCite1234, gateGauntletNA), false, domain.StateImplemented},
		{"spec without numbered criteria", gateSpecPlain, resultBody("", gateGauntletOK), false, domain.StateImplemented},
		{"accept auto", gateSpecNumbered, resultBody(gateCite1234, gateGauntletOK), true, domain.StateDone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repoDir, projDir, _ := newFeatureBackend(t)
			if tc.auto {
				cfg := project.Config{ProjectID: "demo", Backend: "b", Gates: project.GatesConfig{Accept: project.GateAuto}}
				if err := project.SaveConfig(projDir, cfg); err != nil {
					t.Fatal(err)
				}
			}
			seedCompletable(t, repoDir, tc.spec, tc.result)
			if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionComplete, "", "", nil); err != nil {
				t.Fatalf("complete: %v", err)
			}
			if got := reload(t, repoDir, "F-0001").State; got != tc.want {
				t.Fatalf("state = %s, want %s", got, tc.want)
			}
		})
	}
}

// The gate and the lint call the same function, so for every (spec, result)
// pair the gate refuses iff the lint would flag the feature once completed.
func TestResultGateParityWithLint(t *testing.T) {
	pairs := map[string][2]string{
		"clean":                {gateSpecNumbered, resultBody(gateCite1234, gateGauntletOK)},
		"exempt":               {gateSpecNumbered, resultBody(gateCite1234, gateGauntletNA)},
		"no gauntlet":          {gateSpecNumbered, resultBody(gateCite1234, "")},
		"missing criteria":     {gateSpecNumbered, resultBody("critério 1\n", gateGauntletOK)},
		"both":                 {gateSpecNumbered, resultBody("critério 1\n", "")},
		"plain spec, no seção": {gateSpecPlain, resultBody("", "")},
		"plain spec, clean":    {gateSpecPlain, resultBody("", gateGauntletOK)},
		"critic-only round":    {gateSpecPlain, resultBody("", "\n## Gauntlet\n\nBarra: fixture de teste — referência nomeada\nRodada 1 — crítico achou \"x\"\n")},
	}
	for name, p := range pairs {
		t.Run(name, func(t *testing.T) {
			repoDir, projDir, _ := newFeatureBackend(t)
			seedCompletable(t, repoDir, p[0], p[1])
			_, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionComplete, "", "", nil)
			refused := errors.Is(err, ErrResultIncomplete)
			if err != nil && !refused {
				t.Fatalf("unexpected error: %v", err)
			}

			// Force the lint's view: implemented, completed after every cutoff.
			f := reload(t, repoDir, "F-0001")
			f.State = domain.StateImplemented
			if err := store.SaveFeature(repoDir, f); err != nil {
				t.Fatal(err)
			}
			ev := domain.NewEvent("F-0001", "feature.completed", nil, "test")
			ev.At = time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
			if err := store.AppendEvent(repoDir, ev); err != nil {
				t.Fatal(err)
			}
			findings, err := lint.Run(repoDir)
			if err != nil {
				t.Fatal(err)
			}
			flagged := false
			for _, fi := range findings {
				if fi.Entity == "F-0001" && (fi.Rule == lint.RuleResultMissingEvidence || fi.Rule == lint.RuleResultMissingGauntlet) {
					flagged = true
				}
			}
			if refused != flagged {
				t.Fatalf("gate refused=%v but lint flagged=%v", refused, flagged)
			}
		})
	}
}

// TestCompleteGateIgnoresErrataWhileLive: the complete gate charges the result
// while it is still editable, so an erratum carrying the missing criteria and a
// complete Gauntlet section does not open it — fix the result itself (F-0305).
func TestCompleteGateIgnoresErrataWhileLive(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	seedCompletable(t, repoDir, gateSpecNumbered, resultBody("Provas: critério 1, critério 2.\n", ""))
	if _, err := AddErrata(repoDir, projDir, "F-0001", "Provas: critério 3, critério 4.\n"+gateGauntletOK, "dev"); err != nil {
		t.Fatalf("AddErrata: %v", err)
	}
	_, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionComplete, "", "", nil)
	if !errors.Is(err, ErrResultIncomplete) {
		t.Fatalf("err = %v, want ErrResultIncomplete", err)
	}
}
