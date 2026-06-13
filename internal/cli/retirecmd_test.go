package cli

import (
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/project"
	"gitlab.com/rockt/cerne/internal/store"
)

func TestRetire(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	lockBefore, err := project.LoadLock(projDir)
	if err != nil {
		t.Fatalf("LoadLock before: %v", err)
	}

	out, err := execCerne(t, "retire", "Bills",
		"--by", "Finance", "--reason", "absorvido pelo Finance",
		"--alias", "bills", "--alias", "boletos", "--dir", projDir)
	if err != nil {
		t.Fatalf("retire: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "H-0001 registrada") || !strings.Contains(out, "Finance") {
		t.Fatalf("output missing confirmation:\n%s", out)
	}

	h, err := store.LoadHistorical(repoDir, "H-0001")
	if err != nil {
		t.Fatalf("LoadHistorical: %v", err)
	}
	if h.Name != "Bills" || h.SupersededBy != "Finance" || h.State != domain.StateSuperseded {
		t.Fatalf("historical = %+v", h)
	}
	if len(h.Aliases) != 2 || h.Aliases[0] != "bills" || h.Aliases[1] != "boletos" {
		t.Fatalf("aliases = %v", h.Aliases)
	}

	events, err := store.ReadEvents(repoDir)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	if len(events) != 1 || events[0].Type != "historical.recorded" || events[0].EntityID != "H-0001" {
		t.Fatalf("events = %+v, want historical.recorded on H-0001", events)
	}

	lockAfter, err := project.LoadLock(projDir)
	if err != nil {
		t.Fatalf("LoadLock after: %v", err)
	}
	if lockAfter.Snapshot == lockBefore.Snapshot {
		t.Fatalf("lock.Snapshot unchanged, want new backend HEAD")
	}
}

func TestRetireRequiresReason(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	if _, err := execCerne(t, "retire", "Bills", "--by", "Finance", "--dir", projDir); err == nil {
		t.Fatalf("retire without --reason succeeded, want error")
	}
	if _, err := store.LoadHistorical(repoDir, "H-0001"); err == nil {
		t.Fatalf("historical persisted despite missing --reason")
	}
}

func TestRetireRejectsShortAlias(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	_, err := execCerne(t, "retire", "Bills",
		"--by", "Finance", "--reason", "x", "--alias", "abc", "--dir", projDir)
	if err == nil {
		t.Fatalf("retire with short alias succeeded, want error")
	}
	if _, err := store.LoadHistorical(repoDir, "H-0001"); err == nil {
		t.Fatalf("historical persisted despite short alias")
	}
}
