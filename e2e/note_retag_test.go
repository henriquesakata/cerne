package e2e_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/gitback"
)

// TestNoteRetagGoldenPath: an untagged note gets tags via `note retag`, then
// surfaces under `note list --facet` — the backfill path for existing notes.
func TestNoteRetagGoldenPath(t *testing.T) {
	t.Setenv("CERNE_CACHE_DIR", t.TempDir())
	origin := filepath.Join(t.TempDir(), "origin.git")
	if err := gitback.InitBare(origin); err != nil {
		t.Fatalf("InitBare: %v", err)
	}
	proj := t.TempDir()
	mustCerne(t, "init", "--docs-language", "pt-BR", "--name", "Demo", "--backend", origin, "--module", "arch", "--dir", proj)

	mustCerne(t, "note", "new", "Procedimento de setup", "--dir", proj) // N-0001, no tags
	if list := mustCerne(t, "note", "list", "--facet", "infra", "--dir", proj); strings.Contains(list, "N-0001") {
		t.Fatalf("untagged note should not match --facet infra:\n%s", list)
	}

	mustCerne(t, "note", "retag", "N-0001", "--facet", "infra", "--component", "gitea", "--dir", proj)

	list := mustCerne(t, "note", "list", "--facet", "infra", "--dir", proj)
	if !strings.Contains(list, "N-0001") || !strings.Contains(list, "comp gitea") {
		t.Fatalf("retagged note missing from --facet infra or tags not rendered:\n%s", list)
	}
}
