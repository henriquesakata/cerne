// Package doclang is the single catalog of the document vocabulary cerne
// writes and reads, per language (F-0359): the template section headings and
// the words of the bar/Gauntlet protocol. The templates scaffold from it and
// the readers (lint, gates) build their matchers from it, so no reader carries
// a word of its own and the languages cannot drift apart across packages.
//
// The split mirrors internal/clarify: a project has ONE language for what
// cerne writes, while the readers always accept the whole catalog — a document
// authored in another language is never refused.
//
// A new language is one entry in langs plus one column in sections and in
// terms; the tests refuse a catalog with a hole in it.
package doclang

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Lang is a documentation language.
type Lang string

const (
	PT Lang = "pt-BR"
	EN Lang = "en"
)

// langs is every language of the catalog, in the order alternations are
// built. PT stays first: it is the default and its forms predate the catalog.
var langs = []Lang{PT, EN}

// Langs returns the languages of the catalog.
func Langs() []Lang { return append([]Lang(nil), langs...) }

// Strict returns the languages whose protocol words readers match strictly:
// every language but PT. The Portuguese forms predate the catalog and keep
// the loose layout documents already rely on; their words (Barra, Rodada,
// vitória) rarely open a line by accident. A language added later shares
// everyday words with prose (Bar, Round, win), so its forms must look like
// protocol lines to count.
func Strict() []Lang { return append([]Lang(nil), langs[1:]...) }

// ErrUnknownLang reports a language outside the catalog.
var ErrUnknownLang = errors.New("unknown documentation language")

// Parse reads a stored language value. Empty is PT: every project created
// before the value existed is Portuguese.
func Parse(s string) (Lang, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return PT, nil
	}
	for _, l := range langs {
		if strings.EqualFold(s, string(l)) { // "EN" and "pt-br" are what people type
			return l, nil
		}
	}
	return "", fmt.Errorf("%w: %q (want one of: %s)", ErrUnknownLang, s, joinLangs())
}

func joinLangs() string {
	names := make([]string, len(langs))
	for i, l := range langs {
		names[i] = string(l)
	}
	return strings.Join(names, ", ")
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

// SpecCriteria is the position, in the spec template, of the section whose
// numbered items are the acceptance contract (result-missing-evidence reads
// it). Named here so no reader carries the index as a bare number.
const SpecCriteria = 1

// sections holds, per kind and language, the canonical level-2 headings in
// template order. Every language lists the same sections in the same order.
var sections = map[Kind]map[Lang][]string{
	Spec: {
		PT: {"Problema", "Objetivo e critérios de aceitação", "Decisões do design", "Desenho", "Verificação", "Execução recomendada (agente)", "Fora de escopo"},
		EN: {"Problem", "Goal and acceptance criteria", "Design decisions", "Design", "Verification", "Recommended execution (agent)", "Out of scope"},
	},
	Plan: {
		PT: {"Estratégia", "Sequência de tarefas", "Dependências e ordem", "Riscos e rollback", "Restrições para o agente", "Definição de pronto"},
		EN: {"Strategy", "Task sequence", "Dependencies and order", "Risks and rollback", "Constraints for the agent", "Definition of done"},
	},
	Result: {
		PT: {"Resultado", "Aceitação", "O que mudou", "Débitos e follow-ups", "Aprendizados e desvios", "Gauntlet"},
		EN: {"Result", "Acceptance", "What changed", "Debts and follow-ups", "Learnings and deviations", "Gauntlet"},
	},
	Decision: {
		PT: {"Contexto", "Decisão", "Consequências", "Alternativas consideradas", "Implementação e relações"},
		EN: {"Context", "Decision", "Consequences", "Alternatives considered", "Implementation and relations"},
	},
	Runbook: {
		PT: {"Quando usar", "Pré-requisitos", "Procedimento", "Verificação", "Rollback e troubleshooting"},
		EN: {"When to use", "Prerequisites", "Procedure", "Verification", "Rollback and troubleshooting"},
	},
}

// Sections returns the canonical headings of kind k in language l, in
// template order. A language outside the catalog reads as PT.
func Sections(k Kind, l Lang) []string {
	list, ok := sections[k][l]
	if !ok {
		list = sections[k][PT]
	}
	return append([]string(nil), list...)
}

// Missing returns the sections of kind k that appear under none of their
// headings in present, named in language l. A section counts as present under
// any language's heading, so a document mixing languages is complete. Case
// and surrounding space are ignored, as IsSection does: both readers of a
// heading agree on what the same heading is.
func Missing(k Kind, present map[string]bool, l Lang) []string {
	has := make(map[string]bool, len(present))
	for h, ok := range present {
		if ok {
			has[strings.ToLower(strings.TrimSpace(h))] = true
		}
	}
	var missing []string
	names := Sections(k, l)
	for i := range names {
		found := false
		for _, lang := range langs {
			found = found || has[strings.ToLower(sections[k][lang][i])]
		}
		if !found {
			missing = append(missing, names[i])
		}
	}
	return missing
}

// IsSection reports whether heading is section i of kind k in any language,
// ignoring case and surrounding space.
func IsSection(k Kind, i int, heading string) bool {
	h := strings.TrimSpace(heading)
	for _, lang := range langs {
		if strings.EqualFold(h, sections[k][lang][i]) {
			return true
		}
	}
	return false
}

// Term is a word of the bar/Gauntlet protocol that authors write and readers
// match.
type Term int

const (
	// BarLabel opens the bar line: "Barra: <reference>".
	BarLabel Term = iota
	// NotApplicable and Waived are the two exemption forms after the label.
	// WaivedOpen is how a waiver starts: a value that opens with it is read as
	// an attempted waiver, and refused when the rest of the form is missing.
	NotApplicable
	Waived
	WaivedOpen
	// MethodLabel and ExitLabel open the two header lines under the bar.
	MethodLabel
	ExitLabel
	// Round anchors a Gauntlet round: "Rodada 2 — …".
	Round
	// CriticFindings opens the critic's line.
	CriticFindings
	// Win, Loss and Tie are the outcome of a round.
	Win
	Loss
	Tie
	// VerdictLabel is the label of the judge's fixed last line, and TieLetter
	// its value for a tie ("VEREDITO: EMPATE"); the other values are A and B.
	VerdictLabel
	TieLetter
	// OutcomeLabel is a label that may precede an outcome on its own line.
	OutcomeLabel
	// Criterion and Criteria cite acceptance criteria in a result.
	Criterion
	Criteria
	// RangeTo, ListAnd and ListOr join the numbers of a plural citation:
	// "critérios 2 a 4", "criteria 1, 3 and 5".
	RangeTo
	ListAnd
	ListOr
	numTerms
)

// terms holds each term as a regular-expression fragment per language. The
// Portuguese fragments tolerate precomposed and decomposed accents (a paste
// from macOS arrives decomposed) and the unaccented spelling.
var terms = map[Term]map[Lang]string{
	BarLabel:       {PT: `Barra`, EN: `Bar`},
	NotApplicable:  {PT: `n(?:ã|a\x{0303}|a)o[\s-]+aplic(?:á|a\x{0301}|a)vel`, EN: `not[\s-]+applicable`},
	Waived:         {PT: `dispensada\s+por\s+decis(?:ã|a\x{0303}|a)o\s+humana`, EN: `waived\s+by\s+human\s+decision`},
	WaivedOpen:     {PT: `dispensada`, EN: `waived\s+by\b`},
	MethodLabel:    {PT: `M(?:é|e\x{0301}|e)todo`, EN: `Method`},
	ExitLabel:      {PT: `Sa(?:í|i\x{0301}|i)da`, EN: `Exit`},
	Round:          {PT: `Rodada`, EN: `Round`},
	CriticFindings: {PT: `Achados? do cr(?:í|i\x{0301}|i)tico`, EN: `Critic(?:['’]s)? findings?`},
	Win:            {PT: `vit(?:ó|o\x{0301}|o)ria`, EN: `win`},
	Loss:           {PT: `derrota`, EN: `loss`},
	Tie:            {PT: `empate`, EN: `tie`},
	VerdictLabel:   {PT: `veredito`, EN: `verdict`},
	TieLetter:      {PT: `EMPATE`, EN: `TIE`},
	OutcomeLabel:   {PT: `veredito|resultado`, EN: `verdict|result|outcome`},
	Criterion:      {PT: `crit(?:é|e\x{0301}|e)rio`, EN: `criterion`},
	Criteria:       {PT: `crit(?:é|e\x{0301}|e)rios`, EN: `criteria`},
	RangeTo:        {PT: `a`, EN: `to`},
	ListAnd:        {PT: `e`, EN: `and`},
	ListOr:         {PT: `ou`, EN: `or`},
}

// In returns term t as a regular-expression fragment in language l alone.
// Readers that must tell the languages apart (a form whose layout differs
// between them) use it; everything else uses Alt.
func In(t Term, l Lang) string {
	return `(?:` + terms[t][l] + `)`
}

// Alt returns term t as a non-capturing alternation over every language of
// the catalog: the fragment a bilingual reader compiles into its matcher.
func Alt(t Term) string {
	parts := make([]string, len(langs))
	for i, l := range langs {
		parts[i] = terms[t][l]
	}
	return `(?:` + strings.Join(parts, `|`) + `)`
}

var termRes = func() map[Term]*regexp.Regexp {
	m := make(map[Term]*regexp.Regexp, numTerms)
	for t := Term(0); t < numTerms; t++ {
		m[t] = regexp.MustCompile(`(?i)^` + Alt(t) + `$`)
	}
	return m
}()

// Is reports whether word is term t in any language, ignoring case and
// surrounding space: how a reader classifies a word it captured.
func Is(t Term, word string) bool {
	return termRes[t].MatchString(strings.TrimSpace(word))
}

// LangAt returns the language whose form of term t opens s, ignoring case;
// PT when none does. Readers use it to answer in the author's language.
func LangAt(t Term, s string) Lang {
	for _, l := range langs {
		if regexp.MustCompile(`(?i)^` + In(t, l)).MatchString(s) {
			return l
		}
	}
	return PT
}

// display is how a term is written in prose addressed to the author (an
// error message, a hint): the literal words, not a pattern.
var display = map[Term]map[Lang]string{
	BarLabel:      {PT: "Barra", EN: "Bar"},
	NotApplicable: {PT: "não aplicável", EN: "not applicable"},
	Waived:        {PT: "dispensada por decisão humana", EN: "waived by human decision"},
}

// Display returns term t as written for a reader in language l.
func Display(t Term, l Lang) string { return display[t][l] }
