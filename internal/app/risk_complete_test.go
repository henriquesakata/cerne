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

// seedRiskTasks seeds implemented tasks under F-0001, each with riskSpec, a
// won result and a test marking both criteria, committed in projDir by a
// commit that cites every task.
func seedRiskTasks(t *testing.T, repoDir, projDir string, tasks ...store.Feature) {
	t.Helper()
	var ids []string
	for _, task := range tasks {
		task.Parent, task.Title, task.State = "F-0001", "Task "+task.ID, domain.StateImplemented
		if err := store.SaveFeature(repoDir, task); err != nil {
			t.Fatal(err)
		}
		for doc, body := range map[string]string{"spec.md": riskSpec, "result.md": riskResult("vitória")} {
			riskWrite(t, repoDir, filepath.Join("content", "features", task.ID, doc), body)
		}
		riskWrite(t, projDir, filepath.Join("internal", "x", strings.ToLower(task.ID)+"_test.go"), "package x\n\n// "+task.ID+" crit. 1, 2\n")
		ids = append(ids, task.ID)
	}
	if _, err := gitback.CommitAndPush(repoDir, "seed tasks"); err != nil {
		t.Fatal(err)
	}
	riskGit(t, projDir, "add", "-A")
	riskGit(t, projDir, "commit", "-q", "-m", "feat: the tasks ("+strings.Join(ids, ", ")+")")
}

// TestRiskAcceptCascadesTasks: a parent whose tasks are all of low risk is
// accepted as low risk and carries them, each with its own measurements.
// F-0516 crit. 9
func TestRiskAcceptCascadesTasks(t *testing.T) {
	asActor(t, "agent")
	repoDir, projDir, _ := newFeatureBackend(t)
	riskDelivery(t, repoDir, projDir, store.Feature{ID: "F-0001", Facets: []string{"code"}}, "vitória", cleanChange())
	seedRiskTasks(t, repoDir, projDir, store.Feature{ID: "T-0002", Facets: []string{"code"}}, store.Feature{ID: "T-0003", Facets: []string{"code"}})

	out, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionComplete, "", "", nil)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if f := reload(t, repoDir, "F-0001"); f.State != domain.StateDone || f.AcceptedVia != "risk" {
		t.Fatalf("F-0001 = %s, accepted_via %q; want done, risk (%+v)", f.State, f.AcceptedVia, out.Risk)
	}
	for _, id := range []string{"T-0002", "T-0003"} {
		task := reload(t, repoDir, id)
		if task.State != domain.StateDone || task.AcceptedVia != "risk" || task.AcceptedRisk == "" {
			t.Errorf("%s = %s, accepted_via %q, accepted_risk %q; want done, risk, its own summary", id, task.State, task.AcceptedVia, task.AcceptedRisk)
		}
		if ev := lastEntityEvent(t, repoDir, id, "feature.accepted"); ev.Payload["via"] != "risk" || ev.Payload["cascade_from"] != "F-0001" || ev.Payload["risk"] != "low" {
			t.Errorf("%s accept event = %+v, want via risk, cascade_from F-0001 and its own measurements", id, ev.Payload)
		}
		if lines := strings.Join(out.Cascade, "\n"); !strings.Contains(lines, id+": implemented → done along with F-0001") {
			t.Errorf("cascade lines = %q, want %s carried", lines, id)
		}
	}
}

// TestRiskHeldTaskHoldsItsParent: one held task holds the whole card back —
// its work rides the parent's PR. The parent's held lines name the task and
// why; nothing moves, the task records its own measurements, and the human
// merge then carries parent and tasks through accept --merged.
// F-0516 crit. 7, 9
func TestRiskHeldTaskHoldsItsParent(t *testing.T) {
	asActor(t, "agent")
	repoDir, projDir, _ := newFeatureBackend(t)
	riskDelivery(t, repoDir, projDir, store.Feature{ID: "F-0001", Facets: []string{"code"}, DeliveryMRURL: prURL("7")}, "vitória", cleanChange())
	seedRiskTasks(t, repoDir, projDir, store.Feature{ID: "T-0002", Facets: []string{"code"}}, store.Feature{ID: "T-0003", Facets: []string{"auth"}})

	out, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionComplete, "", "", nil)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if out.To != domain.StateImplemented || out.Risk == nil || out.Risk.Low() {
		t.Fatalf("outcome = %+v, want F-0001 held by its task", out)
	}
	if held := strings.Join(out.Risk.Held(), "\n"); !strings.Contains(held, "T-0003") || !strings.Contains(held, "auth") || strings.Contains(held, "T-0002") {
		t.Errorf("held lines = %q, want T-0003 and why, and not T-0002", held)
	}
	for _, id := range []string{"F-0001", "T-0002", "T-0003"} {
		if f := reload(t, repoDir, id); f.State != domain.StateImplemented || f.AcceptedVia != "" {
			t.Errorf("%s = %s, accepted_via %q; want implemented, unmarked", id, f.State, f.AcceptedVia)
		}
	}
	if p := lastEntityEvent(t, repoDir, "F-0001", "feature.completed").Payload; p["risk"] != "held" || !strings.Contains(p["risk_tasks"], "T-0003") {
		t.Errorf("complete event = %+v, want held with T-0003 in risk_tasks", p)
	}
	if p := lastEntityEvent(t, repoDir, "T-0003", "feature.risk_held").Payload; p["risk_facets"] != "auth" || p["cascade_from"] != "F-0001" {
		t.Errorf("T-0003 held event = %+v, want its own measurements", p)
	}

	useFakeForge(t, &fakeForge{pr: mergedPR})
	cfg := mergeCfg()
	cfg.Gates.Accept = project.GateRisk
	if _, err := AcceptMerged(repoDir, projDir, cfg, []string{"F-0001"}); err != nil {
		t.Fatalf("accept --merged: %v", err)
	}
	for _, id := range []string{"F-0001", "T-0002", "T-0003"} {
		if f := reload(t, repoDir, id); f.State != domain.StateDone || f.AcceptedVia != "merge" {
			t.Errorf("after the merge %s = %s, accepted_via %q; want done on merge", id, f.State, f.AcceptedVia)
		}
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

// TestRiskRevertAfterAnyAcceptNeedsANewRound: the revert guard reads the last
// accept whatever its via — a person's, the merge's or the auto gate's: after
// a revert, the old win does not pass the risk gate again.
// F-0516 crit. 4
func TestRiskRevertAfterAnyAcceptNeedsANewRound(t *testing.T) {
	for _, via := range []string{"person", "merge", "auto"} {
		t.Run(via, func(t *testing.T) {
			asActor(t, "agent")
			repoDir, projDir, _ := newFeatureBackend(t)
			riskDelivery(t, repoDir, projDir, store.Feature{ID: "F-0001", Facets: []string{"code"}, DeliveryMRURL: prURL("7")}, "vitória", cleanChange())
			// The gate in .cernerc is tracked: a change is committed, or the
			// risk gate would hold the tree as uncommitted.
			gate := func(g string) {
				saveGates(t, projDir, project.GatesConfig{Accept: g})
				riskGit(t, projDir, "commit", "-q", "-am", "chore: accept gate "+g+" (F-0001)")
			}
			step := func(action domain.Action, reason string) TransitionOutcome {
				t.Helper()
				out, err := TransitionFeature(repoDir, projDir, "F-0001", action, "", reason, nil)
				if err != nil {
					t.Fatalf("%s: %v", action, err)
				}
				return out
			}
			switch via {
			case "person":
				gate(project.GateHuman)
				step(domain.ActionComplete, "")
				asActor(t, "")
				step(domain.ActionAccept, "")
				asActor(t, "agent")
			case "merge":
				gate(project.GateMerge)
				step(domain.ActionComplete, "")
				useFakeForge(t, &fakeForge{pr: mergedPR})
				if _, err := AcceptMerged(repoDir, projDir, mergeCfg(), []string{"F-0001"}); err != nil {
					t.Fatalf("accept --merged: %v", err)
				}
			case "auto":
				gate(project.GateAuto)
				step(domain.ActionComplete, "")
			}
			if f := reload(t, repoDir, "F-0001"); f.State != domain.StateDone {
				t.Fatalf("accepted by %s = %s, want done", via, f.State)
			}
			gate(project.GateRisk)
			step(domain.ActionRevert, "found a problem")
			step(domain.ActionRevert, "back to development")
			out := step(domain.ActionComplete, "")
			if out.To != domain.StateImplemented || out.Risk == nil || out.Risk.Gauntlet.Holds || !strings.Contains(out.Risk.Gauntlet.Found, "revert") {
				t.Fatalf("complete after a revert of the %s accept = %s, gauntlet %+v; want held", via, out.To, out.Risk)
			}
		})
	}
}
