package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/store"
)

// F-0419 crit. 5: idea migrate rewrites the text-only ideas as title + body,
// --dry-run lists without writing, and a second run says 0.
func TestIdeaMigrateCommand(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	dir := filepath.Join(repoDir, "entities", "ideas")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	raw := `{"id":"I-0001","text":"antiga\nresto","state":"active","created_at":"2026-11-06T10:00:00Z","updated_at":"2026-11-06T10:00:00Z"}`
	if err := os.WriteFile(filepath.Join(dir, "I-0001.json"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := gitback.CommitAndPush(repoDir, "seed legacy"); err != nil {
		t.Fatal(err)
	}

	if out := ideaCmd(t, projDir, "migrate", "--dry-run"); !strings.Contains(out, "1 idea would be migrated") || !strings.Contains(out, "I-0001") {
		t.Fatalf("--dry-run output = %q", out)
	}
	if i, _ := store.LoadIdea(repoDir, "I-0001"); !i.Legacy() {
		t.Fatal("--dry-run migrated the idea")
	}
	if out := ideaCmd(t, projDir, "migrate"); !strings.Contains(out, "1 idea migrated") || !strings.Contains(out, "I-0001") {
		t.Fatalf("migrate output = %q", out)
	}
	if i, _ := store.LoadIdea(repoDir, "I-0001"); i.Legacy() || i.Title != "antiga" || i.Body != "resto" {
		t.Fatalf("idea = %+v, want title antiga, body resto, not legacy", i)
	}
	if out := ideaCmd(t, projDir, "migrate"); strings.TrimSpace(out) != "0 ideas migrated" {
		t.Fatalf("second run output = %q, want 0 ideas migrated", out)
	}
}
