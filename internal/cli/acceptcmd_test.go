package cli

import (
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/project"
	"gitlab.com/rockt/cerne/internal/store"
)

// TestFeatureAccept (F-0131 crit. 3): accepting an implemented feature moves
// it to done and records the optional reason.
func TestFeatureAccept(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	id := newFeature(t, projDir, "Feature A", 1)
	writeDoc(t, repoDir, id, "spec.md", realDoc(id, "Problema real descrito aqui."))
	commitBackend(t, repoDir)
	if _, err := execCerne(t, "feature", "approve", id, "--dir", projDir); err != nil {
		t.Fatalf("approve: %v", err)
	}
	writeDoc(t, repoDir, id, "plan.md", realDoc(id, "Passo 1: implementar."))
	commitBackend(t, repoDir)
	if _, err := execCerne(t, "feature", "start", id, "--dir", projDir); err != nil {
		t.Fatalf("start: %v", err)
	}
	writeDoc(t, repoDir, id, "result.md", realDoc(id, "Entregue conforme o plano."))
	commitBackend(t, repoDir)
	if _, err := execCerne(t, "feature", "complete", id, "--dir", projDir); err != nil {
		t.Fatalf("complete: %v", err)
	}

	out, err := execCerne(t, "feature", "accept", id, "--reason", "validado em homologação", "--dir", projDir)
	if err != nil {
		t.Fatalf("accept: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "implemented") || !strings.Contains(out, "done") {
		t.Fatalf("accept output missing old→new state:\n%s", out)
	}
	f, err := store.LoadFeature(repoDir, id)
	if err != nil {
		t.Fatalf("LoadFeature: %v", err)
	}
	if f.State != domain.StateDone {
		t.Fatalf("state = %s, want done", f.State)
	}
	events, err := store.ReadEvents(repoDir)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	last := events[len(events)-1]
	if last.Type != "feature.accepted" || last.Payload["reason"] != "validado em homologação" {
		t.Fatalf("last event = %+v, want feature.accepted with reason", last)
	}
}

// TestFeatureAcceptWrongState: accepting a candidate is refused (invalid
// transition), state untouched.
func TestFeatureAcceptWrongState(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	id := newFeature(t, projDir, "Feature A", 1)

	_, err := execCerne(t, "feature", "accept", id, "--dir", projDir)
	if err == nil {
		t.Fatalf("accept from candidate succeeded, want error")
	}
	if f, _ := store.LoadFeature(repoDir, id); f.State != domain.StateCandidate {
		t.Fatalf("state after failed accept = %s, want candidate", f.State)
	}
}

// TestFeatureAcceptGateRefusesAgent (crit. 9): under the default human gate,
// an agent actor (CERNE_ACTOR=agent) is refused on accept.
func TestFeatureAcceptGateRefusesAgent(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	id := newFeature(t, projDir, "Feature A", 1)
	writeDoc(t, repoDir, id, "spec.md", realDoc(id, "Problema real descrito aqui."))
	commitBackend(t, repoDir)
	if _, err := execCerne(t, "feature", "approve", id, "--dir", projDir); err != nil {
		t.Fatalf("approve: %v", err)
	}
	writeDoc(t, repoDir, id, "plan.md", realDoc(id, "Passo 1: implementar."))
	commitBackend(t, repoDir)
	if _, err := execCerne(t, "feature", "start", id, "--dir", projDir); err != nil {
		t.Fatalf("start: %v", err)
	}
	writeDoc(t, repoDir, id, "result.md", realDoc(id, "Entregue conforme o plano."))
	commitBackend(t, repoDir)
	if _, err := execCerne(t, "feature", "complete", id, "--dir", projDir); err != nil {
		t.Fatalf("complete: %v", err)
	}

	t.Setenv("CERNE_ACTOR", "agent")
	_, err := execCerne(t, "feature", "accept", id, "--dir", projDir)
	if err == nil {
		t.Fatalf("accept as agent under human gate succeeded, want refusal")
	}
	if !strings.Contains(err.Error(), "human") {
		t.Fatalf("error %q does not mention the human gate", err)
	}
	if f, _ := store.LoadFeature(repoDir, id); f.State != domain.StateImplemented {
		t.Fatalf("state after refused accept = %s, want implemented (untouched)", f.State)
	}
}

// TestFeatureApproveGateRefusesAgent (crit. 9): the same gate applies at the
// start of the flow — an agent actor cannot approve its own spec by default.
func TestFeatureApproveGateRefusesAgent(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	id := newFeature(t, projDir, "Feature A", 1)
	writeDoc(t, repoDir, id, "spec.md", realDoc(id, "Problema real descrito aqui."))
	commitBackend(t, repoDir)

	t.Setenv("CERNE_ACTOR", "agent")
	_, err := execCerne(t, "feature", "approve", id, "--dir", projDir)
	if err == nil {
		t.Fatalf("approve as agent under human gate succeeded, want refusal")
	}
	if !strings.Contains(err.Error(), "human") {
		t.Fatalf("error %q does not mention the human gate", err)
	}
	if f, _ := store.LoadFeature(repoDir, id); f.State != domain.StateCandidate {
		t.Fatalf("state after refused approve = %s, want candidate (untouched)", f.State)
	}
}

// TestFeatureAcceptAllImplemented (crit. 7): --all-implemented accepts every
// implemented feature and skips features in other states.
func TestFeatureAcceptAllImplemented(t *testing.T) {
	projDir, _, repoDir := setupProject(t)

	// Two features driven all the way to implemented.
	ids := make([]string, 2)
	for i, title := range []string{"Feature A", "Feature B"} {
		id := newFeature(t, projDir, title, i+1)
		ids[i] = id
		writeDoc(t, repoDir, id, "spec.md", realDoc(id, "Problema real descrito aqui."))
		commitBackend(t, repoDir)
		if _, err := execCerne(t, "feature", "approve", id, "--dir", projDir); err != nil {
			t.Fatalf("approve %s: %v", id, err)
		}
		writeDoc(t, repoDir, id, "plan.md", realDoc(id, "Passo 1: implementar."))
		commitBackend(t, repoDir)
		if _, err := execCerne(t, "feature", "start", id, "--dir", projDir); err != nil {
			t.Fatalf("start %s: %v", id, err)
		}
		writeDoc(t, repoDir, id, "result.md", realDoc(id, "Entregue conforme o plano."))
		commitBackend(t, repoDir)
		if _, err := execCerne(t, "feature", "complete", id, "--dir", projDir); err != nil {
			t.Fatalf("complete %s: %v", id, err)
		}
	}

	// A third feature left in candidate: --all-implemented must skip it.
	skipped := newFeature(t, projDir, "Feature C (candidate)", 3)

	out, err := execCerne(t, "feature", "accept", "--all-implemented", "--reason", "migração do legado", "--dir", projDir)
	if err != nil {
		t.Fatalf("accept --all-implemented: %v\noutput: %s", err, out)
	}
	for _, id := range ids {
		f, err := store.LoadFeature(repoDir, id)
		if err != nil {
			t.Fatalf("LoadFeature %s: %v", id, err)
		}
		if f.State != domain.StateDone {
			t.Fatalf("state of %s = %s, want done", id, f.State)
		}
	}
	f, err := store.LoadFeature(repoDir, skipped)
	if err != nil {
		t.Fatalf("LoadFeature %s: %v", skipped, err)
	}
	if f.State != domain.StateCandidate {
		t.Fatalf("state of skipped feature %s = %s, want untouched candidate", skipped, f.State)
	}
}

// TestFeatureAcceptAllImplementedRequiresReason: the bulk migration refuses
// without --reason.
func TestFeatureAcceptAllImplementedRequiresReason(t *testing.T) {
	projDir, _, _ := setupProject(t)
	_, err := execCerne(t, "feature", "accept", "--all-implemented", "--dir", projDir)
	if err == nil || !strings.Contains(err.Error(), "reason") {
		t.Fatalf("err = %v, want error naming --reason", err)
	}
}

// TestFeatureAcceptMutuallyExclusiveWithID: an id and --all-implemented
// together is rejected.
func TestFeatureAcceptMutuallyExclusiveWithID(t *testing.T) {
	projDir, _, _ := setupProject(t)
	id := newFeature(t, projDir, "Feature A", 1)
	_, err := execCerne(t, "feature", "accept", id, "--all-implemented", "--reason", "x", "--dir", projDir)
	if err == nil {
		t.Fatalf("accept with id + --all-implemented succeeded, want error")
	}
}

// TestFeatureAcceptGateAutoAllowsAgent: with [gates] accept = "auto" the
// agent actor is allowed through.
func TestFeatureAcceptGateAutoAllowsAgent(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	id := newFeature(t, projDir, "Feature A", 1)
	writeDoc(t, repoDir, id, "spec.md", realDoc(id, "Problema real descrito aqui."))
	commitBackend(t, repoDir)
	if _, err := execCerne(t, "feature", "approve", id, "--dir", projDir); err != nil {
		t.Fatalf("approve: %v", err)
	}
	writeDoc(t, repoDir, id, "plan.md", realDoc(id, "Passo 1: implementar."))
	commitBackend(t, repoDir)
	if _, err := execCerne(t, "feature", "start", id, "--dir", projDir); err != nil {
		t.Fatalf("start: %v", err)
	}
	writeDoc(t, repoDir, id, "result.md", realDoc(id, "Entregue conforme o plano."))
	commitBackend(t, repoDir)
	if _, err := execCerne(t, "feature", "complete", id, "--dir", projDir); err != nil {
		t.Fatalf("complete: %v", err)
	}

	cfg, _, err := project.LoadConfig(projDir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	cfg.Gates.Accept = project.GateAuto
	if err := project.SaveConfig(projDir, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	t.Setenv("CERNE_ACTOR", "agent")
	if _, err := execCerne(t, "feature", "accept", id, "--dir", projDir); err != nil {
		t.Fatalf("accept as agent with gates.accept=auto: %v", err)
	}
	if f, _ := store.LoadFeature(repoDir, id); f.State != domain.StateDone {
		t.Fatalf("state = %s, want done", f.State)
	}
}
