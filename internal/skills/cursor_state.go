package skills

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/henriquesakata/cerne/internal/hooks"
)

// CursorSkillState compares the authoring skill under .cursor/skills with the
// embed: SKILL.md and every reference (hooks.FileOK / FileDivergent / FileMissing).
func CursorSkillState(projDir string) string {
	t := targets[hooks.ProviderCursor]
	rel := t.skillsDir + "/" + guideName
	s := authoringSkill()
	base := filepath.Join(projDir, filepath.FromSlash(rel))
	if _, err := os.Stat(filepath.Join(base, "SKILL.md")); errors.Is(err, fs.ErrNotExist) {
		return hooks.FileMissing
	}
	if !sameFile(filepath.Join(base, "SKILL.md"), s.Content) {
		return hooks.FileDivergent
	}
	for _, r := range s.References {
		if !sameFile(filepath.Join(base, filepath.FromSlash(r.RelPath)), r.Content) {
			return hooks.FileDivergent
		}
	}
	return hooks.FileOK
}
