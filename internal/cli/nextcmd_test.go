package cli

import (
	"strings"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/store"
)

// TestNext pins the work queue: active features grouped by the action that
// advances them, lint debts in their own section, terminal/implemented work
// hidden, and exit 0 even with findings (it is informational, not a gate).
func TestNext(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	now := time.Now().UTC()
	features := []store.Feature{
		{ID: "F-0001", Title: "Em dev", Module: "core", State: domain.StateInDevelopment},
		{ID: "F-0002", Title: "Aprovada", State: domain.StateApproved},
		{ID: "F-0003", Title: "Candidata", State: domain.StateCandidate},
		{ID: "F-0004", Title: "Feature encerrada", State: domain.StateImplemented},
		{ID: "F-0005", Title: "Feature antiga", State: domain.StateSuperseded, SupersededBy: "F-0004"},
	}
	for _, f := range features {
		f.CreatedAt, f.UpdatedAt = now, now
		if err := store.SaveFeature(repoDir, f); err != nil {
			t.Fatalf("SaveFeature %s: %v", f.ID, err)
		}
	}
	if _, err := gitback.CommitAndPush(repoDir, "seed next"); err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}

	// No docs were written, so the gated features trip missing-gate-doc — and
	// next must still exit 0 (informational), unlike cerne lint.
	out, err := execCerne(t, "next", "--dir", projDir)
	if err != nil {
		t.Fatalf("next must not fail with findings: %v\noutput: %s", err, out)
	}
	for _, want := range []string{
		"IN PROGRESS (1) — finish",
		"READY TO START (1) — run 'cerne feature start'",
		// F-0308: rehearse with --check; the approve itself is a human act.
		"CANDIDATES (1) — write the spec, run 'cerne feature approve <id> --check', then ask a human to approve (a human act)",
		"DEBTS (cerne lint):",
		"MISSING-GATE-DOC",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("next missing %q:\n%s", want, out)
		}
	}
	// Implemented and terminal features are not in the active queue (titles).
	for _, banned := range []string{"Feature encerrada", "Feature antiga"} {
		if strings.Contains(out, banned) {
			t.Errorf("next leaks non-active feature %q:\n%s", banned, out)
		}
	}
}

// TestNextListsEpicsReadyToClose pins crit.2: an in_development epic whose
// children are all concluded (implemented) or dead (rejected) surfaces in its
// own block; an epic with a live child pending does not.
func TestNextListsEpicsReadyToClose(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	now := time.Now().UTC()
	features := []store.Feature{
		{ID: "F-0100", Title: "Épico pronto", State: domain.StateInDevelopment},
		{ID: "F-0101", Title: "Filha concluída", Parent: "F-0100", State: domain.StateImplemented},
		{ID: "F-0102", Title: "Filha rejeitada", Parent: "F-0100", State: domain.StateRejected, Reason: "não necessária"},
		{ID: "F-0200", Title: "Épico ainda em curso", State: domain.StateInDevelopment},
		{ID: "F-0201", Title: "Filha viva", Parent: "F-0200", State: domain.StateInDevelopment},
	}
	for _, f := range features {
		f.CreatedAt, f.UpdatedAt = now, now
		if err := store.SaveFeature(repoDir, f); err != nil {
			t.Fatalf("SaveFeature %s: %v", f.ID, err)
		}
	}
	if _, err := gitback.CommitAndPush(repoDir, "seed next epics"); err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}

	out, err := execCerne(t, "next", "--dir", projDir)
	if err != nil {
		t.Fatalf("next: %v\noutput: %s", err, out)
	}
	i := strings.Index(out, "EPICS READY TO CLOSE (1)")
	if i < 0 {
		t.Fatalf("next missing epics-ready-to-close block:\n%s", out)
	}
	block := out[i:]
	if j := strings.Index(block, "\n\n"); j >= 0 {
		block = block[:j]
	}
	if !strings.Contains(block, "Épico pronto") {
		t.Errorf("epics-ready-to-close block missing F-0100:\n%s", block)
	}
	if strings.Contains(block, "Épico ainda em curso") {
		t.Errorf("epics-ready-to-close block lists epic with a live child pending:\n%s", block)
	}
}

// TestNextEmpty: a project with no active work and no debts says so, exit 0.
func TestNextEmpty(t *testing.T) {
	projDir, _, _ := setupProject(t)
	out, err := execCerne(t, "next", "--dir", projDir)
	if err != nil {
		t.Fatalf("next: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "Nothing pending. ✓") {
		t.Fatalf("empty project should report nothing pending:\n%s", out)
	}
}
