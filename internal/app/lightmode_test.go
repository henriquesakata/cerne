package app

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/project"
)

// specNoBar is a real spec (numbered criteria) with no bar line at all.
const specNoBar = "# Spec\n\n## Objetivo e critérios de aceitação\n\n1. um\n2. dois\n3. três\n4. quatro\n"

func saveGauntlet(t *testing.T, projDir, v string) {
	t.Helper()
	cfg := project.Config{ProjectID: "demo", Backend: "b", Gates: project.GatesConfig{Gauntlet: v}}
	if err := project.SaveConfig(projDir, cfg); err != nil {
		t.Fatal(err)
	}
}

// TestApprove_LightMode (F-0364 crit. 1, 2): with gauntlet = "off" a real spec
// with no bar line is approvable — the core accepts it and the readiness the
// CLI check and the UI button read lists no bar impediment. Any other value
// keeps the bar gate.
func TestApprove_LightMode(t *testing.T) {
	for _, v := range []string{"off", "", "on", "of", "OFF", "false"} {
		t.Run("gauntlet="+v, func(t *testing.T) {
			repoDir, projDir, _ := newFeatureBackend(t)
			saveGauntlet(t, projDir, v)
			seedCandidateSpec(t, repoDir, "F-0001", specNoBar)

			imps, err := ApproveReadinessAt(repoDir, projDir, "F-0001")
			if err != nil {
				t.Fatal(err)
			}
			barImp := false
			for _, im := range imps {
				barImp = barImp || im.Code == ImpedimentBarMissing
			}
			_, err = TransitionFeature(repoDir, projDir, "F-0001", domain.ActionApprove, "", "", nil)
			if v == "off" {
				if err != nil || barImp {
					t.Fatalf("light mode: approve err = %v, bar impediment = %v; want approved, none", err, barImp)
				}
				if got := reload(t, repoDir, "F-0001").State; got != domain.StateApproved {
					t.Fatalf("state = %s, want approved", got)
				}
				return
			}
			if !errors.Is(err, ErrSpecMissingBar) || !barImp {
				t.Fatalf("gauntlet %q: approve err = %v, bar impediment = %v; want ErrSpecMissingBar, listed", v, err, barImp)
			}
		})
	}
}

// TestComplete_LightMode (F-0364 crit. 1, 2): with gauntlet = "off" a result
// that cites every criterion completes with no Gauntlet section; the evidence
// per criterion is still charged. Any other value keeps the Gauntlet record.
func TestComplete_LightMode(t *testing.T) {
	for _, v := range []string{"off", "", "on", "of", "OFF", "false"} {
		t.Run("gauntlet="+v, func(t *testing.T) {
			repoDir, projDir, _ := newFeatureBackend(t)
			saveGauntlet(t, projDir, v)
			seedCompletable(t, repoDir, specNoBar, resultBody(gateCite1234, ""))
			_, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionComplete, "", "", nil)
			if v == "off" {
				if err != nil {
					t.Fatalf("light mode complete: %v", err)
				}
				if got := reload(t, repoDir, "F-0001").State; got != domain.StateImplemented {
					t.Fatalf("state = %s, want implemented", got)
				}
				return
			}
			if !errors.Is(err, ErrResultIncomplete) {
				t.Fatalf("gauntlet %q: err = %v, want ErrResultIncomplete", v, err)
			}
		})
	}
	t.Run("evidence still charged", func(t *testing.T) {
		repoDir, projDir, _ := newFeatureBackend(t)
		saveGauntlet(t, projDir, "off")
		seedCompletable(t, repoDir, specNoBar, resultBody("Provas: critério 1.\n", ""))
		if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionComplete, "", "", nil); !errors.Is(err, ErrResultIncomplete) {
			t.Fatalf("err = %v, want ErrResultIncomplete (criteria 2-4 uncited)", err)
		}
	})
}

// TestLightMode_HumanGatesUnchanged (F-0364 crit. 5): light mode leaves the
// human gates alone — an agent actor is still refused at approve and accept.
func TestLightMode_HumanGatesUnchanged(t *testing.T) {
	t.Setenv("CERNE_ACTOR", "agent")
	g := project.GatesConfig{Gauntlet: "off"}
	for name, gate := range map[string]string{"approve": g.ApproveGate(), "accept": g.AcceptGate()} {
		if err := CheckHumanGate(gate); !errors.Is(err, ErrHumanGate) {
			t.Errorf("%s: err = %v, want ErrHumanGate", name, err)
		}
	}
}

// TestSpecScaffold_LightMode (F-0364, Ajuste 2): a feature created in light
// mode is scaffolded without the template's bar guidance; with the Gauntlet
// on the scaffold keeps it.
func TestSpecScaffold_LightMode(t *testing.T) {
	for v, wantGuide := range map[string]bool{"off": false, "": true} {
		repoDir, projDir, _ := newFeatureBackend(t)
		saveGauntlet(t, projDir, v)
		f, err := CreateFeature(repoDir, projDir, "Leve", "", "", nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		spec, err := os.ReadFile(FeatureDocPath(repoDir, f.ID, "spec.md"))
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Contains(string(spec), "(Gauntlet)"); got != wantGuide {
			t.Fatalf("gauntlet %q: scaffold carries the bar guide = %v, want %v", v, got, wantGuide)
		}
	}
}
