package store

import (
	"os"
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
