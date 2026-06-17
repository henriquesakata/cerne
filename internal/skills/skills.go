// Package skills embeds cerne's canonical Claude Code skills and installs them
// into a project's .claude/skills/ tree. cerne owns these guides; clients
// consume them via `cerne skill install` instead of maintaining divergent copies.
package skills

import (
	"bytes"
	"embed"
	"os"
	"path/filepath"
)

//go:embed cerne-authoring/SKILL.md
var files embed.FS

// Skill is one embedded skill: Name is its directory, RelPath its location
// relative to .claude/skills/, Content the canonical markdown.
type Skill struct {
	Name    string
	RelPath string
	Content string
}

// All returns every canonical skill cerne ships.
func All() []Skill {
	return []Skill{
		{Name: "cerne-authoring", RelPath: "cerne-authoring/SKILL.md", Content: mustRead("cerne-authoring/SKILL.md")},
	}
}

// mustRead returns the embedded file or panics — a missing embed is a build-time
// bug, not a runtime condition.
func mustRead(name string) string {
	b, err := files.ReadFile(name)
	if err != nil {
		panic("skills: embedded file missing: " + name)
	}
	return string(b)
}

// Status is the outcome of installing one skill.
type Status string

const (
	StatusCreated   Status = "created"
	StatusUpdated   Status = "updated"
	StatusUnchanged Status = "unchanged"
)

// Result pairs a skill with what Install did to it.
type Result struct {
	Skill  string
	Status Status
}

// Install writes every canonical skill into <projDir>/.claude/skills/<RelPath>,
// overwriting to the canonical content (these are managed artifacts). It reports
// per skill whether the file was created, updated, or already current.
func Install(projDir string) ([]Result, error) {
	all := All()
	results := make([]Result, 0, len(all))
	for _, s := range all {
		dst := filepath.Join(projDir, ".claude", "skills", filepath.FromSlash(s.RelPath))
		status, err := writeSkill(dst, s.Content)
		if err != nil {
			return nil, err
		}
		results = append(results, Result{Skill: s.Name, Status: status})
	}
	return results, nil
}

// writeSkill writes content to dst (creating parent dirs), reporting whether the
// file was created, updated, or left unchanged (byte-identical).
func writeSkill(dst, content string) (Status, error) {
	existing, err := os.ReadFile(dst)
	if err == nil {
		if bytes.Equal(existing, []byte(content)) {
			return StatusUnchanged, nil
		}
		if err := os.WriteFile(dst, []byte(content), 0o644); err != nil {
			return "", err
		}
		return StatusUpdated, nil
	}
	if !os.IsNotExist(err) {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(dst, []byte(content), 0o644); err != nil {
		return "", err
	}
	return StatusCreated, nil
}
