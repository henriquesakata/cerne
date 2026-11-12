package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
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

// lastEventPayload returns the payload of the backend's last event.
func lastEventPayload(t *testing.T, repoDir string) map[string]string {
	t.Helper()
	evs, err := store.ReadEvents(repoDir)
	if err != nil || len(evs) == 0 {
		t.Fatalf("ReadEvents: %v (%d events)", err, len(evs))
	}
	return evs[len(evs)-1].Payload
}

// TestLightMode_StampsEvents (F-0364 critic 5): an approve or a complete
// crossed in light mode records gauntlet = off on its event, so the event log
// shows which deliveries skipped the Gauntlet; with it on, nothing is added.
func TestLightMode_StampsEvents(t *testing.T) {
	for v, want := range map[string]string{"off": "off", "": ""} {
		repoDir, projDir, _ := newFeatureBackend(t)
		saveGauntlet(t, projDir, v)
		spec := specNoBar
		if v != "off" {
			spec += testBarTail
		}
		seedCandidateSpec(t, repoDir, "F-0001", spec)
		if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionApprove, "", "", nil); err != nil {
			t.Fatalf("approve: %v", err)
		}
		if got := lastEventPayload(t, repoDir)["gauntlet"]; got != want {
			t.Fatalf("gauntlet %q: approve event gauntlet = %q, want %q", v, got, want)
		}

		repoDir, projDir, _ = newFeatureBackend(t)
		saveGauntlet(t, projDir, v)
		result := resultBody(gateCite1234, "")
		if v != "off" {
			result = resultBody(gateCite1234, gateGauntletOK)
		}
		seedCompletable(t, repoDir, specNoBar, result)
		if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionComplete, "", "", nil); err != nil {
			t.Fatalf("complete: %v", err)
		}
		if got := lastEventPayload(t, repoDir)["gauntlet"]; got != want {
			t.Fatalf("gauntlet %q: complete event gauntlet = %q, want %q", v, got, want)
		}
	}
}

// TestProjectGates_FailSafe (F-0364 critic 7): a .cernerc that fails to parse,
// or a TOML bool where the string goes, keeps the Gauntlet on.
func TestProjectGates_FailSafe(t *testing.T) {
	for name, body := range map[string]string{
		"unparsable": "project_id = \"demo\"\nbackend = \n[gates\n",
		"toml bool":  "project_id = \"demo\"\nbackend = \"b\"\n\n[gates]\ngauntlet = false\n",
	} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, ".cernerc"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if !projectGates(dir).GauntletEnabled() {
			t.Errorf("%s: Gauntlet off, want on", name)
		}
	}
}
