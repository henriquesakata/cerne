package cli

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// TestFeatureAmendRefusesAnotherSessionsCard reproduces 2026-11-20 (F-0485
// criteria 1, 2, 3 and 6): an epic in development whose child task has active
// work bound to another session of the same actor on this machine. The spec
// amend is refused naming the task and the work; --force writes, repeats the
// work and says the amend already holds.
func TestFeatureAmendRefusesAnotherSessionsCard(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	seedDocFeature(t, repoDir, "E-0001", domain.StateInDevelopment, realDoc("E-0001", "Spec original."), realDoc("E-0001", "Plano original."))
	now := time.Now().UTC()
	if err := store.SaveFeature(repoDir, store.Feature{ID: "T-0001", Title: "Tarefa filha", Parent: "E-0001", State: domain.StateInDevelopment, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveWork(repoDir, store.Work{ID: "W-0001", Title: "Implementar a tarefa", State: domain.WorkActive, Actor: app.MachineActor(), Refs: []string{"T-0001"}, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	commitBackend(t, repoDir)
	if err := app.BindSession(filepath.Dir(repoDir), "outrasessao", "W-0001"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CERNE_SESSION", "minhasessao")
	file := bodyFile(t, realDoc("E-0001", "Spec emendada."))

	out, err := execCerne(t, "feature", "amend", "E-0001", "--doc", "spec", "--file", file, "--reason", "critério 2 mudou", "--dir", projDir)
	if !errors.Is(err, app.ErrActiveWorkElsewhere) {
		t.Fatalf("amend: err = %v, want ErrActiveWorkElsewhere\n%s", err, out)
	}
	for _, want := range []string{"W-0001  on T-0001", "session: outrases signal", "Implementar a tarefa", "takes effect at once, with no re-approval", "plan.md, T-0001 (in_development)", "--force"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal does not say %q:\n%v", want, err)
		}
	}

	out, err = execCerne(t, "feature", "amend", "E-0001", "--doc", "spec", "--file", file, "--reason", "critério 2 mudou", "--force", "--dir", projDir)
	if err != nil {
		t.Fatalf("amend --force: %v\n%s", err, out)
	}
	for _, want := range []string{"✓ E-0001 spec amended (in_development)", "forced past active work", "W-0001  on T-0001", "takes effect at once, with no re-approval", "written against the previous spec: plan.md, T-0001 (in_development)"} {
		if !strings.Contains(out, want) {
			t.Errorf("forced output does not say %q:\n%s", want, out)
		}
	}
}

// TestFeatureAmendResultIgnoresSessionGuard (criterion 8): the result amend
// has no session guard — another session's work does not refuse it.
func TestFeatureAmendResultIgnoresSessionGuard(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	id := seedResultFeatures(t, repoDir, domain.StateImplemented)[domain.StateImplemented]
	now := time.Now().UTC()
	if err := store.SaveWork(repoDir, store.Work{ID: "W-0001", Title: "Outro", State: domain.WorkActive, Actor: "outro@maquina", Refs: []string{id}, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	commitBackend(t, repoDir)

	if out, err := execCerne(t, "feature", "amend", id, "--file", amendFile(t, id), "--reason", "corrigir", "--dir", projDir); err != nil {
		t.Fatalf("result amend refused: %v\n%s", err, out)
	}
}

// TestFeatureAmendHelpDescribesTheGuard (criterion 10).
func TestFeatureAmendHelpDescribesTheGuard(t *testing.T) {
	out, err := execCerne(t, "feature", "amend", "--help")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"another session", "refused", "--force", "takes effect at once"} {
		if !strings.Contains(out, want) {
			t.Errorf("help does not say %q:\n%s", want, out)
		}
	}
}
