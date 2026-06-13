package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/project"
	"gitlab.com/rockt/cerne/internal/store"
)

// newDecision creates a decision via the CLI and returns its expected ID.
func newDecision(t *testing.T, projDir, title string, seq int) string {
	t.Helper()
	out, err := execCerne(t, "decision", "new", title, "--dir", projDir)
	if err != nil {
		t.Fatalf("decision new %q: %v\noutput: %s", title, err, out)
	}
	return fmt.Sprintf("D-%04d", seq)
}

func TestDecisionNew(t *testing.T) {
	projDir, origin, repoDir := setupProject(t)

	out, err := execCerne(t, "decision", "new", "Usar Postgres", "--dir", projDir)
	if err != nil {
		t.Fatalf("decision new: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "D-0001 criada (active)") {
		t.Fatalf("decision new output missing D-0001 confirmation:\n%s", out)
	}

	d, err := store.LoadDecision(repoDir, "D-0001")
	if err != nil {
		t.Fatalf("LoadDecision: %v", err)
	}
	if d.State != domain.DecisionActive || d.Title != "Usar Postgres" {
		t.Fatalf("decision = %+v, want active titled 'Usar Postgres'", d)
	}

	// stub ADR doc with cerne_id frontmatter + pending placeholder.
	body, err := os.ReadFile(filepath.Join(repoDir, "content", "decisions", "D-0001.md"))
	if err != nil {
		t.Fatalf("read decision doc: %v", err)
	}
	if !strings.HasPrefix(string(body), "---\ncerne_id: D-0001\n---") {
		t.Fatalf("decision doc missing cerne_id frontmatter:\n%s", body)
	}
	if !strings.Contains(string(body), "> Decisão pendente") {
		t.Fatalf("decision doc is not the pending stub:\n%s", body)
	}

	evs, err := store.ReadEvents(repoDir)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	last := evs[len(evs)-1]
	if last.Type != "decision.created" || last.EntityID != "D-0001" || last.Payload["title"] != "Usar Postgres" {
		t.Fatalf("last event = %+v, want decision.created for D-0001 with title", last)
	}

	// lock snapshot tracks the pushed origin HEAD.
	lock, err := project.LoadLock(projDir)
	if err != nil {
		t.Fatalf("LoadLock: %v", err)
	}
	head, err := gitback.HeadSHA(origin)
	if err != nil {
		t.Fatalf("HeadSHA origin: %v", err)
	}
	if lock.Snapshot != head {
		t.Fatalf("lock.Snapshot = %s, want origin HEAD %s", lock.Snapshot, head)
	}

	// empty title is rejected.
	if _, err := execCerne(t, "decision", "new", "   ", "--dir", projDir); err == nil {
		t.Fatalf("decision new with blank title succeeded, want error")
	}
}

func TestDecisionSubstituir(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	id := newDecision(t, projDir, "ADR A", 1)
	id2 := newDecision(t, projDir, "ADR B", 2)

	// --by self is rejected.
	if _, err := execCerne(t, "decision", "substituir", id, "--by", id, "--reason", "dup", "--dir", projDir); err == nil {
		t.Fatalf("substituir --by self succeeded, want error")
	}
	// --by must exist in the backend.
	if _, err := execCerne(t, "decision", "substituir", id, "--by", "D-9999", "--reason", "dup", "--dir", projDir); err == nil || !strings.Contains(err.Error(), "D-9999") {
		t.Fatalf("substituir --by nonexistent err = %v, want error naming D-9999", err)
	}
	// the decision itself must exist.
	if _, err := execCerne(t, "decision", "substituir", "D-9999", "--by", id2, "--reason", "x", "--dir", projDir); err == nil || !strings.Contains(err.Error(), "D-9999") {
		t.Fatalf("substituir missing decision err = %v, want error naming D-9999", err)
	}

	out, err := execCerne(t, "decision", "substituir", id, "--by", id2, "--reason", "trocamos de banco", "--dir", projDir)
	if err != nil {
		t.Fatalf("substituir: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "active") || !strings.Contains(out, "superseded") {
		t.Fatalf("substituir output missing old→new state:\n%s", out)
	}
	d, err := store.LoadDecision(repoDir, id)
	if err != nil {
		t.Fatalf("LoadDecision: %v", err)
	}
	if d.State != domain.DecisionSuperseded || d.SupersededBy != id2 || d.Reason != "trocamos de banco" {
		t.Fatalf("decision = %+v, want superseded by %s with reason", d, id2)
	}
	evs, err := store.ReadEvents(repoDir)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	last := evs[len(evs)-1]
	if last.Type != "decision.superseded" || last.Payload["by"] != id2 || last.Payload["reason"] != "trocamos de banco" {
		t.Fatalf("last event = %+v, want decision.superseded with by/reason payload", last)
	}

	// a terminal (already superseded) decision cannot be superseded again.
	if _, err := execCerne(t, "decision", "substituir", id, "--by", id2, "--reason", "again", "--dir", projDir); err == nil {
		t.Fatalf("substituir already-superseded decision succeeded, want error")
	}
}

func TestDecisionSubstituirFlagRules(t *testing.T) {
	projDir, _, _ := setupProject(t)
	id := newDecision(t, projDir, "ADR A", 1)
	id2 := newDecision(t, projDir, "ADR B", 2)

	if _, err := execCerne(t, "decision", "substituir", id, "--reason", "x", "--dir", projDir); err == nil || !strings.Contains(err.Error(), "--by") {
		t.Fatalf("substituir without --by err = %v, want error mentioning --by", err)
	}
	if _, err := execCerne(t, "decision", "substituir", id, "--by", id2, "--dir", projDir); err == nil || !strings.Contains(err.Error(), "--reason") {
		t.Fatalf("substituir without --reason err = %v, want error mentioning --reason", err)
	}
}
