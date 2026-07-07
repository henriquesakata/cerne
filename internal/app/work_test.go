package app

import (
	"errors"
	"os"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

func TestStartWorkDefaultActor(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	w, err := StartWork(repoDir, projDir, "Investigar erro X", nil, "", false)
	if err != nil {
		t.Fatalf("StartWork: %v", err)
	}
	if w.ID != "W-0001" || w.Title != "Investigar erro X" || w.State != domain.WorkActive {
		t.Fatalf("work = %+v, want W-0001 active 'Investigar erro X'", w)
	}
	host, _ := os.Hostname()
	user := os.Getenv("USER")
	if user == "" {
		user = os.Getenv("USERNAME")
	}
	wantActor := user + "@" + host
	if w.Actor != wantActor {
		t.Fatalf("actor = %q, want %q", w.Actor, wantActor)
	}
	got, err := store.LoadWork(repoDir, w.ID)
	if err != nil {
		t.Fatalf("LoadWork: %v", err)
	}
	if got.Actor != wantActor {
		t.Fatalf("persisted actor = %q, want %q", got.Actor, wantActor)
	}
}

func TestStartWorkExplicitAgent(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	w, err := StartWork(repoDir, projDir, "Revisar MR Y", nil, "claude-cli", false)
	if err != nil {
		t.Fatalf("StartWork: %v", err)
	}
	if w.Actor != "claude-cli" {
		t.Fatalf("actor = %q, want claude-cli", w.Actor)
	}
}

func TestStartWorkValidatesRefs(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	f, err := CreateFeature(repoDir, projDir, "Feature base", "", "", nil, nil)
	if err != nil {
		t.Fatalf("CreateFeature: %v", err)
	}

	w, err := StartWork(repoDir, projDir, "Trabalhar em cima", []string{f.ID}, "", false)
	if err != nil {
		t.Fatalf("StartWork: %v", err)
	}
	if len(w.Refs) != 1 || w.Refs[0] != f.ID {
		t.Fatalf("refs = %v, want [%s]", w.Refs, f.ID)
	}
}

func TestStartWorkUnknownRefAborts(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	_, err := StartWork(repoDir, projDir, "Trabalhar em cima", []string{"F-9999"}, "", false)
	if err == nil {
		t.Fatal("StartWork with unknown ref: want error, got nil")
	}
	works, lerr := store.LoadAllWorks(repoDir)
	if lerr != nil {
		t.Fatalf("LoadAllWorks: %v", lerr)
	}
	if len(works) != 0 {
		t.Fatalf("invalid StartWork persisted %d work(s), want 0", len(works))
	}
}

func TestFinishWorkDone(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	w, err := StartWork(repoDir, projDir, "Investigar erro X", nil, "", false)
	if err != nil {
		t.Fatalf("StartWork: %v", err)
	}
	out, err := FinishWork(repoDir, projDir, w.ID, "resolvido: race condition", false, "")
	if err != nil {
		t.Fatalf("FinishWork: %v", err)
	}
	if out.From != domain.WorkActive || out.To != domain.WorkDone {
		t.Fatalf("outcome = %+v, want active->done", out)
	}
	got, err := store.LoadWork(repoDir, w.ID)
	if err != nil {
		t.Fatalf("LoadWork: %v", err)
	}
	if got.State != domain.WorkDone || got.Note != "resolvido: race condition" {
		t.Fatalf("got %+v, want done with note", got)
	}
}

func TestFinishWorkAbandon(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	w, err := StartWork(repoDir, projDir, "Investigar erro X", nil, "", false)
	if err != nil {
		t.Fatalf("StartWork: %v", err)
	}
	out, err := FinishWork(repoDir, projDir, w.ID, "", true, "duplicado de outro agente")
	if err != nil {
		t.Fatalf("FinishWork: %v", err)
	}
	if out.From != domain.WorkActive || out.To != domain.WorkAbandoned {
		t.Fatalf("outcome = %+v, want active->abandoned", out)
	}
	got, err := store.LoadWork(repoDir, w.ID)
	if err != nil {
		t.Fatalf("LoadWork: %v", err)
	}
	if got.State != domain.WorkAbandoned || got.Note != "duplicado de outro agente" {
		t.Fatalf("got %+v, want abandoned with reason", got)
	}
}

func TestFinishWorkAbandonRequiresReason(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	w, err := StartWork(repoDir, projDir, "Investigar erro X", nil, "", false)
	if err != nil {
		t.Fatalf("StartWork: %v", err)
	}
	_, err = FinishWork(repoDir, projDir, w.ID, "", true, "")
	if err == nil {
		t.Fatal("FinishWork abandon without reason: want error, got nil")
	}
	got, lerr := store.LoadWork(repoDir, w.ID)
	if lerr != nil {
		t.Fatalf("LoadWork: %v", lerr)
	}
	if got.State != domain.WorkActive {
		t.Fatalf("work state = %s, want still active (refused)", got.State)
	}
}

func TestFinishWorkTerminalRefused(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	w, err := StartWork(repoDir, projDir, "Investigar erro X", nil, "", false)
	if err != nil {
		t.Fatalf("StartWork: %v", err)
	}
	if _, err := FinishWork(repoDir, projDir, w.ID, "ok", false, ""); err != nil {
		t.Fatalf("FinishWork: %v", err)
	}
	_, err = FinishWork(repoDir, projDir, w.ID, "de novo", false, "")
	if !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatalf("err = %v, want errors.Is ErrInvalidTransition", err)
	}
}

func TestFinishWorkNotFound(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	_, err := FinishWork(repoDir, projDir, "W-9999", "ok", false, "")
	if err == nil {
		t.Fatal("FinishWork on unknown id: want error, got nil")
	}
	if !strings.Contains(err.Error(), "W-9999") {
		t.Fatalf("err = %v, want to mention W-9999", err)
	}
}
