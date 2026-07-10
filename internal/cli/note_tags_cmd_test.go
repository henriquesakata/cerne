package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestNoteTags: 'cerne note tags' surfaces the tag catalog (each distinct
// facet/component across active notes, with counts) — the menu an agent reads
// to self-select which knowledge to pull.
func TestNoteTags(t *testing.T) {
	projDir, _, _ := setupProject(t)

	mustNew := func(title, facet, component string) {
		t.Helper()
		args := []string{"note", "new", title, "--dir", projDir}
		if facet != "" {
			args = append(args, "--facet", facet)
		}
		if component != "" {
			args = append(args, "--component", component)
		}
		if out, err := execCerne(t, args...); err != nil {
			t.Fatalf("note new %q: %v\noutput: %s", title, err, out)
		}
	}
	mustNew("A", "infra", "gitea")
	mustNew("B", "infra,code", "gitea,kpack")
	mustNew("C", "", "convencao")

	out, err := execCerne(t, "note", "tags", "--dir", projDir)
	if err != nil {
		t.Fatalf("note tags: %v\noutput: %s", err, out)
	}
	for _, want := range []string{"infra (2)", "code (1)", "gitea (2)", "kpack (1)", "convencao (1)"} {
		if !strings.Contains(out, want) {
			t.Errorf("note tags output missing %q:\n%s", want, out)
		}
	}
}

// TestNoteTagsJSON: --json emits {facets: {...}, components: {...}} with counts.
func TestNoteTagsJSON(t *testing.T) {
	projDir, _, _ := setupProject(t)

	if out, err := execCerne(t, "note", "new", "A", "--facet", "infra", "--component", "convencao", "--dir", projDir); err != nil {
		t.Fatalf("note new: %v\noutput: %s", err, out)
	}

	out, err := execCerne(t, "note", "tags", "--json", "--dir", projDir)
	if err != nil {
		t.Fatalf("note tags --json: %v\noutput: %s", err, out)
	}
	var got struct {
		Facets     map[string]int `json:"facets"`
		Components map[string]int `json:"components"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	if got.Facets["infra"] != 1 || got.Components["convencao"] != 1 {
		t.Fatalf("catalog = %+v, want infra:1 convencao:1", got)
	}
}

// TestNoteTagsEmpty: with no notes the command still succeeds and says so.
func TestNoteTagsEmpty(t *testing.T) {
	projDir, _, _ := setupProject(t)
	out, err := execCerne(t, "note", "tags", "--dir", projDir)
	if err != nil {
		t.Fatalf("note tags: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "No tags") {
		t.Fatalf("empty catalog should say 'No tags…':\n%s", out)
	}
}
