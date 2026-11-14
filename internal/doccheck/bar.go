package doccheck

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/henriquesakata/cerne/internal/doclang"
)

// The two ways a spec fails the Gauntlet bar (F-0545). SpecBar wraps
// ErrBarMalformed with the specific reason.
var (
	ErrBarMissing   = errors.New(`no "Barra:" line`)
	ErrBarMalformed = errors.New(`malformed "Barra:" line`)
)

// The three literal forms a spec may carry (cerne-authoring skill, F-0270).
// Accent alternations tolerate NFC, NFD (darwin paste) and the bare unaccented
// spelling, mirroring the lint-side mention matching. An exemption's
// parenthesized text runs to ")" or to the end of the line: hard-wrapped
// markdown closes it on a later line.
var (
	htmlCommentRe = regexp.MustCompile(`(?s)<!--.*?-->`)
	// Template slots left unfilled, with only separators between them:
	// "<referência + acesso>", "<referência> — <acesso>" (F-0327).
	placeholderRe = regexp.MustCompile(`^<[^<>]*>(?:[\s\p{P}\p{S}]*<[^<>]*>)*$`)
	// A CommonMark autolink ("<https://…>") is a written value, not a slot.
	autolinkRe = regexp.MustCompile(`^<[A-Za-z][A-Za-z0-9+.-]{1,31}:[^\s<>]*>$`)
	// Emphasis is read through, never as part of a form: "**não aplicável**
	// (…)" is the exemption, not a named reference (F-0327).
	emphasis = strings.NewReplacer("*", "", "_", "", "\u00a0", " ")
)

// bare drops the emphasis, code and quote marks around a value: "**",
// "\"\"", "`<ref>`" and "“<citação>”" keep only what is inside them.
func bare(s string) string {
	return strings.Trim(s, " \t\u00a0*_`\"“”«»")
}

// slots reports bare text that is only template slots; an autolink is a value.
func slots(t string) bool {
	return placeholderRe.MatchString(t) && !autolinkRe.MatchString(t)
}

// Unwritten reports text that holds nothing but template: empty, or only
// <…> slots, once bare. The result reader applies it to the judge's citation;
// BarValue, to the bar and an exemption's parenthesized text (F-0327).
func Unwritten(s string) bool {
	t := bare(s)
	return t == "" || slots(t)
}

// BarValue reads value, the text after a "Barra:" label, by the rule the
// approve gate and the result reader share (F-0327). err says why it is no
// written bar, naming the cause: nothing written, a template slot, or an
// exemption form missing its parenthesized text. exempt reports a value in
// one of the two exemption forms.
func BarValue(value string) (exempt bool, err error) {
	return barValueIn(value, doclang.PT)
}

// barValueIn is BarValue with its causes naming the label of a bar line
// written in l (F-0418).
func barValueIn(value string, l doclang.Lang) (exempt bool, err error) {
	label := doclang.Display(doclang.BarLabel, l)
	plain := strings.TrimSpace(emphasis.Replace(value))
	switch t := bare(value); {
	case t == "":
		return false, malformed(l, `no value after "%s:"`, label)
	case slots(t):
		return false, malformed(l, `"%s:" holds only a <…> template placeholder, not a named reference`, label)
	}
	if t, el, inner, complete, ok := doclang.Exemption(plain); ok {
		need := "needs its one-line justification in parentheses"
		if t == doclang.Waived {
			need = "needs the quoted words in parentheses"
		}
		return true, exemptionError(l, inner, complete, `"`+doclang.Display(t, el)+`" `+need)
	}
	return false, nil
}

// exemptionError checks an exemption form's parenthesized text: missing or
// blank gets need, a template slot is named as such; l is the language of
// the bar line's label.
func exemptionError(l doclang.Lang, inner string, complete bool, need string) error {
	if !complete || bare(inner) == "" {
		return malformed(l, "%s", need)
	}
	if slots(bare(inner)) {
		return malformed(l, "%s, not a <…> template placeholder", need)
	}
	return nil
}

// barError is a bar cause worded for one language (F-0418): Error is the
// text the author reads, Unwrap the sentinel callers match with errors.Is.
type barError struct {
	sentinel error
	msg      string
}

func (e *barError) Error() string { return e.msg }
func (e *barError) Unwrap() error { return e.sentinel }

// missing is ErrBarMissing naming the label of language l. In Portuguese its
// text is the sentinel's own.
func missing(l doclang.Lang) error {
	return &barError{ErrBarMissing, fmt.Sprintf(`no "%s:" line`, doclang.Display(doclang.BarLabel, l))}
}

// malformed is ErrBarMalformed for a bar line whose label is in language l:
// the cause names the label the author wrote.
func malformed(l doclang.Lang, format string, a ...any) error {
	return &barError{ErrBarMalformed, fmt.Sprintf(`malformed "%s:" line: `, doclang.Display(doclang.BarLabel, l)) + fmt.Sprintf(format, a...)}
}

// SpecBar returns the first valid bar line of a spec body, or the reason none
// is valid (ErrBarMissing | ErrBarMalformed). Guide comments in the template
// contain the forms as examples, so HTML comments are stripped before
// scanning. Any valid line satisfies it — a malformed leftover (e.g. template
// residue) must not veto a valid bar written further down; when none is valid,
// the first failure is the one reported. Telling a stub from a written spec is
// the caller's job (the document gate). A template placeholder "<…>" is not a
// value, nor an exemption's justification or quote.
func SpecBar(body []byte) (string, error) {
	return SpecBarIn(body, doclang.PT)
}

// SpecBarIn is SpecBar with the causes worded for an author (F-0418): a
// missing bar names lang's label — the project's documentation language —
// and a malformed one the label of the line the author wrote.
func SpecBarIn(body []byte, lang doclang.Lang) (string, error) {
	var firstInvalid error
	inCode := false
	for _, line := range strings.Split(htmlCommentRe.ReplaceAllString(string(body), ""), "\n") {
		if doclang.Fence(line) {
			inCode = !inCode
			continue
		}
		value, l, ok := doclang.BarLine(line, inCode)
		if !ok {
			continue
		}
		_, err := barValueIn(value, l)
		if err == nil {
			return doclang.Display(doclang.BarLabel, l) + ": " + value, nil
		}
		if firstInvalid == nil {
			firstInvalid = err
		}
	}
	if firstInvalid == nil {
		return "", missing(lang)
	}
	return "", firstInvalid
}

// BarLine reads one line as a "Barra:" line, in any layout the approve gate
// and the result reader take: ok, and the text after the colon (past the
// label's closing emphasis), trimmed.
func BarLine(line string) (value string, ok bool) {
	value, _, ok = doclang.BarLine(line, false)
	return value, ok
}

// ErrJudgeQuestionMissing is a spec with a bar but no written judge-question
// line anywhere (F-0381).
var ErrJudgeQuestionMissing = errors.New(`no "Pergunta ao juiz:" line`)

// ErrJudgeQuestionMisplaced is a spec whose judge-question line sits outside
// the Verificação section (F-0381): reported apart from a missing one, with
// its line, because the fix is to move it.
var ErrJudgeQuestionMisplaced = errors.New("judge question outside the verification section")

// MisplacedJudgeQuestion is the ErrJudgeQuestionMisplaced a spec yields: Line
// is the question's 1-based line in the body.
type MisplacedJudgeQuestion struct{ Line int }

func (e *MisplacedJudgeQuestion) Error() string {
	return fmt.Sprintf("%s (line %d)", ErrJudgeQuestionMisplaced, e.Line)
}

func (e *MisplacedJudgeQuestion) Unwrap() error { return ErrJudgeQuestionMisplaced }

// BarExempt reports a spec whose bar is "não aplicável" or waived: the forms
// that owe no judge question.
func BarExempt(body []byte) bool {
	bar, err := SpecBar(body)
	if err != nil {
		return false
	}
	_, value, _ := strings.Cut(bar, ":")
	exempt, _ := BarValue(value)
	return exempt
}

// blankComments drops HTML comments but keeps their newlines, so line numbers
// still match the body.
func blankComments(body string) string {
	return htmlCommentRe.ReplaceAllStringFunc(body, func(c string) string {
		return strings.Repeat("\n", strings.Count(c, "\n"))
	})
}

// SpecJudgeQuestion returns the question the blind judge answers, read from the
// Verificação section of a spec body. With a bar named and no written question
// there it returns ErrJudgeQuestionMissing — or a *MisplacedJudgeQuestion when
// the line exists elsewhere. A spec whose bar is "não aplicável", waived, or
// absent owes none (the bar gate speaks for the last). Guide comments are
// stripped first, as SpecBar does.
func SpecJudgeQuestion(body []byte) (string, error) {
	if _, err := SpecBar(body); err != nil || BarExempt(body) {
		return "", nil
	}
	inCode, inVerif, misplaced := false, false, 0
	for i, line := range strings.Split(blankComments(string(body)), "\n") {
		if doclang.Fence(line) {
			inCode = !inCode
			continue
		}
		if !inCode && strings.HasPrefix(line, "## ") {
			inVerif = doclang.IsVerificationSection(line[3:])
			continue
		}
		q, ok := doclang.JudgeQuestionLine(line, inCode)
		if !ok || Unwritten(q) {
			continue
		}
		if inVerif {
			return q, nil
		}
		if misplaced == 0 {
			misplaced = i + 1
		}
	}
	if misplaced > 0 {
		return "", &MisplacedJudgeQuestion{misplaced}
	}
	return "", ErrJudgeQuestionMissing
}

// BarNotApplicable returns the parenthesized justification of a spec whose
// bar is "não aplicável (…)" (F-0390) — the one the approve check shows a
// standard or advanced spec's human; false for a named or waived bar, or none.
func BarNotApplicable(body []byte) (string, bool) {
	bar, err := SpecBar(body)
	if err != nil {
		return "", false
	}
	_, value, _ := strings.Cut(bar, ":")
	t, _, inner, _, ok := doclang.Exemption(strings.TrimSpace(emphasis.Replace(value)))
	if !ok || t != doclang.NotApplicable {
		return "", false
	}
	return strings.TrimSpace(inner), true
}
