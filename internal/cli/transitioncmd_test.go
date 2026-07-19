package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/project"
	"gitlab.com/rockt/cerne/internal/spectmpl"
	"gitlab.com/rockt/cerne/internal/store"
)

// newFeature creates a feature via the CLI and returns its expected ID.
func newFeature(t *testing.T, projDir, title string, seq int) string {
	t.Helper()
	out, err := execCerne(t, "feature", "new", title, "--context", "ctx de teste", "--dir", projDir)
	if err != nil {
		t.Fatalf("feature new %q: %v\noutput: %s", title, err, out)
	}
	return fmt.Sprintf("F-%04d", seq)
}

// writeDoc writes a feature lifecycle document straight into the backend clone.
func writeDoc(t *testing.T, repoDir, id, filename, content string) {
	t.Helper()
	dir := filepath.Join(repoDir, "content", "features", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, filename), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", filename, err)
	}
}

// commitBackend commits (and pushes) state a test staged directly in the
// backend clone: mutations start from a clean HEAD (F-0121/F-0122), so staged
// state must be committed to be visible to them.
func commitBackend(t *testing.T, repoDir string) {
	t.Helper()
	if _, err := gitback.CommitAndPush(repoDir, "test: stage state"); err != nil && !errors.Is(err, gitback.ErrNothingToCommit) {
		t.Fatalf("commitBackend: %v", err)
	}
}

func realDoc(id, body string) string {
	return "---\ncerne_id: " + id + "\n---\n\n# Título\n\n" + body + "\n"
}

func TestTransitionFullLifecycle(t *testing.T) {
	projDir, origin, repoDir := setupProject(t)
	id := newFeature(t, projDir, "Feature A", 1)

	// The CLI creation gate (F-0120) seeds context into the spec, which counts
	// as real content — restore the pure scaffold to prove the approve gate
	// still blocks a stub spec.
	writeDoc(t, repoDir, id, "spec.md", "---\ncerne_id: "+id+"\n---\n\n"+spectmpl.Stub("Feature A"))
	commitBackend(t, repoDir)

	// approve while the spec is still the stub: gate error with hint.
	out, err := execCerne(t, "feature", "approve", id, "--dir", projDir)
	if err == nil {
		t.Fatalf("approve with stub spec succeeded\noutput: %s", out)
	}
	if !strings.Contains(err.Error(), "spec") || !strings.Contains(err.Error(), "spec.md") {
		t.Fatalf("gate error %q does not mention spec / spec.md", err)
	}
	if f, _ := store.LoadFeature(repoDir, id); f.State != domain.StateCandidate {
		t.Fatalf("state after failed gate = %s, want candidate", f.State)
	}

	writeDoc(t, repoDir, id, "spec.md", realDoc(id, "Problema real descrito aqui."))
	commitBackend(t, repoDir)
	out, err = execCerne(t, "feature", "approve", id, "--dir", projDir)
	if err != nil {
		t.Fatalf("approve: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "candidate") || !strings.Contains(out, "approved") {
		t.Fatalf("approve output missing old→new state:\n%s", out)
	}
	f, err := store.LoadFeature(repoDir, id)
	if err != nil {
		t.Fatalf("LoadFeature: %v", err)
	}
	if f.State != domain.StateApproved {
		t.Fatalf("state = %s, want approved", f.State)
	}
	if !f.UpdatedAt.After(f.CreatedAt) {
		t.Fatalf("UpdatedAt = %v not after CreatedAt = %v", f.UpdatedAt, f.CreatedAt)
	}

	// lock snapshot tracks the pushed origin HEAD after a transition.
	lock, err := project.LoadLock(projDir)
	if err != nil {
		t.Fatalf("LoadLock: %v", err)
	}
	originHead, err := gitback.HeadSHA(origin)
	if err != nil {
		t.Fatalf("HeadSHA origin: %v", err)
	}
	if lock.Snapshot != originHead {
		t.Fatalf("lock.Snapshot = %s, want origin HEAD %s", lock.Snapshot, originHead)
	}

	// start gated on a real plan.
	if _, err := execCerne(t, "feature", "start", id, "--dir", projDir); err == nil || !strings.Contains(err.Error(), "plan.md") {
		t.Fatalf("start without plan err = %v, want plan.md gate error", err)
	}
	writeDoc(t, repoDir, id, "plan.md", realDoc(id, "Passo 1: implementar."))
	commitBackend(t, repoDir)
	if out, err := execCerne(t, "feature", "start", id, "--dir", projDir); err != nil {
		t.Fatalf("start: %v\noutput: %s", err, out)
	}

	// complete gated on a real result.
	if _, err := execCerne(t, "feature", "complete", id, "--dir", projDir); err == nil || !strings.Contains(err.Error(), "result.md") {
		t.Fatalf("complete without result err = %v, want result.md gate error", err)
	}
	writeDoc(t, repoDir, id, "result.md", realDoc(id, "Entregue conforme o plano."))
	commitBackend(t, repoDir)
	if out, err := execCerne(t, "feature", "complete", id, "--dir", projDir); err != nil {
		t.Fatalf("complete: %v\noutput: %s", err, out)
	}
	if f, _ := store.LoadFeature(repoDir, id); f.State != domain.StateImplemented {
		t.Fatalf("state = %s, want implemented", f.State)
	}

	events, err := store.ReadEvents(repoDir)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	var types []string
	for _, ev := range events {
		types = append(types, ev.Type)
	}
	// The start/complete pair also auto-registers the activity (F-0146):
	// work.started rides the start commit, work.done the complete commit.
	want := []string{"feature.created", "feature.approved", "feature.started", "work.started", "feature.completed", "work.done"}
	if strings.Join(types, ",") != strings.Join(want, ",") {
		t.Fatalf("event types = %v, want %v", types, want)
	}
}

// TestApproveScaffoldsPlanStub asserts approving a feature scaffolds plan.md
// from the canonical plan template, the author is pointed at it, yet it still
// reads as a stub so `start` stays gated on a real plan.
func TestApproveScaffoldsPlanStub(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	id := newFeature(t, projDir, "Feature A", 1)
	writeDoc(t, repoDir, id, "spec.md", realDoc(id, "Problema real descrito aqui."))

	out, err := execCerne(t, "feature", "approve", id, "--dir", projDir)
	if err != nil {
		t.Fatalf("approve: %v\noutput: %s", err, out)
	}
	// The approve output points the author at the plan next step.
	if !strings.Contains(out, "plan.md") {
		t.Fatalf("approve output should hint the plan next step:\n%s", out)
	}
	// The plan was scaffolded from the canonical template...
	data, err := os.ReadFile(filepath.Join(repoDir, "content", "features", id, "plan.md"))
	if err != nil {
		t.Fatalf("approve should scaffold plan.md: %v", err)
	}
	for _, want := range []string{"## Estratégia", "## Sequência de tarefas", "## Definição de pronto"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("scaffolded plan missing section %q:\n%s", want, data)
		}
	}
	// ...yet it still reads as a stub, so start stays gated on a real plan.
	if _, err := execCerne(t, "feature", "start", id, "--dir", projDir); err == nil || !strings.Contains(err.Error(), "plan.md") {
		t.Fatalf("start after approve should still be gated on a real plan, err = %v", err)
	}
}

// TestStartScaffoldsResultStub asserts starting development scaffolds result.md
// from the canonical result template, the author is pointed at it, yet it still
// reads as a stub so `complete` stays gated on a real result.
func TestStartScaffoldsResultStub(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	id := newFeature(t, projDir, "Feature A", 1)
	writeDoc(t, repoDir, id, "spec.md", realDoc(id, "Problema real descrito aqui."))
	commitBackend(t, repoDir)
	if _, err := execCerne(t, "feature", "approve", id, "--dir", projDir); err != nil {
		t.Fatalf("approve: %v", err)
	}
	writeDoc(t, repoDir, id, "plan.md", realDoc(id, "Passo 1: implementar."))
	commitBackend(t, repoDir)

	out, err := execCerne(t, "feature", "start", id, "--dir", projDir)
	if err != nil {
		t.Fatalf("start: %v\noutput: %s", err, out)
	}
	// The start output points the author at the result next step.
	if !strings.Contains(out, "result.md") {
		t.Fatalf("start output should hint the result next step:\n%s", out)
	}
	// The result was scaffolded from the canonical template...
	data, err := os.ReadFile(filepath.Join(repoDir, "content", "features", id, "result.md"))
	if err != nil {
		t.Fatalf("start should scaffold result.md: %v", err)
	}
	for _, want := range []string{"## Resultado", "## Aceitação", "## Aprendizados e desvios"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("scaffolded result missing section %q:\n%s", want, data)
		}
	}
	// ...yet it still reads as a stub, so complete stays gated on a real result.
	if _, err := execCerne(t, "feature", "complete", id, "--dir", projDir); err == nil || !strings.Contains(err.Error(), "result.md") {
		t.Fatalf("complete after start should still be gated on a real result, err = %v", err)
	}
}

func TestTransitionFlagRules(t *testing.T) {
	projDir, _, _ := setupProject(t)
	id := newFeature(t, projDir, "Feature A", 1)
	id2 := newFeature(t, projDir, "Feature B", 2)

	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{"approve with reason", []string{"feature", "approve", id, "--reason", "x"}, "--by/--reason"},
		{"approve with by", []string{"feature", "approve", id, "--by", id2}, "--by/--reason"},
		{"start with reason", []string{"feature", "start", id, "--reason", "x"}, "--by/--reason"},
		{"complete with by", []string{"feature", "complete", id, "--by", id2}, "--by/--reason"},
		{"reject without reason", []string{"feature", "reject", id}, "--reason"},
		{"reject with by", []string{"feature", "reject", id, "--reason", "x", "--by", id2}, "--by"},
		{"revert without reason", []string{"feature", "revert", id}, "--reason"},
		{"revert with by", []string{"feature", "revert", id, "--reason", "x", "--by", id2}, "--by"},
		{"substituir without by", []string{"feature", "substituir", id, "--reason", "x"}, "--by"},
		{"substituir without reason", []string{"feature", "substituir", id, "--by", id2}, "--reason"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := append(tt.args, "--dir", projDir)
			out, err := execCerne(t, args...)
			if err == nil {
				t.Fatalf("command succeeded, want error mentioning %q\noutput: %s", tt.wantErr, out)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error %q does not mention %q", err, tt.wantErr)
			}
		})
	}
}

func TestTransitionReject(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	id := newFeature(t, projDir, "Feature A", 1)

	out, err := execCerne(t, "feature", "reject", id, "--reason", "fora de escopo", "--dir", projDir)
	if err != nil {
		t.Fatalf("reject: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "candidate") || !strings.Contains(out, "rejected") {
		t.Fatalf("reject output missing old→new state:\n%s", out)
	}
	f, err := store.LoadFeature(repoDir, id)
	if err != nil {
		t.Fatalf("LoadFeature: %v", err)
	}
	if f.State != domain.StateRejected || f.Reason != "fora de escopo" {
		t.Fatalf("feature = %+v, want rejected with reason", f)
	}
	events, err := store.ReadEvents(repoDir)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	last := events[len(events)-1]
	if last.Type != "feature.rejected" || last.Payload["reason"] != "fora de escopo" {
		t.Fatalf("last event = %+v, want feature.rejected with reason payload", last)
	}
}

func TestTransitionSubstituir(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	id := newFeature(t, projDir, "Feature A", 1)
	id2 := newFeature(t, projDir, "Feature B", 2)

	// --by self is rejected.
	if _, err := execCerne(t, "feature", "substituir", id, "--by", id, "--reason", "dup", "--dir", projDir); err == nil {
		t.Fatalf("substituir --by self succeeded, want error")
	}
	// --by must exist in the backend.
	if _, err := execCerne(t, "feature", "substituir", id, "--by", "F-9999", "--reason", "dup", "--dir", projDir); err == nil || !strings.Contains(err.Error(), "F-9999") {
		t.Fatalf("substituir --by nonexistent err = %v, want error naming F-9999", err)
	}

	out, err := execCerne(t, "feature", "substituir", id, "--by", id2, "--reason", "duplicada da Feature B", "--dir", projDir)
	if err != nil {
		t.Fatalf("substituir: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "superseded") {
		t.Fatalf("substituir output missing new state:\n%s", out)
	}
	f, err := store.LoadFeature(repoDir, id)
	if err != nil {
		t.Fatalf("LoadFeature: %v", err)
	}
	if f.State != domain.StateSuperseded || f.SupersededBy != id2 || f.Reason != "duplicada da Feature B" {
		t.Fatalf("feature = %+v, want superseded by %s with reason", f, id2)
	}
	events, err := store.ReadEvents(repoDir)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	last := events[len(events)-1]
	if last.Type != "feature.superseded" || last.Payload["by"] != id2 || last.Payload["reason"] != "duplicada da Feature B" {
		t.Fatalf("last event = %+v, want feature.superseded with by/reason payload", last)
	}
}

// TestTransitionRevert (F-0169 crit. 3, 7): revert walks an approval back to
// candidate, logs feature.reverted with the reason, and deletes nothing — the
// spec and the plan stub scaffolded by the approve survive intact.
func TestTransitionRevert(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	id := newFeature(t, projDir, "Feature A", 1)
	spec := realDoc(id, "Problema real descrito aqui.")
	writeDoc(t, repoDir, id, "spec.md", spec)
	commitBackend(t, repoDir)
	if _, err := execCerne(t, "feature", "approve", id, "--dir", projDir); err != nil {
		t.Fatalf("approve: %v", err)
	}

	// revert only walks back an approval: in_development and beyond refuse.
	out, err := execCerne(t, "feature", "revert", id, "--reason", "aprovada por engano", "--dir", projDir)
	if err != nil {
		t.Fatalf("revert: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "approved") || !strings.Contains(out, "candidate") {
		t.Fatalf("revert output missing old→new state:\n%s", out)
	}
	f, err := store.LoadFeature(repoDir, id)
	if err != nil {
		t.Fatalf("LoadFeature: %v", err)
	}
	if f.State != domain.StateCandidate {
		t.Fatalf("state = %s, want candidate", f.State)
	}
	// The reason lives in the event, not on the entity (Reason is reserved for
	// terminal states — a revived candidate carries no stale annotation).
	if f.Reason != "" {
		t.Fatalf("f.Reason = %q, want empty (reason belongs to the event)", f.Reason)
	}
	events, err := store.ReadEvents(repoDir)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	last := events[len(events)-1]
	if last.Type != "feature.reverted" || last.Payload["reason"] != "aprovada por engano" {
		t.Fatalf("last event = %+v, want feature.reverted with reason payload", last)
	}
	// Nothing was deleted: the spec is intact and the plan stub scaffolded by
	// the approve is still there for a future re-approval.
	data, err := os.ReadFile(filepath.Join(repoDir, "content", "features", id, "spec.md"))
	if err != nil || string(data) != spec {
		t.Fatalf("spec.md after revert = (%v) %q, want the original spec intact", err, data)
	}
	if _, err := os.Stat(filepath.Join(repoDir, "content", "features", id, "plan.md")); err != nil {
		t.Fatalf("plan.md should survive the revert: %v", err)
	}

	// A reverted candidate refuses a second revert (nothing to walk back).
	if _, err := execCerne(t, "feature", "revert", id, "--reason", "de novo", "--dir", projDir); err == nil {
		t.Fatalf("revert from candidate succeeded, want invalid transition")
	}
}

// TestTransitionRevertGateRefusesAgent (F-0169 crit. 5): undoing a human act
// is a human act — the default gate refuses an agent actor.
func TestTransitionRevertGateRefusesAgent(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	id := newFeature(t, projDir, "Feature A", 1)
	writeDoc(t, repoDir, id, "spec.md", realDoc(id, "Problema real descrito aqui."))
	commitBackend(t, repoDir)
	if _, err := execCerne(t, "feature", "approve", id, "--dir", projDir); err != nil {
		t.Fatalf("approve: %v", err)
	}

	t.Setenv("CERNE_ACTOR", "agent")
	_, err := execCerne(t, "feature", "revert", id, "--reason", "aprovada por engano", "--dir", projDir)
	if err == nil {
		t.Fatalf("revert as agent under human gate succeeded, want refusal")
	}
	if !strings.Contains(err.Error(), "human") {
		t.Fatalf("error %q does not mention the human gate", err)
	}
	if f, _ := store.LoadFeature(repoDir, id); f.State != domain.StateApproved {
		t.Fatalf("state after refused revert = %s, want approved (untouched)", f.State)
	}
}

// TestTransitionRevertGateAutoAllowsAgent (F-0169 crit. 5): [gates]
// revert = "auto" is the explicit per-project bypass.
func TestTransitionRevertGateAutoAllowsAgent(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	id := newFeature(t, projDir, "Feature A", 1)
	writeDoc(t, repoDir, id, "spec.md", realDoc(id, "Problema real descrito aqui."))
	commitBackend(t, repoDir)
	if _, err := execCerne(t, "feature", "approve", id, "--dir", projDir); err != nil {
		t.Fatalf("approve: %v", err)
	}

	cfg, _, err := project.LoadConfig(projDir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	cfg.Gates.Revert = project.GateAuto
	if err := project.SaveConfig(projDir, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	t.Setenv("CERNE_ACTOR", "agent")
	if _, err := execCerne(t, "feature", "revert", id, "--reason", "aprovada por engano", "--dir", projDir); err != nil {
		t.Fatalf("revert as agent with gates.revert=auto: %v", err)
	}
	if f, _ := store.LoadFeature(repoDir, id); f.State != domain.StateCandidate {
		t.Fatalf("state = %s, want candidate", f.State)
	}
}

func TestTransitionMissingFeature(t *testing.T) {
	projDir, _, _ := setupProject(t)
	_, err := execCerne(t, "feature", "approve", "F-9999", "--dir", projDir)
	if err == nil || !strings.Contains(err.Error(), "F-9999") {
		t.Fatalf("approve missing feature err = %v, want error naming F-9999", err)
	}
}
