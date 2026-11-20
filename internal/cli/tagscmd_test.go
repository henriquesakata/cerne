package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

// E-0005 criterion 21: cerne tags lists the components in use, each with the
// count of entities per kind, as text and as --json.
func TestTagsListsComponentVocabulary(t *testing.T) {
	projDir, _, _ := setupProject(t)
	for _, args := range [][]string{
		{"note", "new", "Seed vocabulary", "--component", "gitea,kpack"},
		{"decision", "new", "Uma decisao de forja", "--component", "gitea"},
		{"feature", "new", "Alpha probe", "--context", "why", "--component", "gitea"},
	} {
		if out, err := execCerne(t, append(args, "--dir", projDir)...); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}
	out, err := execCerne(t, "tags", "--dir", projDir)
	if err != nil {
		t.Fatalf("tags: %v\n%s", err, out)
	}
	for _, want := range []string{"  gitea (3): decision 1, feature 1, note 1\n", "  kpack (1): note 1\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("tags output missing %q:\n%s", want, out)
		}
	}
	js, err := execCerne(t, "tags", "--json", "--dir", projDir)
	if err != nil {
		t.Fatalf("tags --json: %v\n%s", err, js)
	}
	var got struct {
		Components map[string]map[string]int `json:"components"`
	}
	if err := json.Unmarshal([]byte(js), &got); err != nil {
		t.Fatalf("tags --json: %v\n%s", err, js)
	}
	if got.Components["gitea"]["feature"] != 1 || got.Components["kpack"]["note"] != 1 || len(got.Components) != 2 {
		t.Fatalf("tags --json = %+v", got.Components)
	}
}

func TestTagsEmptyBackend(t *testing.T) {
	projDir, _, _ := setupProject(t)
	out, err := execCerne(t, "tags", "--dir", projDir)
	if err != nil || !strings.Contains(out, "No components in use") {
		t.Fatalf("tags on an empty backend: %v\n%s", err, out)
	}
}
