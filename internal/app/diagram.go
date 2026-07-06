package app

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	"gitlab.com/rockt/cerne/internal/slug"
	"gitlab.com/rockt/cerne/internal/store"
)

// ErrNotMermaidSequence is returned by SanitizeMermaid when the cleaned model
// output is not a Mermaid sequence diagram (does not start with the
// `sequenceDiagram` keyword).
var ErrNotMermaidSequence = errors.New("the model output is not a Mermaid sequence diagram")

// BuildSequenceDiagramPrompt assembles the prompt instructing the LLM to
// produce a Mermaid sequence diagram for the feature, using its id/title/module,
// the current spec and the project structure as context. When projectStructure
// is non-empty it is embedded and the model is told to ground participants and
// messages in real components — and, when it has repository access (claude-cli /
// delegation agents), to read the relevant source files before drawing. The
// model is told to answer with bare Mermaid (no fences, no prose). Shared by the
// CLI (`cerne spec diagram`) and the server (POST /api/features/{id}/diagram).
func BuildSequenceDiagramPrompt(f store.Feature, specContent, projectStructure string) string {
	module := f.Module
	if module == "" {
		module = "(no module)"
	}
	var b strings.Builder
	b.WriteString("You are drawing a sequence diagram (in Mermaid) for a software feature.\n\n")
	b.WriteString("Feature:\n")
	fmt.Fprintf(&b, "- id: %s\n", f.ID)
	fmt.Fprintf(&b, "- title: %s\n", f.Title)
	fmt.Fprintf(&b, "- module: %s\n\n", module)
	b.WriteString("Feature spec (context; may be empty):\n")
	b.WriteString("---\n")
	b.WriteString(strings.TrimSpace(specContent))
	b.WriteString("\n---\n\n")
	if s := strings.TrimSpace(projectStructure); s != "" {
		b.WriteString("Project structure (tracked files; use it to identify the real components):\n")
		b.WriteString("---\n")
		b.WriteString(s)
		b.WriteString("\n---\n\n")
	}
	b.WriteString("Anchor the diagram in the project's real code:\n")
	b.WriteString("- Participants must be real components (packages, modules, services, types), not generic roles.\n")
	b.WriteString("- Messages must reflect real calls and interactions between those components in this feature's flow.\n")
	b.WriteString("- If you have access to the repository, examine the relevant source files (via the paths above) before drawing, so the flow matches the implementation.\n\n")
	b.WriteString("Produce a Mermaid sequence diagram (a block starting with `sequenceDiagram`) that represents this feature's main flow, using the title and module as context.\n")
	// Interim (F-0135): the diagram renders next to pt-BR docs, so its labels
	// follow that language until UI locale support (F-0125) makes it a choice.
	b.WriteString("Write the diagram labels (participants, messages, notes) in Brazilian Portuguese (pt-BR); keep identifiers such as package, type and function names exactly as they appear in the code.\n")
	b.WriteString("Reply with ONLY the Mermaid code, with no markdown fences and no text before or after.\n")
	return b.String()
}

// maxStructureEntries bounds the project structure listing so the prompt stays
// reasonable on a large repo.
const maxStructureEntries = 400

// skipDirs are directory names pruned from ProjectStructure: VCS, dependency and
// build/generated trees, and cerne's own gitignored projection.
var skipDirs = map[string]bool{
	"node_modules": true,
	"dist":         true,
	"vendor":       true,
	"build":        true,
	"target":       true,
	".docs":        true,
}

// ProjectStructure returns a compact, newline-separated listing of the project's
// files as forward-slash relative paths under projDir, pruning VCS / dependency
// / build trees and dot-entries, sorted and capped at maxStructureEntries. It
// grounds the diagram prompt in the real codebase; callers treat an empty result
// or error as best-effort (omit the structure) rather than failing generation.
func ProjectStructure(projDir string) (string, error) {
	var paths []string
	err := filepath.WalkDir(projDir, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		name := d.Name()
		if d.IsDir() {
			if p == projDir {
				return nil
			}
			if skipDirs[name] || strings.HasPrefix(name, ".") {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(name, ".") {
			return nil // skip dotfiles (.cernerc, .gitignore, …)
		}
		rel, err := filepath.Rel(projDir, p)
		if err != nil {
			return err
		}
		paths = append(paths, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return "", err
	}
	return formatStructure(paths, maxStructureEntries), nil
}

// formatStructure sorts the paths, caps the listing at max entries and appends a
// truncation marker when more were elided.
func formatStructure(paths []string, max int) string {
	sort.Strings(paths)
	truncated := false
	if len(paths) > max {
		paths = paths[:max]
		truncated = true
	}
	s := strings.Join(paths, "\n")
	if truncated {
		s += "\n… (estrutura truncada)"
	}
	return s
}

// SanitizeMermaid cleans raw LLM output into a bare Mermaid sequence diagram:
// it strips a surrounding ```mermaid ... ``` (or bare ``` ... ```) fenced block
// when present, trims whitespace, and requires the result to start with
// `sequenceDiagram`. Otherwise it returns ErrNotMermaidSequence.
func SanitizeMermaid(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if inner, ok := extractFencedBlock(s); ok {
		s = strings.TrimSpace(inner)
	}
	if !strings.HasPrefix(s, "sequenceDiagram") {
		return "", ErrNotMermaidSequence
	}
	return s, nil
}

// MermaidArtifactName normalizes a user-supplied name into a single
// <slug>.mmd filename: it drops any .mmd suffix, slugs the rest (lowercase,
// spaces to "-", diacritics folded), defaults to "sequence" when empty, and
// appends a single .mmd suffix.
func MermaidArtifactName(name string) string {
	base := strings.TrimSuffix(strings.TrimSpace(name), ".mmd")
	base = slug.Make(base)
	if base == "" {
		base = "sequence"
	}
	return base + ".mmd"
}

// extractFencedBlock returns the inner content of the first ``` fenced block in
// s (the opening fence may carry an info string such as "mermaid"), reporting
// whether such a block was found.
func extractFencedBlock(s string) (string, bool) {
	const fence = "```"
	open := strings.Index(s, fence)
	if open < 0 {
		return "", false
	}
	// Skip the opening fence and its optional info string up to the newline.
	rest := s[open+len(fence):]
	nl := strings.IndexByte(rest, '\n')
	if nl < 0 {
		return "", false
	}
	body := rest[nl+1:]
	close := strings.Index(body, fence)
	if close < 0 {
		return "", false
	}
	return body[:close], true
}
