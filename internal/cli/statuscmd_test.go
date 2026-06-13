package cli

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/store"
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
	for _, banned := range []string{"F-0005", "F-0006", "SUBSTITUÍDAS", "REJEITADAS"} {
		if strings.Contains(out, banned) {
			t.Fatalf("default output contains %q, want hidden:\n%s", banned, out)
		}
	}

	// Fixed group order with counts.
	headers := []string{"EM DESENVOLVIMENTO (1)", "APROVADAS (1)", "CANDIDATAS (1)", "IMPLEMENTADAS (1)"}
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
		"EM DESENVOLVIMENTO (1)", "APROVADAS (1)", "CANDIDATAS (1)",
		"IMPLEMENTADAS (1)", "SUBSTITUÍDAS (1)", "REJEITADAS (1)",
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
	if strings.Contains(out, "EM DESENVOLVIMENTO") {
		t.Fatalf("--json output contains table header:\n%s", out)
	}
}

func TestStatusEmptyProject(t *testing.T) {
	projDir, _, _ := setupProject(t)

	out, err := execCerne(t, "status", "--dir", projDir)
	if err != nil {
		t.Fatalf("status: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "Nenhuma feature ainda. Crie a primeira com 'cerne feature new'.") {
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
