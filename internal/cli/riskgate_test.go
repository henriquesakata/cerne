package cli

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
)

// riskCLISpec has two numbered criteria (the second one, with manual, closes
// as verified by hand); riskCLIResult cites both and ends its Gauntlet with
// outcome.
func riskCLISpec(manual bool) string {
	second := "2. Dois."
	if manual {
		second = "2. Dois (verificação manual)."
	}
	return "# Doc\n\n## Problema\n\nX.\n\n## Objetivo e critérios de aceitação\n\n1. Um.\n" + second + "\n\n## Verificação\n\nBarra: fixture — referência nomeada\nPergunta ao juiz: qual é melhor?\n"
}

func riskCLIResult(outcome string) string {
	return "# Doc\n\n## Resultado\n\ncritério 1 → TestUm; critério 2 → TestDois.\n" +
		"\n## Gauntlet\n\nBarra: fixture — referência nomeada\nRodada 1 — " + outcome + ": \"fixture\"\n"
}

// setAcceptGate stores [gates] accept in the project's .cernerc.
func setAcceptGate(t *testing.T, projDir, gate string) {
	t.Helper()
	cfg, _, err := project.LoadConfig(projDir)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Gates.Accept = gate
	if err := project.SaveConfig(projDir, cfg); err != nil {
		t.Fatal(err)
	}
}

// endCalibration stores gates.risk_calibration = 0 (F-0518): the risk gate
// decides on its own from the first delivery.
func endCalibration(t *testing.T, projDir string) {
	t.Helper()
	cfg, _, err := project.LoadConfig(projDir)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Gates.RiskCalibrationN = new(int64)
	if err := project.SaveConfig(projDir, cfg); err != nil {
		t.Fatal(err)
	}
}

// riskProject is a project under [gates] accept = "risk" whose checkout is a
// git repository: main holds what init wrote and the delivery branch commits
// change. F-0001 is in development with facets, riskCLISpec(manual) and
// riskCLIResult(outcome).
func riskProject(t *testing.T, facets []string, outcome string, manual bool, change map[string]string) (projDir, repoDir string) {
	t.Helper()
	projDir, _, repoDir = setupProject(t)
	setAcceptGate(t, projDir, project.GateRisk)
	endCalibration(t, projDir)
	gitRun(t, projDir, "init", "-q", "-b", "main")
	gitRun(t, projDir, "add", "-A")
	gitRun(t, projDir, "commit", "-q", "-m", "base")
	gitRun(t, projDir, "checkout", "-q", "-b", "feat/x")
	for rel, body := range change {
		p := filepath.Join(projDir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitRun(t, projDir, "add", "-A")
	gitRun(t, projDir, "commit", "-q", "-m", "feat: the delivery (F-0001)")
	if err := store.SaveFeature(repoDir, store.Feature{ID: "F-0001", Title: "Card F-0001", State: domain.StateInDevelopment, Facets: facets}); err != nil {
		t.Fatal(err)
	}
	for doc, body := range map[string]string{"spec.md": riskCLISpec(manual), "plan.md": "# Doc\n\n## Plano\n\nCorpo real do plano.\n", "result.md": riskCLIResult(outcome)} {
		p := filepath.Join(repoDir, "content", "features", "F-0001", doc)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	commitBackend(t, repoDir)
	return projDir, repoDir
}

// lastEventOfType is entity id's last event of type typ in the backend.
func lastEventOfType(t *testing.T, repoDir, id, typ string) domain.Event {
	t.Helper()
	evs, err := store.ReadEvents(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i].EntityID == id && evs[i].Type == typ {
			return evs[i]
		}
	}
	t.Fatalf("%s has no %s event", id, typ)
	return domain.Event{}
}

// TestCompleteRiskAccepts: an agent's complete of a low-risk delivery takes
// the card to done, marked risk with the summary, records the measurements on
// the accept event and prints the acceptance with them; no human accept is
// asked for.
// F-0516 crit. 6
func TestCompleteRiskAccepts(t *testing.T) {
	projDir, repoDir := riskProject(t, []string{"code"}, "vitória", false, map[string]string{
		"internal/x/x.go": "package x\n", "internal/x/x_test.go": "package x\n\n// F-0001 crit. 1, 2\n"})
	t.Setenv("CERNE_ACTOR", "agent")
	out, err := execCerne(t, "feature", "complete", "F-0001", "--no-stamp", "--dir", projDir)
	if err != nil {
		t.Fatalf("complete: %v\n%s", err, out)
	}
	const summary = "touches no sensitive path or facet; the Gauntlet's last round is a win; 2/2 criteria with a test"
	if !strings.Contains(out, "F-0001: implemented → done (accepted as low risk: "+summary+")\n") {
		t.Errorf("output lacks the acceptance line:\n%s", out)
	}
	if strings.Contains(out, "human accept:") {
		t.Errorf("a card already done is told to wait for a human accept:\n%s", out)
	}
	f, err := store.LoadFeature(repoDir, "F-0001")
	if err != nil || f.State != domain.StateDone || f.AcceptedVia != "risk" || f.AcceptedRisk != summary {
		t.Fatalf("card = %s, accepted_via %q, accepted_risk %q (%v); want done, risk, the summary", f.State, f.AcceptedVia, f.AcceptedRisk, err)
	}
	ev := lastEventOfType(t, repoDir, "F-0001", "feature.accepted")
	for k, want := range map[string]string{"via": "risk", "risk": "low", "risk_paths": "", "risk_facets": "", "risk_verdict": "win", "risk_criteria_tested": "2/2"} {
		if got, ok := ev.Payload[k]; !ok || got != want {
			t.Errorf("accept event %s = %q (present %v), want %q", k, got, ok, want)
		}
	}
	if ev := lastEventOfType(t, repoDir, "F-0001", "feature.completed"); ev.Payload["risk"] != "low" {
		t.Errorf("complete event = %+v, want the measurements", ev.Payload)
	}
}

// TestCompleteRiskHoldsForMerge: a delivery that fails the conditions stays
// implemented; complete prints one line per failed condition with what it
// found, and the complete event records the measurements.
// F-0516 crit. 7
func TestCompleteRiskHoldsForMerge(t *testing.T) {
	projDir, repoDir := riskProject(t, []string{"code", "auth"}, "empate", true, map[string]string{
		"go.mod": "module x\n", "internal/x/x_test.go": "package x\n\n// F-0001 crit. 1\n"})
	t.Setenv("CERNE_ACTOR", "agent")
	out, err := execCerne(t, "feature", "complete", "F-0001", "--no-stamp", "--dir", projDir)
	if err != nil {
		t.Fatalf("complete: %v\n%s", err, out)
	}
	want := "F-0001: in_development → implemented\n" +
		"F-0001 waits for the human merge of its PR ([gates] accept = \"risk\"); what held it:\n" +
		"  - touches a sensitive path: go.mod; carries a sensitive facet: auth\n" +
		"  - the Gauntlet's last round is a tie, not a win\n" +
		"  - criterion 2 is verified by hand, not by a test\n"
	if !strings.Contains(out, want) {
		t.Errorf("output lacks the held lines %q:\n%s", want, out)
	}
	if f, _ := store.LoadFeature(repoDir, "F-0001"); f.State != domain.StateImplemented || f.AcceptedVia != "" || f.AcceptedRisk != "" {
		t.Fatalf("card = %+v, want implemented and unmarked", f)
	}
	ev := lastEventOfType(t, repoDir, "F-0001", "feature.completed")
	for k, want := range map[string]string{"risk": "held", "risk_paths": "go.mod", "risk_facets": "auth", "risk_verdict": "tie", "risk_criteria_tested": "1/2"} {
		if ev.Payload[k] != want {
			t.Errorf("complete event %s = %q, want %q", k, ev.Payload[k], want)
		}
	}
}

// TestRiskHeldCardFollowsMergeGate: a card the risk gate held behaves as under
// the merge gate: next lists it as waiting for the merge and names the record,
// accept --merged and wait --for done record the human's merge, and the shell
// hook denies an agent's merge.
// F-0516 crit. 7
func TestRiskHeldCardFollowsMergeGate(t *testing.T) {
	held := store.Feature{ID: "F-0001", State: domain.StateImplemented, DeliveryMRURL: "https://github.com/o/r/pull/12"}
	t.Run("next", func(t *testing.T) {
		projDir, _ := mergeGateProject(t, &cliFakeForge{pr: cliMergedPR}, held)
		setAcceptGate(t, projDir, project.GateRisk)
		out, err := execCerne(t, "next", "--dir", projDir)
		if blk := nextBlock(out, "WAITING FOR THE MERGE (1)"); err != nil || !strings.Contains(blk, "F-0001") || strings.Contains(out, "WAITING FOR HUMAN ACCEPT") {
			t.Errorf("next: %v, want F-0001 under WAITING FOR THE MERGE:\n%s", err, out)
		}
		if out, err := execCerne(t, "next", "F-0001", "--dir", projDir); err != nil || !strings.Contains(out, "cerne feature accept F-0001 --merged") {
			t.Errorf("next F-0001: %v, want the --merged record:\n%s", err, out)
		}
	})
	t.Run("accept --merged", func(t *testing.T) {
		projDir, repoDir := mergeGateProject(t, &cliFakeForge{pr: cliMergedPR}, held)
		setAcceptGate(t, projDir, project.GateRisk)
		t.Setenv("CERNE_ACTOR", "agent")
		if out, err := execCerne(t, "feature", "accept", "--merged", "F-0001", "--dir", projDir); err != nil {
			t.Fatalf("accept --merged: %v\n%s", err, out)
		}
		if f, _ := store.LoadFeature(repoDir, "F-0001"); f.State != domain.StateDone || f.AcceptedVia != "merge" {
			t.Errorf("card = %+v, want done on merge", f)
		}
	})
	t.Run("wait --for done", func(t *testing.T) {
		projDir, repoDir := mergeGateProject(t, &cliFakeForge{pr: cliMergedPR}, held)
		setAcceptGate(t, projDir, project.GateRisk)
		t.Setenv("CERNE_ACTOR", "agent")
		out, err := execCerne(t, "feature", "wait", "F-0001", "--for", "done", "--timeout", "0", "--dir", projDir)
		if err != nil || !strings.Contains(out, "accepted on merge of https://github.com/o/r/pull/12") {
			t.Fatalf("wait: %v\n%s", err, out)
		}
		if f, _ := store.LoadFeature(repoDir, "F-0001"); f.State != domain.StateDone || f.AcceptedVia != "merge" {
			t.Errorf("card = %+v, want done on merge", f)
		}
	})
	t.Run("hook", func(t *testing.T) {
		projDir, _, _ := setupProject(t)
		setAcceptGate(t, projDir, project.GateRisk)
		out, err := execCerneStdin(t, bashJSON(t, "s-claude", projDir, "gh pr merge 12 --squash"), "hook", "tool-notes", "--format", "claude")
		var d gateDeny
		if err != nil || json.Unmarshal([]byte(out), &d) != nil || d.HookSpecificOutput.PermissionDecision != "deny" || !strings.Contains(d.HookSpecificOutput.PermissionDecisionReason, "risk") {
			t.Errorf("hook: err %v out %q, want a deny that names the risk gate", err, out)
		}
	})
}

// TestAcceptManualUnderRisk: under the risk gate a person's typed accept still
// takes a held card to done, unmarked: the way out for a delivery with no PR.
// An agent's is refused as a human act.
// F-0516 crit. 8
func TestAcceptManualUnderRisk(t *testing.T) {
	projDir, repoDir := mergeGateProject(t, &cliFakeForge{}, store.Feature{ID: "F-0001", State: domain.StateImplemented})
	setAcceptGate(t, projDir, project.GateRisk)
	t.Setenv("CERNE_ACTOR", "agent")
	if out, err := execCerne(t, "feature", "accept", "F-0001", "--dir", projDir); !errors.Is(err, app.ErrHumanGate) {
		t.Fatalf("agent accept: err = %v (%s), want ErrHumanGate", err, out)
	}
	for _, k := range []string{"CERNE_ACTOR", "CLAUDECODE", "CURSOR_AGENT", "COPILOT_CLI", "CODEX_THREAD_ID"} {
		t.Setenv(k, "")
	}
	if out, err := execCerne(t, "feature", "accept", "F-0001", "--dir", projDir); err != nil || !strings.Contains(out, "F-0001: implemented → done") {
		t.Fatalf("person accept: %v\n%s", err, out)
	}
	if f, _ := store.LoadFeature(repoDir, "F-0001"); f.State != domain.StateDone || f.AcceptedVia != "" {
		t.Errorf("card = %s, accepted_via %q; want done, unmarked", f.State, f.AcceptedVia)
	}
}

// TestShowAcceptedAsLowRisk: show names a risk acceptance with its
// measurements; a card accepted otherwise does not claim one.
// F-0516 crit. 10
func TestShowAcceptedAsLowRisk(t *testing.T) {
	const summary = "touches no sensitive path or facet; the Gauntlet's last round is a win; 2/2 criteria with a test"
	projDir, _ := mergeGateProject(t, &cliFakeForge{},
		store.Feature{ID: "F-0001", State: domain.StateDone, AcceptedVia: "risk", AcceptedRisk: summary},
		store.Feature{ID: "F-0002", State: domain.StateDone})
	out, err := execCerne(t, "show", "F-0001", "--dir", projDir)
	if err != nil || !strings.Contains(out, "\naccepted: as low risk ("+summary+")\n") {
		t.Fatalf("show F-0001: %v\n%s", err, out)
	}
	if out, _ := execCerne(t, "show", "F-0002", "--dir", projDir); strings.Contains(out, "low risk") {
		t.Errorf("show F-0002 must not claim a risk acceptance:\n%s", out)
	}
}

// TestCompleteRiskHeldTaskRidesItsParent: a task the risk gate holds has no
// PR of its own to wait for: complete says it waits for its parent's
// acceptance, with what held it.
// F-0516 crit. 7
func TestCompleteRiskHeldTaskRidesItsParent(t *testing.T) {
	projDir, repoDir := riskProject(t, []string{"code"}, "vitória", false, map[string]string{"internal/x/x.go": "package x\n"})
	if err := store.SaveFeature(repoDir, store.Feature{ID: "T-0002", Title: "Task T-0002", Parent: "F-0001", State: domain.StateInDevelopment, Facets: []string{"auth"}}); err != nil {
		t.Fatal(err)
	}
	for doc, body := range map[string]string{"spec.md": riskCLISpec(false), "plan.md": "# Doc\n\n## Plano\n\nCorpo real do plano.\n", "result.md": riskCLIResult("vitória")} {
		p := filepath.Join(repoDir, "content", "features", "T-0002", doc)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	commitBackend(t, repoDir)
	t.Setenv("CERNE_ACTOR", "agent")
	out, err := execCerne(t, "feature", "complete", "T-0002", "--no-stamp", "--dir", projDir)
	if err != nil {
		t.Fatalf("complete: %v\n%s", err, out)
	}
	if !strings.Contains(out, "T-0002 waits for the acceptance of its parent F-0001 ([gates] accept = \"risk\"); what held it:\n") || strings.Contains(out, "T-0002 waits for the human merge of its PR") {
		t.Errorf("output must say the task rides its parent:\n%s", out)
	}
}
