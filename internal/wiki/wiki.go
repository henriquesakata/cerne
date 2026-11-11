// Package wiki holds the pure model of the compiled synthesis pages: their
// provenance frontmatter and the input hash that drives incremental
// recompilation. No I/O, no LLM — just data and hashing.
package wiki

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/henriquesakata/cerne/internal/doccheck"
)

// Source is one live entity feeding a wiki page. ContentHash is a digest of
// the entity's content (empty when it has none); State is the lifecycle state.
type Source struct {
	ID          string
	State       string
	ContentHash string
}

// InputHash is a stable, order-independent digest of a page's sources. It
// changes when an entity's state or content changes, or when one is added or
// removed — exactly the conditions that make a compiled page stale.
func InputHash(sources []Source) string {
	cp := append([]Source(nil), sources...)
	sort.Slice(cp, func(i, j int) bool { return cp[i].ID < cp[j].ID })
	h := sha256.New()
	for _, s := range cp {
		fmt.Fprintf(h, "%s|%s|%s\n", s.ID, s.State, s.ContentHash)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Provenance is the frontmatter cerne stamps on a compiled wiki page.
type Provenance struct {
	Page         string   // module name, or "project" for the cross-cutting page
	Sources      []string // entity IDs that fed the page
	CompiledFrom string   // backend HEAD sha at compile time
	InputHash    string   // InputHash of the sources, for dirty-tracking
}

// RenderProvenance renders the leading "---" frontmatter block (with a
// trailing newline) for a compiled page.
func RenderProvenance(p Provenance) string {
	return fmt.Sprintf("---\ncerne_wiki: %s\nsources: [%s]\ncompiled_from: %s\ninput_hash: %s\n---\n",
		p.Page, strings.Join(p.Sources, ", "), p.CompiledFrom, p.InputHash)
}

// ParseProvenance reads the frontmatter of a compiled page back into a
// Provenance. Missing fields come back empty; sources is nil when absent.
func ParseProvenance(data []byte) Provenance {
	p := Provenance{
		Page:         doccheck.FrontmatterValue(data, "cerne_wiki"),
		CompiledFrom: doccheck.FrontmatterValue(data, "compiled_from"),
		InputHash:    doccheck.FrontmatterValue(data, "input_hash"),
	}
	raw := strings.TrimSpace(doccheck.FrontmatterValue(data, "sources"))
	raw = strings.TrimSuffix(strings.TrimPrefix(raw, "["), "]")
	for _, part := range strings.Split(raw, ",") {
		if s := strings.TrimSpace(part); s != "" {
			p.Sources = append(p.Sources, s)
		}
	}
	return p
}
