package app

import (
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/henriquesakata/cerne/internal/doccheck"
	"github.com/henriquesakata/cerne/internal/store"
)

// Drop reasons of FilterLinkSuggestions (F-0502).
const (
	LinkDropUnknownSource = "unknown source"
	LinkDropUnknownTarget = "unknown target"
	LinkDropSelf          = "source is target"
	LinkDropExcerpt       = "excerpt not in the source"
	LinkDropShort         = "excerpt too short"
	LinkDropEdge          = "edge already exists"
	LinkDropQueued        = "pair already suggested"
	LinkDropDuplicate     = "duplicate in the batch"
)

// minLinkExcerptRunes is the shortest normalized excerpt that justifies a
// relation, the importer's minimum for evidence (F-0500).
const minLinkExcerptRunes = 12

// RawLinkSuggestion is one relation as the LLM returned it.
type RawLinkSuggestion struct {
	From    string `json:"from"`
	To      string `json:"to"`
	Excerpt string `json:"excerpt"`
}

// FilterLinkSuggestions keeps the suggestions that pass the deterministic
// checks, without any LLM: both ends exist, the excerpt is literally in a
// content document of the source (doccheck.FindLiteral), no edge of any type
// already runs from the source to the target, the pair was never suggested
// before (in any status) and it is not repeated in the batch. dropped counts
// the rest by reason.
func FilterLinkSuggestions(repoDir string, raw []RawLinkSuggestion) (kept []RawLinkSuggestion, dropped map[string]int, err error) {
	g, err := BuildGraph(repoDir)
	if err != nil {
		return nil, nil, err
	}
	nodes := map[string]bool{}
	for _, n := range g.Nodes {
		nodes[n.ID] = true
	}
	edges := map[[2]string]bool{}
	for _, e := range g.Edges {
		edges[[2]string{e.From, e.To}] = true
	}
	queue, err := store.LoadLinkSuggestions(repoDir)
	if err != nil {
		return nil, nil, err
	}
	seen := map[[2]string]bool{}
	for _, s := range queue {
		seen[[2]string{s.From, s.To}] = true
	}
	docs, err := allContentDocs(repoDir)
	if err != nil {
		return nil, nil, err
	}
	text := map[string][]*doccheck.LiteralIndex{} // source id → one index per document, built on demand
	read := func(id string) ([]*doccheck.LiteralIndex, error) {
		if t, ok := text[id]; ok {
			return t, nil
		}
		var out []*doccheck.LiteralIndex
		for _, d := range docs {
			if d.id != id {
				continue
			}
			b, err := os.ReadFile(filepath.Join(repoDir, filepath.FromSlash(d.rel)))
			if err != nil {
				return nil, err
			}
			out = append(out, doccheck.NewLiteralIndex(string(b)))
		}
		text[id] = out
		return out, nil
	}

	dropped = map[string]int{}
	batch := map[[2]string]bool{}
	for _, r := range raw {
		pair := [2]string{r.From, r.To}
		reason := ""
		switch {
		case !nodes[r.From]:
			reason = LinkDropUnknownSource
		case !nodes[r.To]:
			reason = LinkDropUnknownTarget
		case r.From == r.To:
			reason = LinkDropSelf
		case edges[pair]:
			reason = LinkDropEdge
		case seen[pair]:
			reason = LinkDropQueued
		case batch[pair]:
			reason = LinkDropDuplicate
		}
		if reason == "" && utf8.RuneCountInString(strings.Join(strings.Fields(r.Excerpt), " ")) < minLinkExcerptRunes {
			reason = LinkDropShort
		}
		if reason == "" {
			found := false
			contents, err := read(r.From)
			if err != nil {
				return nil, nil, err
			}
			for _, c := range contents {
				if _, _, ok := c.Find(r.Excerpt); ok {
					found = true
					break
				}
			}
			if !found {
				reason = LinkDropExcerpt
			}
		}
		if reason != "" {
			dropped[reason]++
			continue
		}
		batch[pair] = true
		kept = append(kept, r)
	}
	return kept, dropped, nil
}

// allContentDocs lists the content documents of every entity in the backend.
func allContentDocs(repoDir string) ([]contentDoc, error) {
	features, err := store.LoadAllFeatures(repoDir)
	if err != nil {
		return nil, err
	}
	decisions, err := store.LoadAllDecisions(repoDir)
	if err != nil {
		return nil, err
	}
	references, err := store.LoadAllReferences(repoDir)
	if err != nil {
		return nil, err
	}
	notes, err := store.LoadAllNotes(repoDir)
	if err != nil {
		return nil, err
	}
	runbooks, err := store.LoadAllRunbooks(repoDir)
	if err != nil {
		return nil, err
	}
	return contentDocs(repoDir, features, decisions, references, notes, runbooks)
}
