package cli

import (
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/store"
)

// TestNoteNewWithTags: --facet/--component are split, normalized and persisted.
func TestNoteNewWithTags(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	out, err := execCerne(t, "note", "new", "Gitea install gotcha",
		"--facet", "Infra,code", "--component", "Gitea,KPack", "--dir", projDir)
	if err != nil {
		t.Fatalf("note new --facet --component: %v\n%s", err, out)
	}
	n, err := store.LoadNote(repoDir, "N-0001")
	if err != nil {
		t.Fatalf("LoadNote: %v", err)
	}
	if len(n.Facets) != 2 || n.Facets[0] != "infra" || n.Facets[1] != "code" {
		t.Fatalf("facets = %v, want [infra code]", n.Facets)
	}
	if len(n.Components) != 2 || n.Components[0] != "gitea" || n.Components[1] != "kpack" {
		t.Fatalf("components = %v, want [gitea kpack]", n.Components)
	}
}

// TestNoteNewInvalidFacet: a facet outside the closed set fails the command and
// persists nothing.
func TestNoteNewInvalidFacet(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	out, err := execCerne(t, "note", "new", "T", "--facet", "prod", "--dir", projDir)
	if err == nil {
		t.Fatalf("note new --facet prod should fail, got:\n%s", out)
	}
	if !strings.Contains(out, "facet") {
		t.Fatalf("error output should name the offending facet:\n%s", out)
	}
	if notes, _ := store.LoadAllNotes(repoDir); len(notes) != 0 {
		t.Fatalf("invalid facet persisted %d note(s), want 0", len(notes))
	}
}

// TestNoteListFilterByTag: --facet/--component filter the listing (AND when
// combined) and the rendered line shows the tags inline.
func TestNoteListFilterByTag(t *testing.T) {
	projDir, _, _ := setupProject(t)
	mk := func(title, facet, component string) {
		t.Helper()
		if _, err := execCerne(t, "note", "new", title, "--facet", facet, "--component", component, "--dir", projDir); err != nil {
			t.Fatalf("note new %q: %v", title, err)
		}
	}
	mk("Infra Gitea", "infra", "gitea")  // N-0001
	mk("Code Gitea", "code", "gitea")    // N-0002
	mk("Infra Kpack", "infra", "kpack")  // N-0003

	list := func(args ...string) string {
		t.Helper()
		full := append([]string{"note", "list"}, args...)
		full = append(full, "--dir", projDir)
		out, err := execCerne(t, full...)
		if err != nil {
			t.Fatalf("note list %v: %v", args, err)
		}
		return out
	}

	if byFacet := list("--facet", "infra"); !strings.Contains(byFacet, "N-0001") ||
		!strings.Contains(byFacet, "N-0003") || strings.Contains(byFacet, "N-0002") {
		t.Fatalf("--facet infra = %q, want N-0001+N-0003 only", byFacet)
	}
	if byComp := list("--component", "gitea"); !strings.Contains(byComp, "N-0001") ||
		!strings.Contains(byComp, "N-0002") || strings.Contains(byComp, "N-0003") {
		t.Fatalf("--component gitea = %q, want N-0001+N-0002 only", byComp)
	}
	both := list("--facet", "infra", "--component", "gitea")
	if !strings.Contains(both, "N-0001") || strings.Contains(both, "N-0002") || strings.Contains(both, "N-0003") {
		t.Fatalf("--facet infra --component gitea = %q, want N-0001 only", both)
	}
	if !strings.Contains(both, "facet infra") || !strings.Contains(both, "comp gitea") {
		t.Fatalf("list line should render tags inline:\n%s", both)
	}
}

// TestNoteShowRendersTags: note show prints facets/components.
func TestNoteShowRendersTags(t *testing.T) {
	projDir, _, _ := setupProject(t)
	if _, err := execCerne(t, "note", "new", "Gitea gotcha", "--facet", "infra", "--component", "gitea", "--dir", projDir); err != nil {
		t.Fatalf("note new: %v", err)
	}
	out, err := execCerne(t, "note", "show", "N-0001", "--dir", projDir)
	if err != nil {
		t.Fatalf("note show: %v\n%s", err, out)
	}
	if !strings.Contains(out, "facets: infra") || !strings.Contains(out, "components: gitea") {
		t.Fatalf("note show should render tags:\n%s", out)
	}
}
