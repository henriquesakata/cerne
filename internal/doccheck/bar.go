package doccheck

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// The two ways a spec fails the Gauntlet bar (F-0545). SpecBar wraps
// ErrBarMalformed with the specific reason.
var (
	ErrBarMissing   = errors.New(`no "Barra:" line`)
	ErrBarMalformed = errors.New(`malformed "Barra:" line`)
)

// The three literal forms a spec may carry (cerne-authoring skill, F-0270).
// Accent alternations tolerate NFC, NFD (darwin paste) and the bare unaccented
// spelling, mirroring the lint-side mention matching.
var (
	htmlCommentRe = regexp.MustCompile(`(?s)<!--.*?-->`)
	// [ \t], not \s: RE2's \s crosses the newline, so an empty "Barra:" took
	// the next line as its value (F-0327).
	barLineRe            = regexp.MustCompile(`(?m)^[ \t]*Barra:[ \t]*(.*)$`)
	naoAplicavelRe       = regexp.MustCompile(`(?i)^n(?:ã|a\x{0303}|a)o\s+aplic(?:á|a\x{0301}|a)vel\s*\(\s*([^)]+)\)`)
	dispensadaRe         = regexp.MustCompile(`(?i)^dispensada\s+por\s+decis(?:ã|a\x{0303}|a)o\s+humana\s*\(\s*([^)]+)\)`)
	naoAplicavelPrefixRe = regexp.MustCompile(`(?i)^n(?:ã|a\x{0303}|a)o\s+aplic(?:á|a\x{0301}|a)vel`)
	dispensadaPrefixRe   = regexp.MustCompile(`(?i)^dispensada`)
	// A template slot left unfilled ("<referência + acesso>"): the whole text
	// is one <…> (F-0327).
	placeholderRe = regexp.MustCompile(`^<[^<>]*>$`)
)

// isPlaceholder reports text that is only a template slot once the emphasis
// and quote marks around it are dropped ("**<ref>**", "\"<citação>\"").
func isPlaceholder(s string) bool {
	return placeholderRe.MatchString(strings.Trim(s, " \t\u00a0*_\"“”«»"))
}

// filled reports value is form re with its parenthesized text written:
// present and not a template slot.
func filled(re *regexp.Regexp, value string) bool {
	m := re.FindStringSubmatch(value)
	return m != nil && !isPlaceholder(m[1])
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
	text := htmlCommentRe.ReplaceAllString(string(body), "")
	matches := barLineRe.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return "", ErrBarMissing
	}
	var firstInvalid error
	for _, m := range matches {
		value := strings.TrimSpace(m[1])
		var err error
		switch {
		case value == "" || isPlaceholder(value):
			err = fmt.Errorf(`%w: no value after "Barra:" (a <…> template placeholder is not a value)`, ErrBarMalformed)
		case naoAplicavelPrefixRe.MatchString(value) && !filled(naoAplicavelRe, value):
			err = fmt.Errorf(`%w: "não aplicável" needs its one-line justification in parentheses, not a <…> template placeholder`, ErrBarMalformed)
		case dispensadaPrefixRe.MatchString(value) && !filled(dispensadaRe, value):
			err = fmt.Errorf(`%w: "dispensada por decisão humana" needs the quoted words in parentheses, not a <…> template placeholder`, ErrBarMalformed)
		default:
			return "Barra: " + value, nil
		}
		if firstInvalid == nil {
			firstInvalid = err
		}
	}
	return "", firstInvalid
}
