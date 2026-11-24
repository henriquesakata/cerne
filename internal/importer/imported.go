package importer

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/henriquesakata/cerne/internal/doccheck"
	"github.com/henriquesakata/cerne/internal/store"
)

// ImportedSources returns the set of imported_from sources already present in
// the backend's content tree, so a re-run of import can skip them. Documents
// without the frontmatter key (hand-authored) contribute nothing; a missing
// content/ directory yields an empty set.
func ImportedSources(repoDir string) (map[string]bool, error) {
	sources := map[string]bool{}
	err := walkImported(repoDir, func(doc []byte) {
		if src := doccheck.FrontmatterValue(doc, "imported_from"); src != "" {
			sources[src] = true
		}
	})
	return sources, err
}

// importedItemKey identifies an agent-imported item by its origin and title.
func importedItemKey(source, title string) string {
	return source + "\x00" + normalizeTitle(title)
}

// importedItems returns the (imported_from, normalized title) keys of every
// imported entity, the idempotency unit of the agent path: one source may yield
// several entities, and only some of them may have been applied (F-0500).
func importedItems(repoDir string) (map[string]bool, error) {
	items := map[string]bool{}
	var firstErr error
	err := walkImported(repoDir, func(doc []byte) {
		src := doccheck.FrontmatterValue(doc, "imported_from")
		id := doccheck.FrontmatterValue(doc, "cerne_id")
		if src == "" || id == "" {
			return
		}
		m, _, err := store.LoadMeta(repoDir, id)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("loading imported entity %s: %w", id, err)
			}
			return
		}
		items[importedItemKey(src, m.Title)] = true
	})
	if err == nil {
		err = firstErr
	}
	return items, err
}

// walkImported calls fn with every markdown document under the backend's
// content tree; a missing content/ directory visits nothing.
func walkImported(repoDir string, fn func(doc []byte)) error {
	root := filepath.Join(repoDir, "content")
	return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		fn(data)
		return nil
	})
}

// Unimported splits cands into those whose Source is not yet in imported
// (kept, original order preserved) and counts the rest as skipped. It lets a
// re-run of import bring in only the documents added since the last run.
func Unimported(cands []Candidate, imported map[string]bool) (kept []Candidate, skipped int) {
	for _, c := range cands {
		if imported[c.Source] {
			skipped++
			continue
		}
		kept = append(kept, c)
	}
	return kept, skipped
}
