package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/spectmpl"
	"github.com/henriquesakata/cerne/internal/store"
)

// newFeature creates a feature via the CLI and returns its expected ID.
func newFeature(t *testing.T, projDir, title string, seq int) string {
	t.Helper()
	// Born tagged: a complete card (F-0336), so the approve gates under test
	// are the spec's, not the card's.
	out, err := execCerne(t, "feature", "new", title, "--context", "ctx de teste", "--component", "test", "--dir", projDir)
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
	// The Verificação/Barra tail satisfies the approve bar gate and the
	// Gauntlet tail the result-missing-gauntlet lint rule (F-0545), for every
	// fixture doc; sections a doc kind does not need are carried harmlessly.
	return "---\ncerne_id: " + id + "\n---\n\n# Título\n\n" + body + "\n\n## Verificação\n\nBarra: fixture de teste — referência nomeada\n\n## Gauntlet\n\nBarra: fixture de teste — referência nomeada\nRodada 1 — vitória: \"fixture\"\n"
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
	commitBackend(t, repoDir)

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

// TestCompleteReportsEpicReadyToClose (F-0214 crit.1): completing the last
// alive child of an in_development epic scaffolds the epic's result.md with
// the children roster and points the author at the next step, without
// completing the epic itself.
func TestCompleteReportsEpicReadyToClose(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	epic := newFeature(t, projDir, "Épico", 1)
	writeDoc(t, repoDir, epic, "spec.md", realDoc(epic, "Problema real descrito aqui."))
	commitBackend(t, repoDir)
	if _, err := execCerne(t, "feature", "approve", epic, "--dir", projDir); err != nil {
		t.Fatalf("approve epic: %v", err)
	}
	writeDoc(t, repoDir, epic, "plan.md", realDoc(epic, "Passo 1: decompor."))
	commitBackend(t, repoDir)

	child := newFeature(t, projDir, "Fase 1", 2)
	writeDoc(t, repoDir, child, "spec.md", realDoc(child, "Problema real descrito aqui."))
	commitBackend(t, repoDir)
	if _, err := execCerne(t, "feature", "approve", child, "--dir", projDir); err != nil {
		t.Fatalf("approve child: %v", err)
	}
	// Link the child to the epic and give it a plan so it can start.
	f, err := store.LoadFeature(repoDir, child)
	if err != nil {
		t.Fatalf("LoadFeature child: %v", err)
	}
	f.Parent = epic
	if err := store.SaveFeature(repoDir, f); err != nil {
		t.Fatalf("SaveFeature child.Parent: %v", err)
	}
	writeDoc(t, repoDir, child, "plan.md", realDoc(child, "Passo 1: implementar."))
	commitBackend(t, repoDir)
	if _, err := execCerne(t, "feature", "start", child, "--dir", projDir); err != nil {
		t.Fatalf("start child: %v", err)
	}
	if got, _ := store.LoadFeature(repoDir, epic); got.State != domain.StateInDevelopment {
		t.Fatalf("epic state = %s, want in_development (propagated by child start)", got.State)
	}

	writeDoc(t, repoDir, child, "result.md", realDoc(child, "Entregue conforme o plano."))
	commitBackend(t, repoDir)
	out, err := execCerne(t, "feature", "complete", child, "--dir", projDir)
	if err != nil {
		t.Fatalf("complete child: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "epic "+epic+" ready to close") {
		t.Fatalf("complete output should report the epic ready to close:\n%s", out)
	}
	if got, _ := store.LoadFeature(repoDir, epic); got.State != domain.StateInDevelopment {
		t.Fatalf("epic state = %s, want unchanged in_development (never auto-completes)", got.State)
	}
	data, err := os.ReadFile(filepath.Join(repoDir, "content", "features", epic, "result.md"))
	if err != nil {
		t.Fatalf("epic result.md should be scaffolded: %v", err)
	}
	if !strings.Contains(string(data), child) {
		t.Fatalf("epic result.md roster missing child %s:\n%s", child, data)
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

// F-0545 crit. 1 wiring: the approve command itself refuses a real spec that
// names no Gauntlet bar, in one line pointing at --check. Unit coverage of the
// forms lives in app/bargate_test.go — this pins the CLI call site.
func TestApproveRefusesSpecWithoutBar(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	id := newFeature(t, projDir, "Feature sem barra", 1)

	spec := "---\ncerne_id: " + id + "\n---\n\n# Título\n\n## Problema\n\nProblema real descrito aqui.\n\n## Verificação\n\nSó testes unitários.\n"
	writeDoc(t, repoDir, id, "spec.md", spec)
	commitBackend(t, repoDir)

	out, err := execCerne(t, "feature", "approve", id, "--dir", projDir)
	if err == nil {
		t.Fatalf("approve without bar succeeded\noutput: %s", out)
	}
	if !errors.Is(err, app.ErrSpecMissingBar) {
		t.Fatalf("err = %v, want ErrSpecMissingBar", err)
	}
	// One line pointing at the check, which teaches the forms (F-0308 crit. 11).
	if !strings.Contains(err.Error(), "cerne feature approve "+id+" --check") {
		t.Fatalf("refusal does not point at the check: %v", err)
	}
	if f, _ := store.LoadFeature(repoDir, id); f.State != domain.StateCandidate {
		t.Fatalf("state after refusal = %s, want candidate", f.State)
	}

	// --force does not cross this gate (unlike the clarification gate).
	if _, err := execCerne(t, "feature", "approve", id, "--force", "--dir", projDir); !errors.Is(err, app.ErrSpecMissingBar) {
		t.Fatalf("--force crossed the bar gate: %v", err)
	}
}

// TestReapproveIsHumanGated (F-0318 crit. 7): re-approving a stale approval
// is the approve itself — the same human gate refuses an agent, and the human
// re-approval renews the approval and says so.
func TestReapproveIsHumanGated(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	id := newFeature(t, projDir, "Feature A", 1)
	writeDoc(t, repoDir, id, "spec.md", realDoc(id, "Problema real descrito aqui."))
	commitBackend(t, repoDir)
	if out, err := execCerne(t, "feature", "approve", id, "--dir", projDir); err != nil {
		t.Fatalf("approve: %v\n%s", err, out)
	}
	writeDoc(t, repoDir, id, "spec.md", realDoc(id, "Problema real, com um critério novo."))
	commitBackend(t, repoDir)

	t.Setenv("CERNE_ACTOR", "agent")
	if _, err := execCerne(t, "feature", "approve", id, "--dir", projDir); !errors.Is(err, app.ErrHumanGate) {
		t.Fatalf("agent re-approve err = %v, want ErrHumanGate", err)
	}
	t.Setenv("CERNE_ACTOR", "")
	out, err := execCerne(t, "feature", "approve", id, "--dir", projDir)
	if err != nil {
		t.Fatalf("human re-approve: %v\n%s", err, out)
	}
	for _, want := range []string{id + ": approved → approved", "Approval renewed: the current spec is now the approved version."} {
		if !strings.Contains(out, want) {
			t.Errorf("re-approve output missing %q:\n%s", want, out)
		}
	}
}

// TestReapproveNextStepIsTruthful (F-0318 crit. 7): after a re-approval the
// hint only says start is open when the start gate would pass — with the plan
// still the stub it keeps pointing at the plan.
func TestReapproveNextStepIsTruthful(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	id := newFeature(t, projDir, "Feature A", 1)
	writeDoc(t, repoDir, id, "spec.md", realDoc(id, "Problema real descrito aqui."))
	commitBackend(t, repoDir)
	if out, err := execCerne(t, "feature", "approve", id, "--dir", projDir); err != nil {
		t.Fatalf("approve: %v\n%s", err, out)
	}
	reapprove := func(body string) string {
		t.Helper()
		writeDoc(t, repoDir, id, "spec.md", realDoc(id, body))
		commitBackend(t, repoDir)
		out, err := execCerne(t, "feature", "approve", id, "--dir", projDir)
		if err != nil {
			t.Fatalf("re-approve: %v\n%s", err, out)
		}
		return out
	}

	out := reapprove("Problema real, versão B.")
	if !strings.Contains(out, "fill in the plan") || strings.Contains(out, "is open again") {
		t.Fatalf("stub plan: want the plan hint and no 'start is open':\n%s", out)
	}

	writeDoc(t, repoDir, id, "plan.md", realDoc(id, "Plano real."))
	out = reapprove("Problema real, versão C.")
	if !strings.Contains(out, "'cerne feature start "+id+"' is open again") || strings.Contains(out, "fill in the plan") {
		t.Fatalf("real plan: want 'start is open' and no plan hint:\n%s", out)
	}
}

// TestApproveRecordsBaselineCLI (F-0318 crit. 14): on an approved feature with
// no approved copy (approved before F-0318) the check calls it approvable and
// says the baseline will be recorded; the human approve records it and says so.
func TestApproveRecordsBaselineCLI(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	id := newFeature(t, projDir, "Feature A", 1)
	writeDoc(t, repoDir, id, "spec.md", realDoc(id, "Problema real descrito aqui."))
	commitBackend(t, repoDir)
	if out, err := execCerne(t, "feature", "approve", id, "--dir", projDir); err != nil {
		t.Fatalf("approve: %v\n%s", err, out)
	}
	if err := os.RemoveAll(filepath.Dir(store.ApprovedSpecPath(repoDir, id))); err != nil {
		t.Fatal(err)
	}
	commitBackend(t, repoDir)

	out, err := execCerne(t, "feature", "approve", id, "--check", "--dir", projDir)
	if err != nil || !strings.Contains(out, id+" is approvable") || !strings.Contains(out, "records the current spec as its approved version") {
		t.Fatalf("check (%v) does not report an approvable baseline:\n%s", err, out)
	}
	out, err = execCerne(t, "feature", "approve", id, "--dir", projDir)
	if err != nil || !strings.Contains(out, "Approval baseline recorded: the current spec is now the approved version.") || !strings.Contains(out, "fill in the plan") {
		t.Fatalf("baseline approve (%v):\n%s", err, out)
	}
	if _, err := os.Stat(store.ApprovedSpecPath(repoDir, id)); err != nil {
		t.Fatalf("no baseline copy: %v", err)
	}
}
