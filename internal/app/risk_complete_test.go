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
// Past calibration (F-0518): the gate decides on its own.
func riskDelivery(t *testing.T, repoDir, projDir string, f store.Feature, outcome string, change map[string]string) {
	t.Helper()
	riskDeliveryUnder(t, project.GatesConfig{Accept: project.GateRisk, RiskCalibrationN: new(int64)}, repoDir, projDir, f, outcome, change)
}

// riskDeliveryUnder is riskDelivery with the [gates] block g, committed in
// the checkout's base.
func riskDeliveryUnder(t *testing.T, g project.GatesConfig, repoDir, projDir string, f store.Feature, outcome string, change map[string]string) {
	t.Helper()
	saveGates(t, projDir, g)
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
					calibrating(t, projDir, 0) // past calibration (F-0518)
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
	// Each task is measured on its own: T-0002 is of low risk, T-0003 carries auth.
	for _, task := range []store.Feature{
		{ID: "T-0002", Parent: "F-0001", Facets: []string{"code"}},
		{ID: "T-0003", Parent: "F-0001", Facets: []string{"auth"}},
	} {
		task.Title, task.State = "Task "+task.ID, domain.StateImplemented
		if err := store.SaveFeature(repoDir, task); err != nil {
			t.Fatal(err)
		}
		for doc, body := range map[string]string{"spec.md": riskSpec, "result.md": riskResult("vitória")} {
			riskWrite(t, repoDir, filepath.Join("content", "features", task.ID, doc), body)
		}
		riskWrite(t, projDir, filepath.Join("internal", "x", strings.ToLower(task.ID)+"_test.go"), "package x\n\n// "+task.ID+" crit. 1, 2\n")
	}
	if _, err := gitback.CommitAndPush(repoDir, "seed tasks"); err != nil {
		t.Fatal(err)
	}
	riskGit(t, projDir, "add", "-A")
	riskGit(t, projDir, "commit", "-q", "-m", "feat: the tasks (T-0002, T-0003)")

	out, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionComplete, "", "", nil)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if f := reload(t, repoDir, "F-0001"); f.State != domain.StateDone || f.AcceptedVia != "risk" {
		t.Fatalf("F-0001 = %s, accepted_via %q; want done, risk (%+v)", f.State, f.AcceptedVia, out.Risk)
	}
	carried := reload(t, repoDir, "T-0002")
	if carried.State != domain.StateDone || carried.AcceptedVia != "risk" || carried.AcceptedRisk == "" {
		t.Fatalf("T-0002 = %s, accepted_via %q, accepted_risk %q; want done, risk, its own summary", carried.State, carried.AcceptedVia, carried.AcceptedRisk)
	}
	if ev := lastEntityEvent(t, repoDir, "T-0002", "feature.accepted"); ev.Payload["via"] != "risk" || ev.Payload["cascade_from"] != "F-0001" || ev.Payload["risk"] != "low" {
		t.Errorf("T-0002 accept event = %+v, want via risk, cascade_from F-0001 and its own measurements", ev.Payload)
	}
	held := reload(t, repoDir, "T-0003")
	if held.State != domain.StateImplemented || held.AcceptedVia != "" || held.AcceptedRisk != "" {
		t.Fatalf("T-0003 = %s, accepted_via %q, accepted_risk %q; want implemented and unmarked", held.State, held.AcceptedVia, held.AcceptedRisk)
	}
	if ev := lastEntityEvent(t, repoDir, "T-0003", "feature.risk_held"); ev.Payload["risk"] != "held" || ev.Payload["risk_facets"] != "auth" || ev.Payload["cascade_from"] != "F-0001" {
		t.Errorf("T-0003 held event = %+v, want its own held measurements", ev.Payload)
	}
	lines := strings.Join(out.Cascade, "\n")
	if !strings.Contains(lines, "T-0002: implemented → done along with F-0001") || !strings.Contains(lines, "T-0003") || !strings.Contains(lines, "auth") {
		t.Errorf("cascade lines = %q, want T-0002 carried and T-0003 held with why", lines)
	}
}

// TestRiskSeedLeavesNoMark: a scripted seed (cerne demo) crosses no gate, so a
// complete it runs under the risk gate marks the card with neither
// accepted_via nor the risk summary.
// F-0516 crit. 6
func TestRiskSeedLeavesNoMark(t *testing.T) {
	asActor(t, "agent")
	repoDir, projDir, _ := newFeatureBackend(t)
	riskDelivery(t, repoDir, projDir, store.Feature{ID: "F-0001", Facets: []string{"code"}}, "vitória", cleanChange())
	if _, err := TransitionFeatureSeed(repoDir, projDir, "F-0001", domain.ActionComplete, "", "", nil, false); err != nil {
		t.Fatal(err)
	}
	if f := reload(t, repoDir, "F-0001"); f.State != domain.StateDone || f.AcceptedVia != "" || f.AcceptedRisk != "" {
		t.Errorf("seeded card = %s, accepted_via %q, accepted_risk %q; want done and unmarked", f.State, f.AcceptedVia, f.AcceptedRisk)
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

// TestRiskRecordsBaseAndHead: the measurements name the commits measured —
// the merge-base with the default branch and HEAD.
// F-0516 crit. 6
func TestRiskRecordsBaseAndHead(t *testing.T) {
	asActor(t, "agent")
	repoDir, projDir, _ := newFeatureBackend(t)
	riskDelivery(t, repoDir, projDir, store.Feature{ID: "F-0001", Facets: []string{"code"}}, "vitória", cleanChange())
	base, head := gitOut(t, projDir, "merge-base", "main", "HEAD"), gitOut(t, projDir, "rev-parse", "HEAD")
	if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionComplete, "", "", nil); err != nil {
		t.Fatal(err)
	}
	for _, typ := range []string{"feature.completed", "feature.accepted"} {
		p := lastEntityEvent(t, repoDir, "F-0001", typ).Payload
		if p["risk_base"] != base || p["risk_head"] != head {
			t.Errorf("%s: risk_base %q, risk_head %q; want %s, %s", typ, p["risk_base"], p["risk_head"], base, head)
		}
	}
}

// TestRiskRevertNeedsANewRound: a card accepted as low risk, reverted and
// completed again with no new Gauntlet round is held — the win it carries was
// judged before the revert; a round recorded after the revert lets it through.
// F-0516 crit. 4
func TestRiskRevertNeedsANewRound(t *testing.T) {
	asActor(t, "agent")
	repoDir, projDir, _ := newFeatureBackend(t)
	riskDelivery(t, repoDir, projDir, store.Feature{ID: "F-0001", Facets: []string{"code"}}, "vitória", cleanChange())
	step := func(action domain.Action, reason string) TransitionOutcome {
		t.Helper()
		out, err := TransitionFeature(repoDir, projDir, "F-0001", action, "", reason, nil)
		if err != nil {
			t.Fatalf("%s: %v", action, err)
		}
		return out
	}
	if out := step(domain.ActionComplete, ""); out.To != domain.StateDone {
		t.Fatalf("first complete = %s, want done by risk", out.To)
	}
	step(domain.ActionRevert, "found a problem")
	step(domain.ActionRevert, "back to development")
	out := step(domain.ActionComplete, "")
	if out.To != domain.StateImplemented || out.Risk == nil || out.Risk.Gauntlet.Holds || !strings.Contains(out.Risk.Gauntlet.Found, "revert") {
		t.Fatalf("complete after the revert = %s, gauntlet %+v; want held: the win predates the revert", out.To, out.Risk)
	}
	if got := lastEntityEvent(t, repoDir, "F-0001", "feature.completed").Payload["risk_round"]; got != "1" {
		t.Errorf("risk_round = %q, want 1", got)
	}

	step(domain.ActionRevert, "a new round")
	riskWrite(t, repoDir, filepath.Join("content", "features", "F-0001", "result.md"), riskResult("vitória")+"Rodada 2 — vitória: \"de novo\"\n")
	if _, err := gitback.CommitAndPush(repoDir, "round 2"); err != nil {
		t.Fatal(err)
	}
	if out := step(domain.ActionComplete, ""); out.To != domain.StateDone {
		t.Fatalf("complete with a round after the revert = %s (%+v), want done by risk", out.To, out.Risk)
	}
}
