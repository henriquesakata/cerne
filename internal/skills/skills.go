// Package skills embeds cerne's canonical Claude Code skills and installs them
// into a project's .claude/skills/ tree. cerne owns these guides; clients
// consume them via `cerne skill install` instead of maintaining divergent copies.
package skills

import (
	"bytes"
	"embed"
	"os"
	"path/filepath"

	"github.com/henriquesakata/cerne/internal/hooks"
)

//go:embed cerne-authoring/SKILL.md cerne-self-learning/SKILL.md
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
		{Name: "cerne-self-learning", RelPath: "cerne-self-learning/SKILL.md", Content: mustRead("cerne-self-learning/SKILL.md")},
	}
}

// Read returns one embedded skill file by its path under skills/ (e.g.
// "cerne-authoring/SKILL.md"); ok is false when it is not embedded.
func Read(rel string) (content string, ok bool) {
	b, err := files.ReadFile(rel)
	return string(b), err == nil
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
	// StatusSkipped marks an asset cerne could not deliver to another agent;
	// Result.Note carries the manual step. Never an error (F-0362).
	StatusSkipped Status = "skipped"
)

// Result pairs a skill with what Install did to it.
type Result struct {
	Skill  string
	Status Status
	Agent  hooks.Provider // set for non-Claude deliveries
	Path   string         // project-relative location, slash-separated, for display
	Note   string         // manual step when Status is StatusSkipped
}

// Install writes every canonical skill into <projDir>/.claude/skills/<RelPath>,
// overwriting to the canonical content (these are managed artifacts). It reports
// per skill whether the file was created, updated, or already current.
//
// For each requested non-Claude provider (the set hooks.DetectProviders
// returns) it also delivers the authoring guide — the same SKILL.md, one
// source — to that agent's own skills directory (Agent Skills standard; paths
// per R-0064/R-0065/R-0066), and Codex/Gemini get a one-line managed pointer in
// AGENTS.md/GEMINI.md. A failure there degrades to a skipped result (F-0362).
func Install(projDir string, providers ...hooks.Provider) ([]Result, error) {
	all := All()
	results := make([]Result, 0, len(all))
	for _, s := range all {
		dst := filepath.Join(projDir, ".claude", "skills", filepath.FromSlash(s.RelPath))
		status, err := writeSkill(dst, s.Content)
		if err != nil {
			return nil, err
		}
		results = append(results, Result{Skill: s.Name, Status: status, Path: ".claude/skills/" + s.Name})
	}
	for _, p := range providers {
		results = append(results, installProvider(projDir, p)...)
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
