package cli

import (
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/store"
)

func TestFeatureRename(t *testing.T) {
	projDir, _, repoDir := setupProject(t)

	if out, err := execCerne(t, "feature", "new", "Título antigo", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v\n%s", err, out)
	}

	out, err := execCerne(t, "feature", "rename", "F-0001", "Título novo", "--dir", projDir)
	if err != nil {
		t.Fatalf("rename: %v\n%s", err, out)
	}
	if !strings.Contains(out, "F-0001 renomeada") {
		t.Fatalf("output missing confirmation:\n%s", out)
	}
	if f, _ := store.LoadFeature(repoDir, "F-0001"); f.Title != "Título novo" {
		t.Fatalf("title = %q, want 'Título novo'", f.Title)
	}

	// Wrong arity is rejected.
	if _, err := execCerne(t, "feature", "rename", "F-0001", "--dir", projDir); err == nil {
		t.Fatalf("rename without new title succeeded, want error")
	}
}
