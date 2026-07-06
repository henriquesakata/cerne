package lint

import (
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

// TestStaleWorkFindings: an active work whose UpdatedAt is older than the
// window (hours) is flagged; a fresh active work and an old but done work are
// left alone.
func TestStaleWorkFindings(t *testing.T) {
	now := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	stale := now.Add(-10 * time.Hour)
	fresh := now.Add(-2 * time.Hour)

	works := []store.Work{
		{ID: "W-0001", Title: "Investigação antiga", State: domain.WorkActive, Actor: "a@b", UpdatedAt: stale},
		{ID: "W-0002", Title: "Investigação recente", State: domain.WorkActive, Actor: "a@b", UpdatedAt: fresh},
		{ID: "W-0003", Title: "Já encerrada há muito", State: domain.WorkDone, Actor: "a@b", UpdatedAt: stale},
	}

	got := staleWorkFindings(works, 8, now)
	if len(got) != 1 {
		t.Fatalf("got %d findings, want 1: %+v", len(got), got)
	}
	if got[0].Rule != RuleStaleWork || got[0].Entity != "W-0001" {
		t.Fatalf("finding = %+v, want stale-work for W-0001", got[0])
	}
}

// TestStaleWorkFindingsDefaultWindow: hours<=0 falls back to the 8-hour
// default, matching project.Config.StaleWorkWindowHours.
func TestStaleWorkFindingsDefaultWindow(t *testing.T) {
	now := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	justOver8h := now.Add(-9 * time.Hour)
	works := []store.Work{
		{ID: "W-0001", Title: "Investigação", State: domain.WorkActive, Actor: "a@b", UpdatedAt: justOver8h},
	}
	got := staleWorkFindings(works, 0, now)
	if len(got) != 1 || got[0].Entity != "W-0001" {
		t.Fatalf("staleWorkFindings with hours=0 = %+v, want one finding for W-0001 (8h default)", got)
	}
}

// TestRunConfigFlagsStaleWork proves the rule is wired into RunConfig: an old
// active work surfaces exactly one stale-work finding.
func TestRunConfigFlagsStaleWork(t *testing.T) {
	dir := t.TempDir()
	ancient := time.Now().UTC().Add(-20 * time.Hour)
	w := store.Work{ID: "W-0001", Title: "Investigação", State: domain.WorkActive, Actor: "a@b", CreatedAt: ancient, UpdatedAt: ancient}
	if err := store.SaveWork(dir, w); err != nil {
		t.Fatalf("SaveWork: %v", err)
	}

	got, err := RunConfig(dir, 14, 8)
	if err != nil {
		t.Fatalf("RunConfig: %v", err)
	}
	var stale []Finding
	for _, fnd := range got {
		if fnd.Rule == RuleStaleWork {
			stale = append(stale, fnd)
		}
	}
	if len(stale) != 1 || stale[0].Entity != "W-0001" {
		t.Fatalf("stale-work findings = %+v, want exactly one for W-0001", stale)
	}
}
