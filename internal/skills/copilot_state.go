package skills

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/henriquesakata/cerne/internal/hooks"
)

// copilotRel returns the project-relative skill dir and instructions path.
func copilotRel() (skillDir, instructions string) {
	t := targets[hooks.ProviderCopilot]
	return t.skillsDir + "/" + guideName, t.instructions
}

// CopilotSkillState compares the authoring skill under .github/skills with the
// embed: SKILL.md and every reference (hooks.FileOK / FileDivergent /
// FileMissing).
func CopilotSkillState(projDir string) string {
	rel, _ := copilotRel()
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

// CopilotInstructionsState compares .github/instructions/cerne.instructions.md
// with its render.
func CopilotInstructionsState(projDir string) string {
	rel, ins := copilotRel()
	path := filepath.Join(projDir, filepath.FromSlash(ins))
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return hooks.FileMissing
	}
	if !sameFile(path, copilotInstructions(rel+"/SKILL.md")) {
		return hooks.FileDivergent
	}
	return hooks.FileOK
}
