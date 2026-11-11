package cli

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/store"
)

// seedStatusFeatures saves features in assorted states directly into the
// backend clone and pushes them to origin.
func seedStatusFeatures(t *testing.T, repoDir string) {
	t.Helper()
	now := time.Now().UTC()
	features := []store.Feature{
		{ID: "F-0001", Title: "Login social", Module: "identity", State: domain.StateApproved},
		{ID: "F-0002", Title: "Exportar CSV", Module: "", State: domain.StateCandidate},
		{ID: "F-0003", Title: "Parcelamento de boletos", Module: "payments", State: domain.StateInDevelopment},
		{ID: "F-0004", Title: "Relatório mensal", Module: "core", State: domain.StateImplemented},
		{ID: "F-0005", Title: "Feature antiga", Module: "core", State: domain.StateSuperseded, SupersededBy: "F-0004", Reason: "duplicada"},
		{ID: "F-0006", Title: "Fora de escopo", Module: "core", State: domain.StateRejected, Reason: "fora de escopo"},
	}
	for _, f := range features {
		f.CreatedAt, f.UpdatedAt = now, now
		if err := store.SaveFeature(repoDir, f); err != nil {
			t.Fatalf("SaveFeature %s: %v", f.ID, err)
		}
	}
	if _, err := gitback.CommitAndPush(repoDir, "seed status fixtures"); err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}
}

func TestStatusDefault(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	seedStatusFeatures(t, repoDir)

	out, err := execCerne(t, "status", "--dir", projDir)
	if err != nil {
		t.Fatalf("status: %v\noutput: %s", err, out)
	}

	// Superseded and rejected hidden by default.
	for _, banned := range []string{"F-0005", "F-0006", "SUPERSEDED", "REJECTED"} {
		if strings.Contains(out, banned) {
			t.Fatalf("default output contains %q, want hidden:\n%s", banned, out)
		}
	}

	// Fixed group order with counts.
	headers := []string{"IN DEVELOPMENT (1)", "APPROVED (1)", "CANDIDATES (1)", "IMPLEMENTED (1)"}
	prev := -1
	for _, h := range headers {
		idx := strings.Index(out, h)
		if idx < 0 {
			t.Fatalf("output missing header %q:\n%s", h, out)
		}
		if idx < prev {
			t.Fatalf("header %q out of order:\n%s", h, out)
		}
		prev = idx
	}

	// Module column padded to the longest module (identity/payments = 8).
	lines := []string{
		"  F-0003  payments  Parcelamento de boletos",
		"  F-0001  identity  Login social",
		"  F-0002            Exportar CSV",
		"  F-0004  core      Relatório mensal",
	}
	for _, l := range lines {
		if !strings.Contains(out, l+"\n") {
			t.Fatalf("output missing line %q:\n%s", l, out)
		}
	}

	if !strings.HasSuffix(out, "\nTotal: 4 feature(s)\n") {
		t.Fatalf("output does not end with blank line + total of 4:\n%s", out)
	}
}

func TestStatusAll(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	seedStatusFeatures(t, repoDir)

	out, err := execCerne(t, "status", "--all", "--dir", projDir)
	if err != nil {
		t.Fatalf("status --all: %v\noutput: %s", err, out)
	}

	headers := []string{
		"IN DEVELOPMENT (1)", "APPROVED (1)", "CANDIDATES (1)",
		"IMPLEMENTED (1)", "SUPERSEDED (1)", "REJECTED (1)",
	}
	prev := -1
	for _, h := range headers {
		idx := strings.Index(out, h)
		if idx < 0 {
			t.Fatalf("output missing header %q:\n%s", h, out)
		}
		if idx < prev {
			t.Fatalf("header %q out of order:\n%s", h, out)
		}
		prev = idx
	}

	if !strings.Contains(out, "  F-0005  core      Feature antiga → F-0004\n") {
		t.Fatalf("superseded line missing → F-0004:\n%s", out)
	}
	if !strings.Contains(out, "  F-0006  core      Fora de escopo\n") {
		t.Fatalf("rejected line missing:\n%s", out)
	}
	if !strings.HasSuffix(out, "\nTotal: 6 feature(s)\n") {
		t.Fatalf("output does not end with blank line + total of 6:\n%s", out)
	}
}

func TestStatusJSON(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	seedStatusFeatures(t, repoDir)

	out, err := execCerne(t, "status", "--json", "--dir", projDir)
	if err != nil {
		t.Fatalf("status --json: %v\noutput: %s", err, out)
	}

	var parsed struct {
		Features []store.Feature `json:"features"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, out)
	}
	// All features regardless of state, sorted by ID.
	if len(parsed.Features) != 6 {
		t.Fatalf("features = %d, want 6", len(parsed.Features))
	}
	for i, f := range parsed.Features {
		wantID := []string{"F-0001", "F-0002", "F-0003", "F-0004", "F-0005", "F-0006"}[i]
		if f.ID != wantID {
			t.Fatalf("features[%d].ID = %s, want %s", i, f.ID, wantID)
		}
	}
	f5 := parsed.Features[4]
	if f5.State != domain.StateSuperseded || f5.SupersededBy != "F-0004" || f5.Reason != "duplicada" {
		t.Fatalf("F-0005 = %+v, want superseded by F-0004 with reason", f5)
	}
	if strings.Contains(out, "IN DEVELOPMENT") {
		t.Fatalf("--json output contains table header:\n%s", out)
	}
}

func TestStatusEpics(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	now := time.Now().UTC()
	features := []store.Feature{
		{ID: "F-0001", Title: "Revamp de assinaturas", Module: "billing", State: domain.StateInDevelopment},
		{ID: "F-0002", Title: "Tela de planos", Parent: "F-0001", State: domain.StateImplemented},
		{ID: "F-0003", Title: "Cobrança recorrente", Parent: "F-0001", State: domain.StateApproved},
		{ID: "F-0004", Title: "Feature solta", State: domain.StateCandidate},
	}
	for _, f := range features {
		f.CreatedAt, f.UpdatedAt = now, now
		if err := store.SaveFeature(repoDir, f); err != nil {
			t.Fatalf("SaveFeature %s: %v", f.ID, err)
		}
	}
	if _, err := gitback.CommitAndPush(repoDir, "seed epics"); err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}

	out, err := execCerne(t, "status", "--dir", projDir)
	if err != nil {
		t.Fatalf("status: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "EPICS (1)") {
		t.Fatalf("missing EPICS section:\n%s", out)
	}
	if !strings.Contains(out, "  F-0001  Revamp de assinaturas — 1/2 implemented\n") {
		t.Fatalf("missing epic progress line:\n%s", out)
	}
	for _, l := range []string{
		"    F-0002  [implemented]  Tela de planos\n",
		"    F-0003  [approved]  Cobrança recorrente\n",
	} {
		if !strings.Contains(out, l) {
			t.Fatalf("missing child line %q:\n%s", l, out)
		}
	}
	if strings.Contains(out, "F-0004  Feature solta —") {
		t.Fatalf("non-epic F-0004 listed as epic:\n%s", out)
	}
}

// TestStatusDone pins the F-0131 acceptance state: a done feature gets its own
// DONE group after IMPLEMENTED in the default view, counts in the total, shows
// the [done] short label in the EPICS child list (counting toward the epic
// rollup), and is hidden by --active like the rest of the finished history.
func TestStatusDone(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	now := time.Now().UTC()
	features := []store.Feature{
		{ID: "F-0001", Title: "Epico ativo", State: domain.StateInDevelopment},
		{ID: "F-0002", Title: "Filho aceito", Parent: "F-0001", State: domain.StateDone},
		{ID: "F-0003", Title: "Filho aprovado", Parent: "F-0001", State: domain.StateApproved},
		{ID: "F-0004", Title: "Pronta aguardando aceite", State: domain.StateImplemented},
		{ID: "F-0005", Title: "Aceita solta", State: domain.StateDone},
	}
	for _, f := range features {
		f.CreatedAt, f.UpdatedAt = now, now
		if err := store.SaveFeature(repoDir, f); err != nil {
			t.Fatalf("SaveFeature %s: %v", f.ID, err)
		}
	}
	if _, err := gitback.CommitAndPush(repoDir, "seed done"); err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}

	out, err := execCerne(t, "status", "--dir", projDir)
	if err != nil {
		t.Fatalf("status: %v\noutput: %s", err, out)
	}
	// DONE comes after IMPLEMENTED, both done features counted (child + loose).
	implIdx := strings.Index(out, "IMPLEMENTED (1)")
	doneIdx := strings.Index(out, "DONE (2)")
	if implIdx < 0 || doneIdx < 0 || doneIdx < implIdx {
		t.Fatalf("want DONE (2) after IMPLEMENTED (1):\n%s", out)
	}
	if !strings.Contains(out, "  F-0005  Aceita solta\n") {
		t.Fatalf("done feature row missing:\n%s", out)
	}
	if !strings.Contains(out, "Total: 5 feature(s)\n") {
		t.Fatalf("total should count done features:\n%s", out)
	}
	// Epic rollup counts the accepted child; short label is [done].
	if !strings.Contains(out, "  F-0001  Epico ativo — 1/2 implemented\n") {
		t.Fatalf("epic progress should count done child:\n%s", out)
	}
	if !strings.Contains(out, "    F-0002  [done]  Filho aceito\n") {
		t.Fatalf("epic child missing [done] label:\n%s", out)
	}

	// --active hides DONE together with IMPLEMENTED.
	out, err = execCerne(t, "status", "--active", "--dir", projDir)
	if err != nil {
		t.Fatalf("status --active: %v\noutput: %s", err, out)
	}
	for _, banned := range []string{"DONE", "Aceita solta", "IMPLEMENTED"} {
		if strings.Contains(out, banned) {
			t.Fatalf("--active leaks %q:\n%s", banned, out)
		}
	}
}

func TestStatusEmptyProject(t *testing.T) {
	projDir, _, _ := setupProject(t)

	out, err := execCerne(t, "status", "--dir", projDir)
	if err != nil {
		t.Fatalf("status: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "No features yet. Create the first one with 'cerne feature new'.") {
		t.Fatalf("empty project message missing:\n%s", out)
	}

	out, err = execCerne(t, "status", "--json", "--dir", projDir)
	if err != nil {
		t.Fatalf("status --json: %v\noutput: %s", err, out)
	}
	var parsed struct {
		Features []store.Feature `json:"features"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("empty --json output invalid: %v\noutput: %s", err, out)
	}
	if parsed.Features == nil || len(parsed.Features) != 0 {
		t.Fatalf("features = %v, want present empty array", parsed.Features)
	}
}

// TestStatusActive pins the lean view: --active shows only the active groups
// (+ EPICS), hides IMPLEMENTED and terminals, counts only active features,
// and takes precedence over --all.
func TestStatusActive(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	now := time.Now().UTC()
	features := []store.Feature{
		{ID: "F-0001", Title: "Epico ativo", State: domain.StateInDevelopment},
		{ID: "F-0002", Title: "Filho implementado", Parent: "F-0001", State: domain.StateImplemented},
		{ID: "F-0003", Title: "Filho aprovado", Parent: "F-0001", State: domain.StateApproved},
		{ID: "F-0004", Title: "Candidata solta", State: domain.StateCandidate},
		{ID: "F-0005", Title: "Implementada solta", State: domain.StateImplemented},
		{ID: "F-0006", Title: "Substituida", State: domain.StateSuperseded, SupersededBy: "F-0005"},
	}
	for _, f := range features {
		f.CreatedAt, f.UpdatedAt = now, now
		if err := store.SaveFeature(repoDir, f); err != nil {
			t.Fatalf("SaveFeature %s: %v", f.ID, err)
		}
	}
	if _, err := gitback.CommitAndPush(repoDir, "seed active"); err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}

	out, err := execCerne(t, "status", "--active", "--dir", projDir)
	if err != nil {
		t.Fatalf("status --active: %v\noutput: %s", err, out)
	}
	for _, want := range []string{"IN DEVELOPMENT (1)", "APPROVED (1)", "CANDIDATES (1)", "EPICS (1)"} {
		if !strings.Contains(out, want) {
			t.Errorf("--active missing %q:\n%s", want, out)
		}
	}
	// Implemented + terminal groups and their standalone rows are hidden.
	for _, banned := range []string{"IMPLEMENTED", "SUPERSEDED", "REJECTED", "Implementada solta", "Substituida"} {
		if strings.Contains(out, banned) {
			t.Errorf("--active leaks %q:\n%s", banned, out)
		}
	}
	// Total counts only the 3 active features (in_dev epic + approved child + candidate).
	if !strings.Contains(out, "Total: 3 feature(s)\n") {
		t.Errorf("--active total should be 3:\n%s", out)
	}

	// --active takes precedence over --all.
	out2, err := execCerne(t, "status", "--active", "--all", "--dir", projDir)
	if err != nil {
		t.Fatalf("status --active --all: %v\noutput: %s", err, out2)
	}
	if strings.Contains(out2, "IMPLEMENTED") || strings.Contains(out2, "SUPERSEDED") {
		t.Errorf("--active must override --all:\n%s", out2)
	}
}

// TestStatusEpicsWithTasks pins criterion 6 of F-0203: a legacy F- with
// children (now including T- tasks) keeps acting as a structural epic — no
// behavior derives from the prefix — and T-/E- ids surface in the rollup.
func TestStatusEpicsWithTasks(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	now := time.Now().UTC()
	features := []store.Feature{
		{ID: "F-0001", Title: "PRD legado", State: domain.StateInDevelopment},
		{ID: "T-0001", Title: "Passo um", Parent: "F-0001", State: domain.StateImplemented},
		{ID: "T-0002", Title: "Passo dois", Parent: "F-0001", State: domain.StateApproved},
	}
	for _, f := range features {
		f.CreatedAt, f.UpdatedAt = now, now
		if err := store.SaveFeature(repoDir, f); err != nil {
			t.Fatalf("SaveFeature %s: %v", f.ID, err)
		}
	}
	if _, err := gitback.CommitAndPush(repoDir, "seed tasks"); err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}
	out, err := execCerne(t, "status", "--dir", projDir)
	if err != nil {
		t.Fatalf("status: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "  F-0001  PRD legado — 1/2 implemented\n") {
		t.Fatalf("F- with task children must stay a structural epic:\n%s", out)
	}
	if !strings.Contains(out, "    T-0001  [implemented]  Passo um\n") {
		t.Fatalf("missing task child line:\n%s", out)
	}
}
