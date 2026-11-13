package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStagedLintRootOverlaysStagedDocs(t *testing.T) {
	projDir, _, repoDir, specPath := pulledProject(t)
	// Edit the spec locally without pushing.
	edited := "---\ncerne_id: F-0001\n---\n\n# Parcelamento\n\nEDITADO localmente.\n"
	mustWrite(t, specPath, edited)

	root, _, cleanup, err := stagedLintRoot(repoDir, projDir, false)
	if err != nil {
		t.Fatalf("stagedLintRoot: %v", err)
	}
	defer cleanup()

	// The staged edit overlays the backend content path...
	got, err := os.ReadFile(filepath.Join(root, "content", "features", "F-0001", "spec.md"))
	if err != nil || string(got) != edited {
		t.Fatalf("overlay spec = %q (err %v); want the edited content", string(got), err)
	}
	// ...while entity state came from the snapshot copy.
	if _, err := os.Stat(filepath.Join(root, "entities", "features", "F-0001.json")); err != nil {
		t.Fatalf("entity json not copied from snapshot: %v", err)
	}
}
