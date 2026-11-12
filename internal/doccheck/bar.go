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
	plain := strings.TrimSpace(emphasis.Replace(value))
	switch t := bare(value); {
	case t == "":
		return false, fmt.Errorf(`%w: no value after "Barra:"`, ErrBarMalformed)
	case slots(t):
		return false, fmt.Errorf(`%w: "Barra:" holds only a <…> template placeholder, not a named reference`, ErrBarMalformed)
	}
	if t, l, inner, complete, ok := doclang.Exemption(plain); ok {
		need := "needs its one-line justification in parentheses"
		if t == doclang.Waived {
			need = "needs the quoted words in parentheses"
		}
		return true, exemptionError(inner, complete, `"`+doclang.Display(t, l)+`" `+need)
	}
	return false, nil
}

// exemptionError checks an exemption form's parenthesized text: missing or
// blank gets need, a template slot is named as such.
func exemptionError(inner string, complete bool, need string) error {
	if !complete || bare(inner) == "" {
		return fmt.Errorf("%w: %s", ErrBarMalformed, need)
	}
	if slots(bare(inner)) {
		return fmt.Errorf("%w: %s, not a <…> template placeholder", ErrBarMalformed, need)
	}
	return nil
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
	firstInvalid := ErrBarMissing
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
		_, err := BarValue(value)
		if err == nil {
			return doclang.Display(doclang.BarLabel, l) + ": " + value, nil
		}
		if firstInvalid == ErrBarMissing {
			firstInvalid = err
		}
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
