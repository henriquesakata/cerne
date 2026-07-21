// Package lang detects the language of doc content so authoring surfaces can
// pick the matching clarification-marker vocabulary (F-0182). Detection is
// deliberately confined to the authoring path: reading and resolving markers
// never depend on it (the parser accepts the whole catalog), so a wrong guess
// here only picks the word a scaffold is born with — visible and editable —
// and can never hide a chip or loosen a gate.
package lang

import (
	"regexp"
	"strings"
)

// Stopword sets per language: short, high-frequency function words that
// rarely appear in the other language. Single letters and shared words
// (a, o, no, as, ...) are deliberately absent — they cost more false hits
// than they buy signal.
var stopwords = map[string][]string{
	"pt": {"de", "que", "não", "nao", "uma", "um", "para", "com", "por", "como", "mais", "mas", "foi", "são", "sao", "dos", "das", "ele", "ela", "seu", "sua", "ou", "se", "na", "em", "do", "da", "ao", "já", "também", "é", "e"},
	"en": {"the", "of", "and", "to", "in", "is", "that", "for", "it", "with", "as", "on", "be", "are", "this", "was", "or", "not", "by", "from", "at", "an", "which", "has", "must", "when", "about", "needs", "already"},
}

// Citation contexts carry code, not prose — strip them before counting so a
// snippet-heavy doc is judged by its actual sentences.
var (
	fencedRe = regexp.MustCompile("(?m)^```[^\n]*\n[\\s\\S]*?(^```|\\z)")
	inlineRe = regexp.MustCompile("`[^`\n]+`")
	wordRe   = regexp.MustCompile(`[\p{L}]+`)
)

// Detect returns "pt" or "en" for content, by stopword frequency over the
// prose (fenced blocks and inline code stripped first). Deterministic and
// total: insufficient or ambiguous content — including a tie — returns "pt",
// the project's default, never an error.
func Detect(content string) string {
	content = fencedRe.ReplaceAllString(content, "")
	content = inlineRe.ReplaceAllString(content, "")
	counts := map[string]int{}
	for _, w := range wordRe.FindAllString(strings.ToLower(content), -1) {
		for lang, words := range stopwords {
			for _, s := range words {
				if w == s {
					counts[lang]++
					break
				}
			}
		}
	}
	if counts["en"] > counts["pt"] {
		return "en"
	}
	return "pt"
}
