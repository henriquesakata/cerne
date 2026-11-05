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

// literalModelIDs walks roots and reports every literal model id outside what
// the contract allows: internal/models/baseline.json, Go tests (*_test.go),
// Go fixtures (testdata/) and vitest files (*.test.ts, *.test.tsx). Build
// output and dependencies (dist/, node_modules/) are not sources.
func literalModelIDs(t *testing.T, roots ...string) []string {
	t.Helper()
	var hits []string
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			name := d.Name()
			if d.IsDir() {
				if name == "node_modules" || name == "dist" || name == "testdata" {
					return filepath.SkipDir
				}
				return nil
			}
			abs, err := filepath.Abs(path)
			if err != nil {
				return err
			}
			if strings.HasSuffix(filepath.ToSlash(abs), "/internal/models/baseline.json") ||
				strings.HasSuffix(name, "_test.go") || strings.HasSuffix(name, ".test.ts") || strings.HasSuffix(name, ".test.tsx") {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for i, line := range strings.Split(string(data), "\n") {
				if m := modelID.FindString(line); m != "" {
					hits = append(hits, filepath.ToSlash(path)+":"+strconv.Itoa(i+1)+": "+m)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
	return hits
}

// TestNoLiteralModelIDsOutsideCatalog: criterion 1 — every surface reads the
// catalog; only the embedded baseline may name a model. Scans the Go tree and
// the UI sources.
func TestNoLiteralModelIDsOutsideCatalog(t *testing.T) {
	if hits := literalModelIDs(t, "..", "../../cmd", "../../ui/src"); len(hits) > 0 {
		t.Fatalf("literal model ids outside the catalog baseline:\n%s", strings.Join(hits, "\n"))
	}
}

// TestGuardSkipsOnlyWhatTheContractAllows: C9 — the guard's exemptions are
// exactly the baseline file, Go tests, testdata and vitest files; a literal in
// any other file — including production code next to the baseline — is a hit.
func TestGuardSkipsOnlyWhatTheContractAllows(t *testing.T) {
	root := t.TempDir()
	write := func(rel string) {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(`id := "claude-opus-9"`+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, rel := range []string{
		"internal/models/baseline.json",      // the catalog itself
		"internal/models/catalog_test.go",    // Go test
		"internal/lint/testdata/fixture.md",  // Go fixture
		"ui/src/Menu.test.tsx",               // vitest
		"ui/src/menu.test.ts",                // vitest
		"internal/models/catalog.go",         // production code beside the baseline: a hit
		"internal/server/config.go",          // a hit
		"ui/src/models.fixture.test.json.ts", // not a vitest name: a hit
	} {
		write(rel)
	}
	var got []string
	for _, h := range literalModelIDs(t, filepath.Join(root, "internal"), filepath.Join(root, "ui", "src")) {
		got = append(got, strings.TrimPrefix(strings.SplitN(h, ":", 2)[0], filepath.ToSlash(root)+"/"))
	}
	want := "internal/models/catalog.go,internal/server/config.go,ui/src/models.fixture.test.json.ts"
	if strings.Join(got, ",") != want {
		t.Fatalf("guard hits = %v, want %s", got, want)
	}
}
