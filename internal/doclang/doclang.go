// Package doclang is the catalog of the document vocabulary cerne writes and
// reads, per language (F-0359): the template section headings (this file) and
// the bar/Gauntlet protocol with its recognizers (protocol.go). The templates
// scaffold from it; the readers (lint, gates) call its recognizers and carry
// no word, no pattern and no language branch of their own.
//
// The split mirrors internal/clarify: a project has ONE language for what
// cerne writes, while reading always takes the whole catalog — a document
// authored in another language, or mixing two, is never refused.
//
// Adding a language: one entry in langs, one layout in layouts, one column
// in sections and in terms. TestCatalog_Complete refuses a hole. No reader
// changes. Adding a term: one row in terms, used by a recognizer here.
//
// Declared limits:
//   - Languages are not symmetric. Portuguese is "loose" (layouts): its forms
//     predate the catalog and documents rely on their looseness, so nothing
//     they accepted may be refused. Every later language is "strict".
//   - A list line that opens with the bar label and a colon is a bar line even
//     when it is prose ("- Bar: the progress bar shows…"), in every language.
//   - Messages that name the bar still say "Barra:" whatever the author's
//     language: they are sentinel errors other code and tests compare.
//   - A citation joins its numbers with the words of its own language:
//     "criteria 1, 2 e 3" cites 1 and 2.
//   - Words cerne itself writes into documents elsewhere are not here yet: the
//     cost and delivery stamps, an epic's execution map, the origin line of a
//     promoted pending or idea.
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

// layout is how forgiving the recognizers are with a language's forms.
type layout int

const (
	// strict is every language added after Portuguese, and the zero value on
	// purpose: its words are everyday prose (Bar, Round, win, tie), so a form
	// must look like a protocol line to count.
	strict layout = iota
	// loose is Portuguese: the forms as they matched before the catalog —
	// any case, a free qualifier before the bar's colon, an outcome word
	// anywhere in a round. Its words (Barra, Rodada, vitória) rarely open a
	// line by accident, and documents already rely on the looseness.
	loose
)

// layouts declares each language's layout; a language missing here fails
// TestCatalog_Complete.
var layouts = map[Lang]layout{PT: loose, EN: strict}

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

// Positions, in their templates, of the sections a reader looks for by role.
const (
	specProblem      = 0 // where feature new seeds the context
	specCriteria     = 1 // its numbered items are the acceptance contract
	resultDebts      = 3 // what the delivery left open
	specVerification = 4 // where the bar and the judge's question live
	resultGauntlet   = 5 // the loop's record
)

// ProblemHeadings returns the heading of the spec's problem section in every
// language of the catalog.
func ProblemHeadings() []string {
	out := make([]string, len(langs))
	for i, l := range langs {
		out[i] = sections[Spec][l][specProblem]
	}
	return out
}

// IsDebtsSection reports whether heading opens the result section that lists
// what the delivery left open, in any language.
func IsDebtsSection(heading string) bool { return IsSection(Result, resultDebts, heading) }

// IsGauntletSection reports whether heading opens the result's Gauntlet
// section, in any language.
func IsGauntletSection(heading string) bool { return IsSection(Result, resultGauntlet, heading) }

// IsVerificationSection reports whether heading opens the spec section that
// carries the Gauntlet bar and the judge's question, in any language.
func IsVerificationSection(heading string) bool { return IsSection(Spec, specVerification, heading) }

// IsCriteriaSection reports whether heading opens the spec section whose
// numbered items are the acceptance contract, in any language.
func IsCriteriaSection(heading string) bool { return IsSection(Spec, specCriteria, heading) }

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
	// Unmet marks an acceptance criterion the delivery did not meet.
	Unmet
	// EvidenceLabel, IfLabel and NextStepLabel open the sections of a
	// handoff document (F-0368): "Evidência:", "Se:", "Próximo passo:".
	EvidenceLabel
	IfLabel
	NextStepLabel
	// JudgeQuestionLabel opens the line under the bar that holds the question
	// the blind judge answers (F-0381).
	JudgeQuestionLabel
	numTerms
)

// form is one term in one language: re, the regular-expression fragment the
// recognizers compile, and text, the literal words shown to an author in a
// message (only the terms a message names carry one).
type form struct{ re, text string }

// terms is the vocabulary. The Portuguese fragments tolerate precomposed and
// decomposed accents (a paste from macOS arrives decomposed) and the
// unaccented spelling.
var terms = map[Term]map[Lang]form{
	BarLabel:           {PT: {`Barra`, "Barra"}, EN: {`Bar`, "Bar"}},
	NotApplicable:      {PT: {`n(?:ã|a\x{0303}|a)o[\s-]+aplic(?:á|a\x{0301}|a)vel`, "não aplicável"}, EN: {`not[\s-]+applicable`, "not applicable"}},
	Waived:             {PT: {`dispensada\s+por\s+decis(?:ã|a\x{0303}|a)o\s+humana`, "dispensada por decisão humana"}, EN: {`waived\s+by\s+human\s+decision`, "waived by human decision"}},
	WaivedOpen:         {PT: {re: `dispensada`}, EN: {re: `waived\s+by\b`}},
	MethodLabel:        {PT: {re: `M(?:é|e\x{0301}|e)todo`}, EN: {re: `Method`}},
	ExitLabel:          {PT: {re: `Sa(?:í|i\x{0301}|i)da`}, EN: {re: `Exit`}},
	Round:              {PT: {`Rodada`, "Rodada"}, EN: {`Round`, "Round"}},
	CriticFindings:     {PT: {`Achados? do cr(?:í|i\x{0301}|i)tico`, "Achados do crítico"}, EN: {`Critic(?:['’]s)? findings?`, "Critic findings"}},
	Win:                {PT: {`vit(?:ó|o\x{0301}|o)ria`, "vitória"}, EN: {`win`, "win"}},
	Loss:               {PT: {`derrota`, "derrota"}, EN: {`loss`, "loss"}},
	Tie:                {PT: {`empate`, "empate"}, EN: {`tie`, "tie"}},
	VerdictLabel:       {PT: {re: `veredito`}, EN: {re: `verdict`}},
	TieLetter:          {PT: {re: `EMPATE`}, EN: {re: `TIE`}},
	OutcomeLabel:       {PT: {re: `veredito|resultado`}, EN: {re: `verdict|result|outcome`}},
	Criterion:          {PT: {re: `crit(?:é|e\x{0301}|e)rio`}, EN: {re: `criterion`}},
	Criteria:           {PT: {re: `crit(?:é|e\x{0301}|e)rios`}, EN: {re: `criteria`}},
	RangeTo:            {PT: {re: `a`}, EN: {re: `to`}},
	ListAnd:            {PT: {re: `e`}, EN: {re: `and`}},
	ListOr:             {PT: {re: `ou`}, EN: {re: `or`}},
	Unmet:              {PT: {re: `PENDENTE`}, EN: {re: `UNMET`}},
	EvidenceLabel:      {PT: {`Evid(?:ê|e\x{0302}|e)ncia`, "Evidência"}, EN: {`Evidence`, "Evidence"}},
	IfLabel:            {PT: {`Se`, "Se"}, EN: {`If`, "If"}},
	NextStepLabel:      {PT: {`Pr(?:ó|o\x{0301}|o)ximo passo`, "Próximo passo"}, EN: {`Next step`, "Next step"}},
	JudgeQuestionLabel: {PT: {`Pergunta ao juiz`, "Pergunta ao juiz"}, EN: {`Question for the judge`, "Question for the judge"}},
}

// in is term t as a regular-expression fragment in language l.
func in(t Term, l Lang) string {
	return `(?:` + terms[t][l].re + `)`
}

// alt is term t as an alternation over every language of the catalog.
func alt(t Term) string {
	parts := make([]string, len(langs))
	for i, l := range langs {
		parts[i] = terms[t][l].re
	}
	return `(?:` + strings.Join(parts, `|`) + `)`
}

var termRes = func() map[Term]*regexp.Regexp {
	m := make(map[Term]*regexp.Regexp, numTerms)
	for t := Term(0); t < numTerms; t++ {
		m[t] = regexp.MustCompile(`(?i)^` + alt(t) + `$`)
	}
	return m
}()

// Is reports whether word is term t in any language, ignoring case and
// surrounding space: how a reader classifies a word a recognizer returned.
func Is(t Term, word string) bool {
	return termRes[t].MatchString(strings.TrimSpace(word))
}

// Display returns term t as written for an author in language l.
func Display(t Term, l Lang) string { return terms[t][l].text }
