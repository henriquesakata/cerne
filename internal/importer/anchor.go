package importer

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/henriquesakata/cerne/internal/doccheck"
)

const (
	maxEvidence      = 3
	reasonSnippetCap = 80
	// ponytail: a source over 10 MiB is left out unread (no real doc is that big;
	// the cap keeps a hostile or accidental huge file out of memory). Raise it or
	// stream the search if a legitimate source ever needs more.
	maxSourceBytes = 10 << 20
)

// Span is a line range (1-based, inclusive) of a source file.
type Span struct {
	File       string
	Start, End int
}

// String renders the span as "file:start-end".
func (s Span) String() string { return fmt.Sprintf("%s:%d-%d", s.File, s.Start, s.End) }

// AnchoredItem is a plan item whose evidence was found literally in its source.
type AnchoredItem struct {
	PlanItem
	Spans []Span
}

// AnchorText renders the spans for the frontmatter and the event: a
// comma-separated "file:start-end" list, empty when there are none.
func (a AnchoredItem) AnchorText() string {
	parts := make([]string, len(a.Spans))
	for i, s := range a.Spans {
		parts[i] = s.String()
	}
	return strings.Join(parts, ", ")
}

// Unanchored is a plan item left out of the import, with the reason.
type Unanchored struct {
	Item   PlanItem
	Reason string
}

// AnchorPlanItems splits items by whether their evidence appears literally in
// the source file under root (the part of Source before any '#'). Order is
// preserved in both results. A source that cannot be read is that item's
// problem (unanchored), never an error of the call; a source outside root counts
// as not found, never read.
func AnchorPlanItems(root string, items []PlanItem) (anchored []AnchoredItem, unanchored []Unanchored, err error) {
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, nil, fmt.Errorf("resolving root %s: %w", root, err)
	}
	cache := map[string]loadedSource{} // each file is read and normalized once
	for _, it := range items {
		switch {
		case len(it.Evidence) == 0:
			unanchored = append(unanchored, Unanchored{it, "no evidence"})
			continue
		case len(it.Evidence) > maxEvidence:
			unanchored = append(unanchored, Unanchored{it, "more than 3 evidence snippets"})
			continue
		}
		file, _, _ := strings.Cut(it.Source, "#")
		src, ok := cache[file]
		if !ok {
			src = loadSource(rootReal, file)
			cache[file] = src
		}
		if src.reason != "" {
			unanchored = append(unanchored, Unanchored{it, src.reason})
			continue
		}
		a := AnchoredItem{PlanItem: it}
		reason := ""
		for _, ev := range it.Evidence {
			s, e, ok := src.index.Find(ev)
			if !ok {
				reason = fmt.Sprintf("evidence not found in %s: %q", file, cutRunes(ev, reasonSnippetCap))
				break
			}
			a.Spans = append(a.Spans, Span{File: file, Start: s, End: e})
		}
		if reason != "" {
			unanchored = append(unanchored, Unanchored{it, reason})
			continue
		}
		anchored = append(anchored, a)
	}
	return anchored, unanchored, nil
}

// loadedSource is a source file ready for lookups, or the reason it is not.
type loadedSource struct {
	index  *doccheck.LiteralIndex
	reason string
}

// loadSource reads file under rootReal (already symlink-resolved). The file must
// resolve, symlinks followed, to a regular file inside rootReal and be at most
// maxSourceBytes; anything outside is "not found", never read. A symlink
// inside root to a file inside root is fine (CLAUDE.md -> AGENTS.md).
func loadSource(rootReal, file string) loadedSource {
	notFound := loadedSource{reason: "source not found: " + file}
	if !filepath.IsLocal(file) {
		return notFound
	}
	p, err := filepath.EvalSymlinks(filepath.Join(rootReal, filepath.FromSlash(file)))
	if errors.Is(err, fs.ErrNotExist) {
		return notFound
	}
	if err != nil {
		return loadedSource{reason: "source unreadable: " + file}
	}
	if rel, err := filepath.Rel(rootReal, p); err != nil || !filepath.IsLocal(rel) {
		return notFound
	}
	st, err := os.Stat(p)
	if err != nil {
		return loadedSource{reason: "source unreadable: " + file}
	}
	if !st.Mode().IsRegular() {
		return notFound
	}
	if st.Size() > maxSourceBytes {
		return loadedSource{reason: "source too large: " + file}
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return loadedSource{reason: "source unreadable: " + file}
	}
	return loadedSource{index: doccheck.NewLiteralIndex(string(data))}
}

// cutRunes truncates s to n runes, marking the cut with an ellipsis.
func cutRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
