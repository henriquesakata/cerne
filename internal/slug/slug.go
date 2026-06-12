// Package slug converts human-readable names into filesystem-safe slugs.
package slug

import "strings"

// Make lowercases name, maps spaces to "-" and strips every rune
// outside [a-z0-9-].
func Make(name string) string {
	s := strings.ReplaceAll(strings.ToLower(name), " ", "-")
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
