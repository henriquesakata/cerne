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

// ContentLinks parses the inline markdown links of content and classifies each
// distinct target with resolve. External (http/https/mailto), pure-anchor and
// absolute targets are skipped; a #fragment suffix is stripped before resolve
// is called. Targets resolve reports present go to resolved (as the id resolve
// returns); the rest go to broken (the original target text, fragment kept).
// Each distinct raw target is considered once, in first-appearance order.
func ContentLinks(content string, resolve func(target string) (id string, ok bool)) (resolved, broken []string) {
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
