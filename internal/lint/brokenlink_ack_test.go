package lint

import (
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/store"
)

func errataBrokenLinks(t *testing.T, dir string) []Finding {
	t.Helper()
	var out []Finding
	for _, f := range brokenLinks(t, dir) {
		if f.File == "content/errata/F-0001.md" {
			out = append(out, f)
		}
	}
	return out
}

func appendErr(t *testing.T, dir string, day int, text string) {
	t.Helper()
	if err := store.AppendErrata(dir, "F-0001", text, "ator", time.Date(2026, 1, day, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("errata: %v", err)
	}
}

// F-0352 A1: the Fix mirrors ref-to-superseded: a marker entry, the target
// never as a wiki-link.
func TestBrokenLinkErrataFixText(t *testing.T) {
	dir := brokenLinkBackend(t, "# Spec\n\ntexto\n")
	appendErr(t, dir, 1, "cita [[N-9999]]")
	got := errataBrokenLinks(t, dir)
	const want = "cerne feature errata F-0001 '<!-- lint:ok-broken-link N-9999 --> <the right target, or why the link is history>'"
	if len(got) != 1 || got[0].Fix != want {
		t.Fatalf("got %+v, want Fix %q", got, want)
	}
}

// F-0352 A1 (a)+(b)+(c): applying the Fix clears earlier occurrences; a new
// occurrence after the marker is a finding again; the marker line itself is
// not an occurrence.
func TestBrokenLinkErrataAckMarker(t *testing.T) {
	dir := brokenLinkBackend(t, "# Spec\n\ntexto\n")
	appendErr(t, dir, 1, "cita [[N-9999]]")
	appendErr(t, dir, 2, "de novo [[N-9999]] e [x](./nada.md)")
	if got := errataBrokenLinks(t, dir); len(got) != 3 {
		t.Fatalf("before the Fix: %+v, want 3", got)
	}
	appendErr(t, dir, 3, "<!-- lint:ok-broken-link N-9999 --> era id errado, histórico")
	got := errataBrokenLinks(t, dir)
	if len(got) != 1 || got[0].Line != 11 {
		t.Fatalf("after the Fix only the unmarked link must remain, got %+v", got)
	}
	appendErr(t, dir, 4, "outra vez [[N-9999]]")
	if got := errataBrokenLinks(t, dir); len(got) != 2 {
		t.Fatalf("a new occurrence after the marker must be a finding, got %+v", got)
	}
	appendErr(t, dir, 5, "mesma linha [[N-9999]] <!-- lint:ok-broken-link N-9999 -->")
	if got := errataBrokenLinks(t, dir); len(got) != 1 {
		t.Fatalf("same-line marker must clear its own occurrence, got %+v", got)
	}
}
