// Package doclang is the single catalog of the document vocabulary cerne
// writes and reads, per language (F-0359). The templates scaffold from it and
// the readers (lint, gates) match against it, so the two languages cannot
// drift apart across packages.
//
// The split mirrors internal/clarify: a project has ONE language for what
// cerne writes (docs.language), while the readers always accept the whole
// catalog — a document authored in the other language is never refused.
package doclang

import (
	"errors"
	"fmt"
	"strings"
)

// Lang is a documentation language.
type Lang string

const (
	PT Lang = "pt-BR"
	EN Lang = "en"
)

// ErrUnknownLang reports a language outside the catalog.
var ErrUnknownLang = errors.New("unknown documentation language")

// Parse reads a docs.language value. Empty is PT: every project created
// before the setting existed is Portuguese.
func Parse(s string) (Lang, error) {
	switch strings.TrimSpace(s) {
	case "", string(PT):
		return PT, nil
	case string(EN):
		return EN, nil
	}
	return "", fmt.Errorf("%w: %q (want %q or %q)", ErrUnknownLang, s, PT, EN)
}

// Kind is a templated document kind.
type Kind int

const (
	Spec Kind = iota
	Plan
	Result
	Decision
	Runbook
)

// sections holds, per kind, the canonical level-2 headings in template order:
// one row per section, PT then EN.
var sections = map[Kind][][2]string{
	Spec: {
		{"Problema", "Problem"},
		{"Objetivo e critérios de aceitação", "Goal and acceptance criteria"},
		{"Decisões do design", "Design decisions"},
		{"Desenho", "Design"},
		{"Verificação", "Verification"},
		{"Execução recomendada (agente)", "Recommended execution (agent)"},
		{"Fora de escopo", "Out of scope"},
	},
	Plan: {
		{"Estratégia", "Strategy"},
		{"Sequência de tarefas", "Task sequence"},
		{"Dependências e ordem", "Dependencies and order"},
		{"Riscos e rollback", "Risks and rollback"},
		{"Restrições para o agente", "Constraints for the agent"},
		{"Definição de pronto", "Definition of done"},
	},
	Result: {
		{"Resultado", "Result"},
		{"Aceitação", "Acceptance"},
		{"O que mudou", "What changed"},
		{"Débitos e follow-ups", "Debts and follow-ups"},
		{"Aprendizados e desvios", "Learnings and deviations"},
		{"Gauntlet", "Gauntlet"},
	},
	Decision: {
		{"Contexto", "Context"},
		{"Decisão", "Decision"},
		{"Consequências", "Consequences"},
		{"Alternativas consideradas", "Alternatives considered"},
		{"Implementação e relações", "Implementation and relations"},
	},
	Runbook: {
		{"Quando usar", "When to use"},
		{"Pré-requisitos", "Prerequisites"},
		{"Procedimento", "Procedure"},
		{"Verificação", "Verification"},
		{"Rollback e troubleshooting", "Rollback and troubleshooting"},
	},
}

func col(l Lang) int {
	if l == EN {
		return 1
	}
	return 0
}

// Sections returns the canonical headings of kind k in language l, in
// template order. An unknown language reads as PT.
func Sections(k Kind, l Lang) []string {
	out := make([]string, len(sections[k]))
	for i, row := range sections[k] {
		out[i] = row[col(l)]
	}
	return out
}

// Missing returns the sections of kind k that have none of their accepted
// headings in present, named in language l. A section counts as present under
// either language, so a document mixing the two is complete.
func Missing(k Kind, present map[string]bool, l Lang) []string {
	var missing []string
	for _, row := range sections[k] {
		if !present[row[0]] && !present[row[1]] {
			missing = append(missing, row[col(l)])
		}
	}
	return missing
}

// IsSection reports whether heading is section index i of kind k in any
// language, ignoring case and surrounding space.
func IsSection(k Kind, i int, heading string) bool {
	h := strings.TrimSpace(heading)
	row := sections[k][i]
	return strings.EqualFold(h, row[0]) || strings.EqualFold(h, row[1])
}
