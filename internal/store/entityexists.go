package store

import (
	"os"
	"path"
	"path/filepath"
	"strings"
)

// EntityExists reports whether the backend holds an entity with this id
// (F-/T-/E-/D-/R-/RB-/N-/H-/W-/C-/P-/I-/HO-), by its entity file. Read-only; an
// unknown prefix or a malformed id is simply "no".
func EntityExists(backendDir, id string) bool {
	prefix, _, ok := strings.Cut(id, "-")
	if !ok || strings.ContainsAny(id, `/\`) {
		return false
	}
	sub, ok := idSubdirs[prefix]
	if !ok {
		return false
	}
	st, err := os.Stat(filepath.Join(backendDir, filepath.FromSlash(sub), id+".json"))
	return err == nil && st.Mode().IsRegular()
}

// EntityIDFromContentPath maps a backend-relative slash path of a content doc
// to the id of the entity that owns it (content/features/<ID>/spec.md,
// content/notes/<ID>.md, content/works/<ID>/handoff.md, ...), or "" when the
// path is not under the content dir of the id's own kind. The kinds and their
// dirs come from the layout table (idSubdirs), so a new kind needs no edit here.
func EntityIDFromContentPath(rel string) string {
	rest, ok := strings.CutPrefix(rel, "content/")
	if !ok {
		return ""
	}
	dir, rest, ok := strings.Cut(rest, "/")
	if !ok {
		return ""
	}
	id, _, nested := strings.Cut(rest, "/")
	if !nested {
		if id, ok = strings.CutSuffix(id, ".md"); !ok {
			return ""
		}
	}
	prefix, _, ok := strings.Cut(id, "-")
	if !ok || path.Base(idSubdirs[prefix]) != dir {
		return ""
	}
	return id
}

// EntityIDs lists the id of every entity file in the backend, of every kind in
// the layout table. Unreadable dirs and non-entity files are skipped.
func EntityIDs(backendDir string) []string {
	var ids []string
	for prefix, sub := range idSubdirs {
		ents, err := os.ReadDir(filepath.Join(backendDir, filepath.FromSlash(sub)))
		if err != nil {
			continue
		}
		for _, e := range ents {
			if id, ok := strings.CutSuffix(e.Name(), ".json"); ok && e.Type().IsRegular() && strings.HasPrefix(id, prefix+"-") {
				ids = append(ids, id)
			}
		}
	}
	return ids
}
