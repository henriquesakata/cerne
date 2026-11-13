package lint

import (
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// TestIdeaLinksResolveAndIdeasFeedNoRule (F-0312 crit. 2 and 6): a promoted
// candidate's spec links back with [[I-NNNN]], which must resolve; the id set
// is real (an unknown I- is still broken); and an idea — even untagged — never
// becomes the subject of any finding (untagged-live included).
func TestIdeaLinksResolveAndIdeasFeedNoRule(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	f := store.Feature{ID: "F-0001", Title: "Nasceu de ideia", State: domain.StateCandidate, Facets: []string{"code"}, CreatedAt: now, UpdatedAt: now}
	if err := store.SaveFeature(dir, f); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveIdea(dir, store.Idea{ID: "I-0001", Text: "sem tag", State: domain.IdeaActive, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	writeDoc(t, dir, "content/features/F-0001/spec.md", "---\ncerne_id: F-0001\n---\n\n# Nasceu\n\n**Origem:** ideia [[I-0001]]; e [[I-0099]].\n")
	got, err := RunConfig(dir, 14, 8, 0)
	if err != nil {
		t.Fatalf("RunConfig: %v", err)
	}
	broken99 := false
	for _, fnd := range got {
		if fnd.Rule == RuleBrokenLink && strings.Contains(fnd.Detail, "wiki-link [[I-0001]]") {
			t.Fatalf("[[I-0001]] must resolve as a wiki-link: %+v", fnd)
		}
		if fnd.Rule == RuleBrokenLink && strings.Contains(fnd.Detail, "I-0099") {
			broken99 = true
		}
		if strings.HasPrefix(fnd.Entity, "I-") || strings.Contains(fnd.File, "entities/ideas/") {
			t.Fatalf("an idea fed a rule: %+v", fnd)
		}
	}
	if !broken99 {
		t.Fatalf("[[I-0099]] (no such idea) must stay a broken-link: %+v", got)
	}
}
