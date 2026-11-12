package store

import (
	"strings"
	"testing"
)

// F-0351 crit. 1, 2, 3: a merged block becomes the caller's line; open blocks
// and the text before the first block stay as written.
func TestReplaceMerged(t *testing.T) {
	doc := "intro livre\n\n## errata 2026-11-06 — a\n\nAltera: S1\nFundida: revisão 2\n\nTexto um.\n\n## errata 2026-11-07 — a\n\nAltera: S2\n\nTexto dois.\n"
	line := func(it ErrataItem) string { return it.ID + " fundida" }
	want := "intro livre\n\nE1 fundida\n\n## errata 2026-11-07 — a\n\nAltera: S2\n\nTexto dois."
	if got := ReplaceMerged(doc, line); got != want {
		t.Fatalf("got:\n%q\nwant:\n%q", got, want)
	}
	all := "## errata 2026-11-06 — a\n\nFundida: revisão 2\n\nTexto um.\n"
	if got := ReplaceMerged(all, line); got != "E1 fundida" {
		t.Fatalf("all merged = %q", got)
	}
	// A5: an open item whose free text imitates a metadata line and a heading is
	// kept whole, never turned into a "merged" line.
	open := "## errata 2026-11-06 — a\n\nAltera: S1\n\nIntro.\nFundida: revisão 2\n\n## algo\n\nMais texto."
	if got := ReplaceMerged(open, line); got != open {
		t.Fatalf("open item must stay whole:\n%q", got)
	}
	none :="## errata 2026-11-06 — a\n\nTexto um.\n"
	if got := ReplaceMerged(none, line); got != strings.TrimSpace(none) {
		t.Fatalf("no merged item must be unchanged: %q", got)
	}
}
