package lint

import (
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// brokenLinkBackend seeds F-0001 (live, owner of the spec) plus the given
// entity ids as candidate features; spec is the body of F-0001's spec.md.
func brokenLinkBackend(t *testing.T, spec string, ids ...string) string {
	t.Helper()
	dir := t.TempDir()
	now := time.Now().UTC()
	for _, id := range append([]string{"F-0001"}, ids...) {
		f := store.Feature{ID: id, Title: "Entidade " + id, State: domain.StateCandidate, CreatedAt: now, UpdatedAt: now}
		if strings.HasPrefix(id, "N-") {
			if err := store.SaveNote(dir, store.Note{ID: id, Title: "Nota " + id, State: domain.NoteActive,
				Facets: []string{"process"}, Components: []string{"cli"}, CreatedAt: now, UpdatedAt: now}); err != nil {
				t.Fatalf("SaveNote: %v", err)
			}
			continue
		}
		if err := store.SaveFeature(dir, f); err != nil {
			t.Fatalf("SaveFeature(%s): %v", id, err)
		}
	}
	writeDoc(t, dir, "content/features/F-0001/spec.md", "---\ncerne_id: F-0001\n---\n\n"+spec)
	return dir
}

func brokenLinks(t *testing.T, dir string) []Finding {
	t.Helper()
	got, err := Run(dir)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return byRule(got, RuleBrokenLink)
}

// F-0352 crit. 1: one finding per occurrence, with the file line (frontmatter
// counted) in Line and in the Detail.
func TestBrokenLinkOneFindingPerOccurrence(t *testing.T) {
	dir := brokenLinkBackend(t, "# Spec\n\nprimeira [[N-9999]]\n\n```\n[[N-9999]]\n```\n\nsegunda [[N-9999]] e [x](./nada.md)\n")
	got := brokenLinks(t, dir)
	if len(got) != 3 {
		t.Fatalf("findings = %+v, want 3 (two wiki + one link)", got)
	}
	want := []struct {
		line   int
		detail string
	}{
		{7, "line 7: broken wiki-link [[N-9999]]"},
		{13, "line 13: broken wiki-link [[N-9999]]"},
		{13, "line 13: broken link ./nada.md"},
	}
	for _, w := range want {
		ok := false
		for _, f := range got {
			if f.Line == w.line && strings.HasPrefix(f.Detail, w.detail) {
				ok = true
			}
		}
		if !ok {
			t.Errorf("missing finding line=%d %q in %+v", w.line, w.detail, got)
		}
	}
}
