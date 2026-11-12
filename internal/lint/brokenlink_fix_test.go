package lint

import (
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/store"
)

// F-0352 crit. 2: an unknown id of a known prefix names the last id of it.
func TestBrokenLinkSuggestsLastID(t *testing.T) {
	dir := brokenLinkBackend(t, "# Spec\n\n[[N-0009]] e [[Z-0001]]\n", "N-0001", "N-0003", "N-0002")
	got := brokenLinks(t, dir)
	if len(got) != 2 {
		t.Fatalf("findings = %+v, want 2", got)
	}
	if want := "line 7: broken wiki-link [[N-0009]] — no such id; the last N- id is N-0003"; got[0].Detail != want {
		t.Errorf("detail = %q, want %q", got[0].Detail, want)
	}
	if strings.Contains(got[1].Detail, " — ") {
		t.Errorf("unknown prefix Z- must carry no suggestion, got %q", got[1].Detail)
	}
}

// F-0352 crit. 3: a different zero padding (or case) gets the right form.
func TestBrokenLinkSuggestsPadding(t *testing.T) {
	dir := brokenLinkBackend(t, "# Spec\n\n[[f-12]] [[F-12]] [[F-0012]]\n", "F-0012")
	got := brokenLinks(t, dir)
	if len(got) != 2 {
		t.Fatalf("findings = %+v, want 2 (the exact id resolves)", got)
	}
	for _, target := range []string{"f-12", "F-12"} {
		want := "line 7: broken wiki-link [[" + target + "]] — did you mean [[F-0012]]?"
		if got[0].Detail != want && got[1].Detail != want {
			t.Errorf("no finding %q in %+v", want, got)
		}
	}
}

// F-0352 crit. 4: a missing file with exactly one same-named file in the
// backend is suggested; with several candidates there is no guess.
func TestBrokenLinkSuggestsPath(t *testing.T) {
	dir := brokenLinkBackend(t, "# Spec\n\n[a](./a.md) [b](./b.md) [c](./c.md)\n")
	writeDoc(t, dir, "content/old/a.md", "# a\n")
	writeDoc(t, dir, "content/old/b.md", "# b\n")
	writeDoc(t, dir, "content/other/b.md", "# b\n")
	got := brokenLinks(t, dir)
	if len(got) != 3 {
		t.Fatalf("findings = %+v, want 3", got)
	}
	if want := "line 7: broken link ./a.md — did you mean content/old/a.md?"; got[0].Detail != want {
		t.Errorf("detail = %q, want %q", got[0].Detail, want)
	}
	for _, f := range got[1:] {
		if strings.Contains(f.Detail, " — ") {
			t.Errorf("ambiguous or missing name must carry no suggestion, got %q", f.Detail)
		}
	}
}

// F-0352 crit. 5: in an append-only errata the finding carries the errata
// command in Fix, like ref-to-superseded.
func TestBrokenLinkErrataFix(t *testing.T) {
	dir := brokenLinkBackend(t, "# Spec\n\ntexto\n")
	if err := store.AppendErrata(dir, "F-0001", "cita [[N-9999]]", "ator", time.Unix(0, 0).UTC()); err != nil {
		t.Fatalf("errata: %v", err)
	}
	var errata *Finding
	for _, f := range brokenLinks(t, dir) {
		if f.File == "content/errata/F-0001.md" {
			f := f
			errata = &f
		}
	}
	if errata == nil {
		t.Fatal("no broken-link finding in the errata")
	}
	if !strings.HasPrefix(errata.Fix, "cerne feature errata F-0001 ") {
		t.Errorf("Fix = %q, want a `cerne feature errata F-0001 …` command", errata.Fix)
	}
	if !strings.Contains(errata.Fix, "[[N-9999]]") || errata.Line == 0 {
		t.Errorf("Fix %q must name the link and Line be set (%d)", errata.Fix, errata.Line)
	}
}
