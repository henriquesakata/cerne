package store

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Meta is what every entity file carries whatever its kind: what a list of
// mixed kinds shows (the task package, E-0005).
type Meta struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	State string `json:"state"`
}

// metaFile is an entity file as LoadMeta reads it: the title and what labels
// an entity without one.
type metaFile struct {
	Meta
	Name string `json:"name"` // a historical
	Body string `json:"body"` // an idea, a pending
	Next string `json:"next"` // a handoff
}

// LoadMeta reads the id, title and state of the entity id of any kind, by its
// prefix; ErrNotFound for an unknown prefix or a missing file. An entity
// without a title is labelled by its name, the first line of its body, its
// next step, or "(untitled)". kind is the singular of its entity dir
// ("feature" for E-/T-/B- too).
func LoadMeta(backendDir, id string) (m Meta, kind string, err error) {
	prefix, _, _ := strings.Cut(id, "-")
	sub, ok := idSubdirs[prefix]
	if !ok {
		return Meta{}, "", fmt.Errorf("%s: %w", id, ErrNotFound)
	}
	f, err := loadEntity[metaFile](backendDir, sub, id)
	m = f.Meta
	first, _, _ := strings.Cut(strings.TrimSpace(f.Body), "\n")
	switch {
	case m.Title != "":
	case f.Name != "":
		m.Title = f.Name
	case first != "":
		m.Title = first
	case f.Next != "":
		m.Title = "next: " + f.Next
	default:
		m.Title = "(untitled)"
	}
	kind = strings.TrimSuffix(path.Base(sub), "s")
	if kind == "capabilitie" {
		kind = "capability"
	}
	return m, kind, err
}

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
