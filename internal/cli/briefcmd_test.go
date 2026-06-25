package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// seedBriefEntities creates one feature, one decision and one note (with a body)
// so brief has something to map. Returns the project dir.
func seedBriefEntities(t *testing.T, projDir string) {
	t.Helper()
	if _, err := execCerne(t, "feature", "new", "Conta digital", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v", err)
	}
	if _, err := execCerne(t, "decision", "new", "Usar Postgres", "--dir", projDir); err != nil {
		t.Fatalf("decision new: %v", err)
	}
	bodyFile := filepath.Join(t.TempDir(), "note.md")
	if err := os.WriteFile(bodyFile, []byte("O índice é cache derivado.\n"), 0o644); err != nil {
		t.Fatalf("write note body: %v", err)
	}
	if _, err := execCerne(t, "note", "new", "SSH gotcha", "--file", bodyFile, "--dir", projDir); err != nil {
		t.Fatalf("note new: %v", err)
	}
}

// TestBrief: the cheap project map prints id + state + title for every entity,
// grouped by kind, and NO file paths — the path is reconstructible from the id,
// so brief omits it to stay token-lean (its whole reason to exist next to the
// path-heavy CONTEXT.md).
func TestBrief(t *testing.T) {
	projDir, _, _ := setupProject(t)
	seedBriefEntities(t, projDir)

	out, err := execCerne(t, "brief", "--dir", projDir)
	if err != nil {
		t.Fatalf("brief: %v\noutput: %s", err, out)
	}
	for _, want := range []string{
		"FEATURES", "F-0001", "candidate", "Conta digital",
		"DECISÕES", "D-0001", "active", "Usar Postgres",
		"NOTAS", "N-0001", "SSH gotcha",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("brief output missing %q:\n%s", want, out)
		}
	}
	// The cheap map carries no paths: not the materialized slug paths, not even
	// a ".md" — those are what bloat CONTEXT.md and are reconstructible from ids.
	if strings.Contains(out, "current/") || strings.Contains(out, ".md") {
		t.Fatalf("brief must not print file paths:\n%s", out)
	}
}

// TestBriefJSON: --json emits the same map as a parseable object keyed by kind.
func TestBriefJSON(t *testing.T) {
	projDir, _, _ := setupProject(t)
	seedBriefEntities(t, projDir)

	out, err := execCerne(t, "brief", "--json", "--dir", projDir)
	if err != nil {
		t.Fatalf("brief --json: %v\noutput: %s", err, out)
	}
	var data struct {
		Features  []struct{ ID, State, Title string } `json:"features"`
		Decisions []struct{ ID, State, Title string } `json:"decisions"`
		Notes     []struct{ ID, State, Title string } `json:"notes"`
	}
	if err := json.Unmarshal([]byte(out), &data); err != nil {
		t.Fatalf("brief --json is not valid JSON: %v\n%s", err, out)
	}
	if len(data.Features) != 1 || data.Features[0].ID != "F-0001" || data.Features[0].State != "candidate" {
		t.Fatalf("brief --json features = %+v, want one F-0001 candidate", data.Features)
	}
	if len(data.Decisions) != 1 || data.Decisions[0].ID != "D-0001" {
		t.Fatalf("brief --json decisions = %+v, want one D-0001", data.Decisions)
	}
	if len(data.Notes) != 1 || data.Notes[0].Title != "SSH gotcha" {
		t.Fatalf("brief --json notes = %+v, want one 'SSH gotcha'", data.Notes)
	}
}
