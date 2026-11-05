package models

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// modelID matches a concrete model id (claude-opus-5, claude-3-5-sonnet,
// gpt-5, gemini-2) — not a provider or tool name (claude, claude-cli, gemini).
var modelID = regexp.MustCompile(`\b(claude-[a-z]+-\d|claude-\d|gpt-\d|gemini-\d)`)

// TestNoLiteralModelIDsOutsideCatalog: criterion 1 — every surface reads the
// catalog; only the embedded baseline may name a model. Scans the Go tree and
// the UI sources (tests and the built dist excluded).
func TestNoLiteralModelIDsOutsideCatalog(t *testing.T) {
	roots := []string{"..", "../../cmd", "../../ui/src"}
	var hits []string
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			name := d.Name()
			if d.IsDir() {
				if name == "node_modules" || name == "dist" || path == filepath.Join("..", "models") {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.HasSuffix(name, "_test.go") || strings.Contains(name, ".test.") {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for i, line := range strings.Split(string(data), "\n") {
				if m := modelID.FindString(line); m != "" {
					hits = append(hits, path+":"+strconv.Itoa(i+1)+": "+m)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
	if len(hits) > 0 {
		t.Fatalf("literal model ids outside the catalog baseline:\n%s", strings.Join(hits, "\n"))
	}
}
