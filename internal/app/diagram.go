package app

import (
	"errors"
	"fmt"
	"strings"

	"gitlab.com/rockt/cerne/internal/slug"
	"gitlab.com/rockt/cerne/internal/store"
)

// ErrNotMermaidSequence is returned by SanitizeMermaid when the cleaned model
// output is not a Mermaid sequence diagram (does not start with the
// `sequenceDiagram` keyword).
var ErrNotMermaidSequence = errors.New("a saída do modelo não é um diagrama de sequência Mermaid")

// BuildSequenceDiagramPrompt assembles the pt-BR prompt instructing the LLM to
// produce a Mermaid sequence diagram for the feature, using its id/title/module
// and the current spec as context. The model is told to answer with bare
// Mermaid (no markdown fences, no prose). Shared by the CLI (`cerne spec
// diagram`) and the server (POST /api/features/{id}/diagram).
func BuildSequenceDiagramPrompt(f store.Feature, specContent string) string {
	module := f.Module
	if module == "" {
		module = "(sem módulo)"
	}
	var b strings.Builder
	b.WriteString("Você está desenhando um diagrama de sequência (em Mermaid) para uma feature de software, em português do Brasil.\n\n")
	b.WriteString("Feature:\n")
	fmt.Fprintf(&b, "- id: %s\n", f.ID)
	fmt.Fprintf(&b, "- título: %s\n", f.Title)
	fmt.Fprintf(&b, "- módulo: %s\n\n", module)
	b.WriteString("Spec da feature (contexto; pode estar vazia):\n")
	b.WriteString("---\n")
	b.WriteString(strings.TrimSpace(specContent))
	b.WriteString("\n---\n\n")
	b.WriteString("Produza um diagrama de sequência Mermaid (bloco começando com `sequenceDiagram`) que represente o fluxo principal desta feature, usando o título e o módulo como contexto.\n")
	b.WriteString("Responda APENAS com o código Mermaid, sem cercas de markdown e sem texto antes ou depois.\n")
	return b.String()
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
