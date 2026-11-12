// Package mdlink parses inline markdown links and classifies their targets via
// a caller-supplied resolver. It is a leaf helper shared by the lint
// broken-link rule and the entity graph's content-link edges, so it imports
// neither — keeping the relationship parsing in one place without a cycle.
package mdlink

import (
	"regexp"
	"strings"
)

// linkRx captures the target of inline markdown links [x](target) and
// [x](target "title"); the optional title after the first whitespace is
// ignored.
var linkRx = regexp.MustCompile(`\[[^\]]*\]\(([^)\s]+)[^)]*\)`)

// inlineCodeRx matches a single- or double-backtick inline code span on one
// line. The double-backtick alternative is listed first so, at a given
// position, it is preferred (Go regexp is leftmost-first) and consumes the
// span whole before the single-backtick alternative can nibble its delimiters.
var inlineCodeRx = regexp.MustCompile("``[^`\n]*``|`[^`\n]*`")

// urlRx matches an http(s) URL up to the first delimiter — whitespace, a closing
// paren/bracket, an angle bracket, or a quote/backtick — enough to lift a URL
// out of a markdown link [x](url) or bare prose. Trailing sentence punctuation
// is trimmed by ExternalURLs.
var urlRx = regexp.MustCompile("https?://[^\\s)\\]<>\"'`]+")

// ContentLinks parses the inline markdown links of content and classifies each
// distinct target with resolve. External (http/https/mailto), pure-anchor and
// absolute targets are skipped; a #fragment suffix is stripped before resolve
// is called. Targets resolve reports present go to resolved (as the id resolve
// returns); the rest go to broken (the original target text, fragment kept).
// Each distinct raw target is considered once, in first-appearance order.
func ContentLinks(content string, resolve func(target string) (id string, ok bool)) (resolved, broken []string) {
	content = stripCode(content)
	seen := map[string]bool{}
	for _, m := range linkRx.FindAllStringSubmatch(content, -1) {
		target := m[1]
		if seen[target] || skipTarget(target) {
			continue
		}
		seen[target] = true
		clean, _, _ := strings.Cut(target, "#")
		if clean == "" {
			continue
		}
		if id, ok := resolve(clean); ok {
			resolved = append(resolved, id)
		} else {
			broken = append(broken, target)
		}
	}
	return resolved, broken
}

// wikiRx captures the target of id-form wiki-links: [[F-0123]] or the alias
// form [[F-0123|shown text]] (target before the |). Restricting the pattern to
// <alpha>-<digits> ids is by design (F-0167): [[some title]] or a TOML
// [[table]] in prose is not a wiki-link for cerne, so it can never false-
// positive — title resolution was cut from v1.
var wikiRx = regexp.MustCompile(`\[\[([A-Za-z]+-\d+)(?:\|[^\]\n]*)?\]\]`)

// WikiLinks parses the id-form wiki-links of content and classifies each
// distinct target with resolve, mirroring the ContentLinks contract: targets
// resolve reports present go to resolved (as the id resolve returns), the rest
// to broken (the raw target). Each distinct raw target is considered once, in
// first-appearance order; code fences and inline spans are excluded.
func WikiLinks(content string, resolve func(target string) (id string, ok bool)) (resolved, broken []string) {
	content = stripCode(content)
	seen := map[string]bool{}
	for _, m := range wikiRx.FindAllStringSubmatch(content, -1) {
		target := m[1]
		if seen[target] {
			continue
		}
		seen[target] = true
		if id, ok := resolve(target); ok {
			resolved = append(resolved, id)
		} else {
			broken = append(broken, target)
		}
	}
	return resolved, broken
}

// CanonicalID normalizes a wiki-link target to the canonical entity-id form
// (uppercase). Lint and the entity graph both resolve through this single
// helper so the two layers can never diverge on case handling (F-0167).
func CanonicalID(target string) string { return strings.ToUpper(target) }

// ExternalURLs returns the distinct http(s) URLs in content — whether written as
// a markdown link [x](url) or bare in prose — in first-appearance order. URLs
// inside fenced or inline code are excluded (stripCode) and trailing sentence
// punctuation is trimmed. Non-http schemes (mailto:, …) and relative/anchor
// targets never appear. These are the external sources a provenance rule checks
// for registration.
func ExternalURLs(content string) []string {
	content = stripCode(content)
	seen := map[string]bool{}
	var urls []string
	for _, m := range urlRx.FindAllString(content, -1) {
		u := strings.TrimRight(m, ".,;:!?")
		if u == "" || seen[u] {
			continue
		}
		seen[u] = true
		urls = append(urls, u)
	}
	return urls
}

// stripCode removes fenced code blocks and inline code spans so that
// link-looking syntax inside code — e.g. a shell command [x](docker inspect …)
// — is not mistaken for a markdown link. Fenced blocks (``` or ~~~ delimited)
// are dropped by a line scan; inline spans are blanked per surviving line.
func stripCode(content string) string {
	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	inFence := false
	for _, line := range lines {
		if IsFenceDelim(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		out = append(out, inlineCodeRx.ReplaceAllString(line, ""))
	}
	return strings.Join(out, "\n")
}

// IsFenceDelim reports whether the line opens or closes a fenced code block:
// its first non-space content is a run of three or more backticks or tildes.
func IsFenceDelim(line string) bool {
	t := strings.TrimLeft(line, " \t")
	return strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~")
}

// Ref is one link occurrence: its raw target and the 1-based line of the
// content it was written on.
type Ref struct {
	Target string
	Line   int
}

// Refs lists every occurrence (no de-duplication) of the inline markdown links
// (wiki false; external, anchor and absolute targets skipped, fragment kept) or
// the id-form wiki-links (wiki true) of content, in document order. Code is
// excluded as in ContentLinks/WikiLinks, but Line counts from the start of
// content, fenced blocks included, so it matches the file in an editor.
func Refs(content string, wiki bool) []Ref {
	var out []Ref
	inFence := false
	for i, line := range strings.Split(content, "\n") {
		if IsFenceDelim(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		line = inlineCodeRx.ReplaceAllString(line, "")
		if wiki {
			for _, m := range wikiRx.FindAllStringSubmatch(line, -1) {
				out = append(out, Ref{m[1], i + 1})
			}
			continue
		}
		for _, m := range linkRx.FindAllStringSubmatch(line, -1) {
			if skipTarget(m[1]) {
				continue
			}
			if clean, _, _ := strings.Cut(m[1], "#"); clean != "" {
				out = append(out, Ref{m[1], i + 1})
			}
		}
	}
	return out
}

// skipTarget reports whether the link target is outside link resolution:
// external schemes, pure anchors and absolute paths.
func skipTarget(target string) bool {
	lower := strings.ToLower(target)
	return strings.HasPrefix(lower, "http://") ||
		strings.HasPrefix(lower, "https://") ||
		strings.HasPrefix(lower, "mailto:") ||
		strings.HasPrefix(target, "#") ||
		strings.HasPrefix(target, "/")
}
