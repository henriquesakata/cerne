package lint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// F-0419 crit. 7: an idea still stored as text only (a backend not yet
// migrated) keeps resolving as a wiki-link target and feeds no rule.
func TestLegacyIdeaStillResolvesWikiLinks(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	f := store.Feature{ID: "F-0001", Title: "Nasceu de ideia", State: domain.StateCandidate, Facets: []string{"code"}, CreatedAt: now, UpdatedAt: now}
	if err := store.SaveFeature(dir, f); err != nil {
		t.Fatal(err)
	}
	ideas := filepath.Join(dir, "entities", "ideas")
	if err := os.MkdirAll(ideas, 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := `{"id":"I-0001","text":"formato antigo\nresto","state":"active","created_at":"2026-11-06T10:00:00Z","updated_at":"2026-11-06T10:00:00Z"}`
	if err := os.WriteFile(filepath.Join(ideas, "I-0001.json"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	writeDoc(t, dir, "content/features/F-0001/spec.md", "---\ncerne_id: F-0001\n---\n\n# Nasceu\n\n**Origem:** ideia [[I-0001]].\n")
	got, err := RunConfig(dir, 14, 8, 0)
	if err != nil {
		t.Fatalf("RunConfig: %v", err)
	}
	for _, fnd := range got {
		if strings.Contains(fnd.Detail, "I-0001") || strings.HasPrefix(fnd.Entity, "I-") {
			t.Fatalf("a legacy idea broke a link or fed a rule: %+v", fnd)
		}
	}
}
