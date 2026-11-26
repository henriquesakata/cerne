package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
)

// riskSpec has two numbered criteria; riskResult cites both and ends its
// Gauntlet with outcome (vitória, empate, derrota).
const riskSpec = "# Doc\n\n## Objetivo e critérios de aceitação\n\n1. Um.\n2. Dois.\n" + testBarTail

func riskResult(outcome string) string {
	return "# Doc\n\n## Resultado\n\ncritério 1 → TestUm; critério 2 → TestDois.\n" +
		"\n## Gauntlet\n\nBarra: fixture de teste — referência nomeada\nRodada 1 — " + outcome + ": \"fixture\"\n"
}

// riskDelivery makes projDir a project under accept = "risk" whose delivery
// branch commits change, and seeds f in development in the backend with
// riskSpec, a plan and riskResult(outcome).
func riskDelivery(t *testing.T, repoDir, projDir string, f store.Feature, outcome string, change map[string]string) {
	t.Helper()
	saveGates(t, projDir, project.GatesConfig{Accept: project.GateRisk})
	riskRepo(t, projDir, nil, change)
	f.Title, f.State = "Feature "+f.ID, domain.StateInDevelopment
	if err := store.SaveFeature(repoDir, f); err != nil {
		t.Fatal(err)
	}
	for doc, body := range map[string]string{"spec.md": riskSpec, "plan.md": "# Doc\n\n## Plano\n\nCorpo real do plano.\n", "result.md": riskResult(outcome)} {
		riskWrite(t, repoDir, filepath.Join("content", "features", f.ID, doc), body)
	}
	if _, err := gitback.CommitAndPush(repoDir, "seed "+f.ID); err != nil {
		t.Fatal(err)
	}
}

// cleanChange is a delivery that touches nothing sensitive and marks both
// criteria of riskSpec.
func cleanChange() map[string]string {
	return map[string]string{"internal/x/x.go": "package x\n", "internal/x/x_test.go": "package x\n\n// F-0001 crit. 1, 2\n"}
}

// TestRiskGateNeedsAllThree: the eight combinations of the three conditions;
// only all three holding is low risk, and each one that fails is one line.
// F-0516 crit. 2
func TestRiskGateNeedsAllThree(t *testing.T) {
	for _, area := range []bool{true, false} {
		for _, win := range []bool{true, false} {
			for _, tested := range []bool{true, false} {
				t.Run(fmt.Sprintf("area=%v,win=%v,tested=%v", area, win, tested), func(t *testing.T) {
					repoDir, projDir := t.TempDir(), t.TempDir()
					change := cleanChange()
					if !area {
						change["go.mod"] = "module x\n"
					}
					if !tested {
						change["internal/x/x_test.go"] = "package x\n\n// F-0001 crit. 1\n"
					}
					riskRepo(t, projDir, nil, change)
					outcome := "vitória"
					if !win {
						outcome = "empate"
					}
					riskWrite(t, repoDir, filepath.Join("content", "features", "F-0001", "spec.md"), riskSpec)
					riskWrite(t, repoDir, filepath.Join("content", "features", "F-0001", "result.md"), riskResult(outcome))

					ra := assessRisk(repoDir, projDir, store.Feature{ID: "F-0001", Facets: []string{"code"}})
					if ra.Area.Holds != area || ra.Gauntlet.Holds != win || ra.Tests.Holds != tested {
						t.Fatalf("conditions = %v/%v/%v, want %v/%v/%v: %+v", ra.Area.Holds, ra.Gauntlet.Holds, ra.Tests.Holds, area, win, tested, ra)
					}
					if ra.Low() != (area && win && tested) {
						t.Errorf("Low() = %v, want %v", ra.Low(), area && win && tested)
					}
					failed := 0
					for _, ok := range []bool{area, win, tested} {
						if !ok {
							failed++
						}
					}
					if len(ra.Held()) != failed {
						t.Errorf("Held() = %q, want one line per failed condition (%d)", ra.Held(), failed)
					}
					if ra.Low() && ra.Summary() == "" {
						t.Error("a low-risk delivery has no summary")
					}
				})
			}
		}
	}
}

// TestRiskAcceptCascadesTasks: a parent accepted as low risk carries its
// implemented tasks, marked risk, with cascade_from on their accept event.
// F-0516 crit. 9
func TestRiskAcceptCascadesTasks(t *testing.T) {
	asActor(t, "agent")
	repoDir, projDir, _ := newFeatureBackend(t)
	riskDelivery(t, repoDir, projDir, store.Feature{ID: "F-0001", Facets: []string{"code"}}, "vitória", cleanChange())
	seedWithSpec(t, repoDir, store.Feature{ID: "T-0002", Parent: "F-0001", State: domain.StateImplemented}, taskBody+exemptTaskBar)

	out, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionComplete, "", "", nil)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if f := reload(t, repoDir, "F-0001"); f.State != domain.StateDone || f.AcceptedVia != "risk" {
		t.Fatalf("F-0001 = %s, accepted_via %q; want done, risk (%+v)", f.State, f.AcceptedVia, out.Risk)
	}
	if task := reload(t, repoDir, "T-0002"); task.State != domain.StateDone || task.AcceptedVia != "risk" {
		t.Fatalf("T-0002 = %s, accepted_via %q; want done, risk", task.State, task.AcceptedVia)
	}
	if ev := lastEntityEvent(t, repoDir, "T-0002", "feature.accepted"); ev.Payload["via"] != "risk" || ev.Payload["cascade_from"] != "F-0001" {
		t.Errorf("T-0002 accept event = %+v, want via risk and cascade_from F-0001", ev.Payload)
	}
	if lines := strings.Join(out.Cascade, "\n"); !strings.Contains(lines, "T-0002: implemented → done along with F-0001") {
		t.Errorf("cascade lines = %q, want T-0002 carried", lines)
	}
}

// TestRiskHeldKeepsTasks: a held parent stays implemented and so do its
// tasks: nothing was accepted.
// F-0516 crit. 7, 9
func TestRiskHeldKeepsTasks(t *testing.T) {
	asActor(t, "agent")
	repoDir, projDir, _ := newFeatureBackend(t)
	riskDelivery(t, repoDir, projDir, store.Feature{ID: "F-0001", Facets: []string{"auth"}}, "vitória", cleanChange())
	seedWithSpec(t, repoDir, store.Feature{ID: "T-0002", Parent: "F-0001", State: domain.StateImplemented}, taskBody+exemptTaskBar)

	out, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionComplete, "", "", nil)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if out.Risk == nil || out.Risk.Low() || out.To != domain.StateImplemented {
		t.Fatalf("outcome = %+v, want held at implemented", out)
	}
	for _, id := range []string{"F-0001", "T-0002"} {
		if f := reload(t, repoDir, id); f.State != domain.StateImplemented || f.AcceptedVia != "" {
			t.Errorf("%s = %s, accepted_via %q; want implemented, unmarked", id, f.State, f.AcceptedVia)
		}
	}
	if ev := lastEntityEvent(t, repoDir, "F-0001", "feature.completed"); ev.Payload["risk"] != "held" || ev.Payload["risk_facets"] != "auth" || !strings.Contains(ev.Payload["risk_area"], "auth") {
		t.Errorf("complete event = %+v, want the held measurements", ev.Payload)
	}
}

// TestNextStepRiskGate: the next step of a card in development under the risk
// gate says the acceptance depends on the three conditions, and reading it
// runs no git: a git on PATH that records its calls is never called.
// F-0516 crit. 11
func TestNextStepRiskGate(t *testing.T) {
	proj, repo := nextStepRepo(t)
	riskWrite(t, proj, ".cernerc", "project_id = \"demo\"\nbackend = \"b\"\n\n[gates]\naccept = \"risk\"\n")
	if err := store.SaveFeature(repo, store.Feature{ID: "F-0001", Title: "t", State: domain.StateInDevelopment}); err != nil {
		t.Fatal(err)
	}
	nextStepDoc(t, repo, "F-0001", "spec.md", nextStepGoodSpec)
	nextStepDoc(t, repo, "F-0001", "plan.md", "# plano\n\nPassos reais do plano.\n")
	nextStepDoc(t, repo, "F-0001", "result.md", nextStepGoodRes)
	bin, called := t.TempDir(), filepath.Join(t.TempDir(), "called")
	riskWrite(t, bin, "git", "#!/bin/sh\necho \"$@\" >> "+called+"\nexit 1\n")
	if err := os.Chmod(filepath.Join(bin, "git"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	got, err := NextStep(repo, proj, "F-0001")
	if err != nil {
		t.Fatal(err)
	}
	if got.Command != "cerne feature complete F-0001" {
		t.Errorf("command = %q, want the complete", got.Command)
	}
	for _, want := range []string{"risk gate", "sensitive path or facet", "Gauntlet", "win", "every criterion has a test", "human merge"} {
		if !strings.Contains(got.Step, want) {
			t.Errorf("step %q lacks %q", got.Step, want)
		}
	}
	if raw, err := os.ReadFile(called); err == nil {
		t.Errorf("NextStep ran git: %s", raw)
	}
}
