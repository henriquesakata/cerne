package app

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/store"
)

// ErrInvalidProjectName is wrapped with the violated rule when a rename is refused.
var ErrInvalidProjectName = errors.New("invalid project name")

// maxProjectNameRunes is the same ceiling as channel.human_name (F-0325).
const maxProjectNameRunes = 64

// The name rules, mirrored by ui/src/projectName.ts and kept in step by the
// shared testdata/project_names.json corpus:
//   - trim: Unicode White_Space plus the BOM (the UI: JS \s plus NEL);
//   - visible: at least one letter, number, punctuation or symbol (emoji
//     included), so a zero-width-only name is empty;
//   - breaking: a control character, a line or paragraph separator, or a
//     bidi control — each forges structure in CONTEXT.md and prompts, or
//     reorders what the selector shows.
func nameSpace(r rune) bool { return unicode.IsSpace(r) || r == '\uFEFF' }
func nameVisible(r rune) bool {
	return unicode.In(r, unicode.L, unicode.N, unicode.P, unicode.S)
}
func nameBreaking(r rune) bool {
	return unicode.IsControl(r) || unicode.In(r, unicode.Zl, unicode.Zp, unicode.Bidi_Control)
}

// RenameProject sets the backend's project.json name to the trimmed name, then
// commits, pushes and refreshes the docs.lock snapshot (F-0331). The clone is
// first fast-forwarded to origin, so a name already current on the backend
// commits nothing. The project's identity (project_id, pid, cache path, entity
// ids) is untouched. Returns the name as stored; ErrInvalidProjectName (wrapped
// with the rule) for an empty, over-long or line-breaking name.
func RenameProject(repoDir, projDir, name string) (string, error) {
	name = strings.TrimFunc(name, nameSpace)
	switch {
	case strings.IndexFunc(name, nameVisible) < 0:
		return "", fmt.Errorf("%w: the name cannot be empty", ErrInvalidProjectName)
	case utf8.RuneCountInString(name) > maxProjectNameRunes:
		return "", fmt.Errorf("%w: at most %d characters", ErrInvalidProjectName, maxProjectNameRunes)
	case strings.IndexFunc(name, nameBreaking) >= 0:
		return "", fmt.Errorf("%w: no line breaks or control characters", ErrInvalidProjectName)
	}
	err := mutateAndSnapshotLatest(repoDir, projDir, func() (string, error) {
		p, err := store.LoadProject(repoDir)
		if err != nil {
			return "", err
		}
		if p.Name == name {
			return "", nil // unchanged: surfaces as ErrNothingToCommit
		}
		p.Name = name
		if err := store.SaveProject(repoDir, p); err != nil {
			return "", err
		}
		return fmt.Sprintf("cerne: rename project to %q", name), nil
	})
	if err != nil && !errors.Is(err, gitback.ErrNothingToCommit) {
		return "", err
	}
	return name, nil
}
