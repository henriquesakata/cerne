package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/cost"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/store"
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
	// F-0371: an implemented feature owes the agent its delivery; a terminal one is not queued.
	if !strings.Contains(out, "FINISH DELIVERY (1)") || !strings.Contains(out, "cerne feature deliver F-0004 --mr <url>") {
		t.Errorf("next missing the implemented feature's delivery step:\n%s", out)
	}
	for _, banned := range []string{"Feature antiga"} {
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

// nextAfterCompleteProject seeds the implemented features the queue follows
// past complete (F-0371): which command is still missing for each.
func nextAfterCompleteProject(t *testing.T) string {
	t.Helper()
	projDir, _, repoDir := setupProject(t)
	now := time.Now().UTC()
	stamped, err := cost.Stamp("# Resultado\n", cost.Report{ID: "F-0012"})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []store.Feature{
		{ID: "F-0010", Title: "Sem custo e sem entrega", State: domain.StateImplemented},
		{ID: "F-0011", Title: "Sem vinculo e sem entrega", State: domain.StateImplemented},
		{ID: "F-0012", Title: "Completa", State: domain.StateImplemented, DeliveryCommitURL: "https://example.com/c/1"},
		{ID: "F-0013", Title: "Ja aceita", State: domain.StateDone},
		{ID: "F-0014", Title: "Sem custo, entregue por MR", State: domain.StateImplemented, DeliveryMRURL: "https://example.com/mr/1"},
	} {
		f.CreatedAt, f.UpdatedAt = now, now
		if err := store.SaveFeature(repoDir, f); err != nil {
			t.Fatalf("SaveFeature %s: %v", f.ID, err)
		}
	}
	dir := filepath.Join(repoDir, "content", "features", "F-0012")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "result.md"), []byte(stamped), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"F-0010", "F-0012", "F-0014"} {
		costBind(t, filepath.Dir(repoDir), "sess-"+id, id, "2026-11-01T10:00:00Z")
	}
	if _, err := gitback.CommitAndPush(repoDir, "seed next after complete"); err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}
	return projDir
}

// nextBlock is the text block that starts at header, up to the blank line.
func nextBlock(out, header string) string {
	i := strings.Index(out, header)
	if i < 0 {
		return ""
	}
	b := out[i:]
	if j := strings.Index(b, "\n\n"); j >= 0 {
		b = b[:j]
	}
	return b
}

// TestNextFinishDelivery pins F-0371 crit.1-2, 5: an implemented feature
// missing its cost stamp (only with a session bound) or its delivery gets the
// exact missing command; a complete or done one is not in the group.
func TestNextFinishDelivery(t *testing.T) {
	projDir := nextAfterCompleteProject(t)
	out, err := execCerne(t, "next", "--dir", projDir)
	if err != nil {
		t.Fatalf("next: %v\n%s", err, out)
	}
	blk := nextBlock(out, "FINISH DELIVERY (3)")
	if blk == "" {
		t.Fatalf("missing FINISH DELIVERY (3) group:\n%s", out)
	}
	for _, want := range []string{
		"cerne cost F-0010 --stamp", "cerne feature deliver F-0010 --mr <url>",
		"cerne feature deliver F-0011 --mr <url>",
		"cerne cost F-0014 --stamp",
	} {
		if !strings.Contains(blk, want) {
			t.Errorf("group missing %q:\n%s", want, blk)
		}
	}
	for _, banned := range []string{"cerne cost F-0011", "cerne feature deliver F-0014", "F-0012", "F-0013"} {
		if strings.Contains(blk, banned) {
			t.Errorf("group must not contain %q:\n%s", banned, blk)
		}
	}
	if strings.Index(out, "FINISH DELIVERY") > strings.Index(out, "DEBTS") && strings.Contains(out, "DEBTS") {
		t.Errorf("FINISH DELIVERY must precede the debts:\n%s", out)
	}

	js, err := execCerne(t, "next", "--dir", projDir, "--json")
	if err != nil {
		t.Fatalf("next --json: %v", err)
	}
	var q nextJSON
	if err := json.Unmarshal([]byte(js), &q); err != nil {
		t.Fatalf("json: %v\n%s", err, js)
	}
	for _, g := range q.Groups {
		if g.Queue != "finish_delivery" {
			continue
		}
		if g.State != domain.StateImplemented || len(g.Features) != 3 || len(g.Features[0].Commands) != 2 {
			t.Errorf("finish_delivery group = %+v", g)
		}
		return
	}
	t.Errorf("no finish_delivery group in json:\n%s", js)
}

// TestNextAwaitingAccept pins F-0371 crit.3, 5: an implemented feature with cost
// (or nothing to stamp) and delivery waits on a human, with no command for the
// agent; the group sits after CANDIDATES and before the debts.
func TestNextAwaitingAccept(t *testing.T) {
	projDir := nextAfterCompleteProject(t)
	out, err := execCerne(t, "next", "--dir", projDir)
	if err != nil {
		t.Fatalf("next: %v\n%s", err, out)
	}
	blk := nextBlock(out, "WAITING FOR HUMAN ACCEPT (1)")
	if blk == "" {
		t.Fatalf("missing WAITING FOR HUMAN ACCEPT (1) group:\n%s", out)
	}
	if !strings.Contains(blk, "ask a human to run 'cerne feature accept <id>'") || !strings.Contains(blk, "F-0012") {
		t.Errorf("group = %s", blk)
	}
	if strings.Contains(blk, "F-0010") || strings.Contains(blk, "F-0013") || strings.Contains(blk, "\n    cerne ") {
		t.Errorf("group must hold only the complete feature, with no agent command:\n%s", blk)
	}
	if i := strings.Index(out, "WAITING FOR HUMAN ACCEPT"); i < strings.Index(out, "FINISH DELIVERY") {
		t.Errorf("WAITING FOR HUMAN ACCEPT must follow FINISH DELIVERY:\n%s", out)
	}

	js, err := execCerne(t, "next", "--dir", projDir, "--json")
	if err != nil {
		t.Fatalf("next --json: %v", err)
	}
	var q nextJSON
	if err := json.Unmarshal([]byte(js), &q); err != nil {
		t.Fatalf("json: %v", err)
	}
	for _, g := range q.Groups {
		if g.Queue == "awaiting_accept" {
			if len(g.Features) != 1 || g.Features[0].ID != "F-0012" || g.State != domain.StateImplemented {
				t.Errorf("awaiting_accept group = %+v", g)
			}
			return
		}
	}
	t.Errorf("no awaiting_accept group in json:\n%s", js)
}
