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

// Unanchored is a plan item left out of the import, with the reason.
type Unanchored struct {
	Item   PlanItem
	Reason string
}

// AnchorPlanItems splits items by whether their evidence appears literally in
// the source file under root (the part of Source before any '#'). Order is
// preserved in both results. err is only for I/O failures other than a missing
// file; a source outside root counts as not found, never read.
func AnchorPlanItems(root string, items []PlanItem) (anchored []AnchoredItem, unanchored []Unanchored, err error) {
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
		var content []byte
		if filepath.IsLocal(file) {
			content, err = os.ReadFile(filepath.Join(root, filepath.FromSlash(file)))
		} else {
			err = fs.ErrNotExist
		}
		if errors.Is(err, fs.ErrNotExist) {
			unanchored = append(unanchored, Unanchored{it, "source not found: " + file})
			err = nil
			continue
		}
		if err != nil {
			return nil, nil, fmt.Errorf("reading source %s: %w", file, err)
		}
		a := AnchoredItem{PlanItem: it}
		reason := ""
		for _, ev := range it.Evidence {
			s, e, ok := doccheck.FindLiteral(string(content), ev)
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

// cutRunes truncates s to n runes, marking the cut with an ellipsis.
func cutRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
