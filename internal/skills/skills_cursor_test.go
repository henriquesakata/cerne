package skills

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/henriquesakata/cerne/internal/hooks"
)

// TestCursorSkillState (F-0526 crit. 3): missing/ok/divergent for
// .cursor/skills/cerne-authoring/SKILL.md.
func TestCursorSkillState(t *testing.T) { // F-0526 crit. 3
	proj := t.TempDir()

	if CursorSkillState(proj) != hooks.FileMissing {
		t.Error("absent .cursor/skills/cerne-authoring = want missing")
	}

	// Install via skills.Install → ok.
	if _, err := Install(proj, hooks.ProviderCursor); err != nil {
		t.Fatal(err)
	}
	if CursorSkillState(proj) != hooks.FileOK {
		t.Error("installed cursor skill = want ok")
	}

	// Corrupt SKILL.md → divergent.
	dst := filepath.Join(proj, ".cursor", "skills", "cerne-authoring", "SKILL.md")
	if err := os.WriteFile(dst, []byte("stale content"), 0o644); err != nil {
		t.Fatal(err)
	}
	if CursorSkillState(proj) != hooks.FileDivergent {
		t.Error("corrupted cursor skill SKILL.md = want divergent")
	}
}
