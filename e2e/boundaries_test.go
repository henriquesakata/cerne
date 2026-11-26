package e2e_test

import (
	"errors"
	"fmt"
	"go/build"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// F-0519: the layer boundaries the CLAUDE.md package table promises, checked
// against the import graph. One table of five rules, one table of named
// exceptions, each row with its reason.

type boundaryKind int

const (
	forbid         boundaryKind = iota // pkg must not import any of list
	onlyModule                         // pkg may import only list from this module
	importedOnlyBy                     // only the packages in list may import pkg
)

type boundaryRule struct {
	pkg  string
	kind boundaryKind
	list []string
	why  string
}

// boundaryException names an import that is allowed although a rule would
// flag it, or that the table would otherwise leave unexplained.
type boundaryException struct {
	pkg, imp, why string
}

var boundaryRules = []boundaryRule{
	{"internal/domain", onlyModule, []string{"internal/slug"},
		"domain is the pure core and may depend on no other package of the module but slug"},
	{"internal/domain", forbid, []string{"os", "io", "io/fs", "net", "net/http", "os/exec", "database/sql"},
		"domain does no file, network, process or database I/O (crypto/rand is allowed: entropy for ids)"},
	{"internal/store", forbid, []string{"internal/app", "internal/cli", "internal/server"},
		"store sits below app: persistence must not know the layers that call it"},
	{"internal/cli", importedOnlyBy, []string{"cmd/cerne"},
		"cli is the outermost layer: only the main package may import it"},
	{"internal/server", importedOnlyBy, []string{"internal/cli"},
		"server is reached through the cli (cerne ui): no lower package may import it"},
	{"internal/app", forbid, []string{"internal/cli", "internal/server"},
		"app is the shared core of cli and server: it must not depend on either"},
}

var boundaryExceptions = []boundaryException{
	{"internal/project", "internal/tracker",
		"project owns .cernerc and the settings catalog: it decodes [[tracker]] records into tracker.Config and validates them with tracker.New, and lists tracker.KindNames as the kind enum"},
	{"internal/compile", "internal/app",
		"compile rewrites references to a superseded entity before prompting, through app.TerminalSuccessor, the one resolver of the supersession chain"},
}

const modulePath = "github.com/henriquesakata/cerne/"

func isModuleImport(imp string) bool {
	return strings.HasPrefix(imp, "internal/") || strings.HasPrefix(imp, "cmd/")
}

// checkBoundaries returns one sorted line per violation:
// "<package> imports <import>: <rule reason>".
func checkBoundaries(graph map[string][]string, rules []boundaryRule, exceptions []boundaryException) []string {
	var out []string
	add := func(pkg, imp, why string) {
		for _, e := range exceptions {
			if e.pkg == pkg && e.imp == imp {
				return
			}
		}
		out = append(out, fmt.Sprintf("%s imports %s: %s", pkg, imp, why))
	}
	for _, r := range rules {
		switch r.kind {
		case forbid:
			for _, imp := range graph[r.pkg] {
				if slices.Contains(r.list, imp) {
					add(r.pkg, imp, r.why)
				}
			}
		case onlyModule:
			for _, imp := range graph[r.pkg] {
				if isModuleImport(imp) && !slices.Contains(r.list, imp) {
					add(r.pkg, imp, r.why)
				}
			}
		case importedOnlyBy:
			for pkg, imps := range graph {
				if slices.Contains(imps, r.pkg) && !slices.Contains(r.list, pkg) {
					add(pkg, r.pkg, r.why)
				}
			}
		}
	}
	slices.Sort(out)
	return out
}

// F-0519 crit. 7
func TestBoundaryViolationMessage(t *testing.T) {
	graph := map[string][]string{
		"internal/domain": {"internal/slug", "os"},
		"internal/app":    {"internal/server"},
		"internal/server": nil,
		"internal/store":  {"internal/app"},
	}
	exceptions := []boundaryException{{"internal/store", "internal/app", "test exception"}}
	got := checkBoundaries(graph, boundaryRules, exceptions)
	want := []string{
		"internal/app imports internal/server: " + boundaryRules[5].why,
		"internal/app imports internal/server: " + boundaryRules[4].why,
		"internal/domain imports os: " + boundaryRules[1].why,
	}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("violations:\n got  %q\n want %q", got, want)
	}
}

// F-0519 crit. 6
func TestBoundaryRulesHaveReason(t *testing.T) {
	for _, r := range boundaryRules {
		if strings.TrimSpace(r.why) == "" || len(r.list) == 0 {
			t.Errorf("rule for %s has no reason or no list", r.pkg)
		}
	}
	for _, e := range boundaryExceptions {
		if strings.TrimSpace(e.why) == "" {
			t.Errorf("exception %s -> %s has no reason", e.pkg, e.imp)
		}
	}
}

// buildImportGraph maps each package directory under internal/ and cmd/
// (module-relative) to its non-test imports, module imports made
// module-relative. It unions GOOS linux, darwin and windows so a build
// constraint cannot hide an import.
func buildImportGraph(t *testing.T, root string) map[string][]string {
	t.Helper()
	graph := map[string][]string{}
	for _, top := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, top), func(path string, d fs.DirEntry, err error) error {
			if err != nil || !d.IsDir() {
				return err
			}
			// The go tool ignores testdata and dirs starting with _ or . (go help packages).
			if n := d.Name(); path != filepath.Join(root, top) && (n == "testdata" || strings.HasPrefix(n, "_") || strings.HasPrefix(n, ".")) {
				return filepath.SkipDir
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			seen := map[string]bool{}
			found := false
			for _, goos := range []string{"linux", "darwin", "windows"} {
				ctx := build.Default
				ctx.GOOS, ctx.CgoEnabled = goos, false
				pkg, err := ctx.ImportDir(path, 0)
				var noGo *build.NoGoError
				if errors.As(err, &noGo) {
					continue
				}
				if err != nil {
					return fmt.Errorf("%s (%s): %w", rel, goos, err)
				}
				found = true
				for _, imp := range pkg.Imports {
					seen[strings.TrimPrefix(imp, modulePath)] = true
				}
			}
			if found {
				imps := make([]string, 0, len(seen))
				for imp := range seen {
					imps = append(imps, imp)
				}
				slices.Sort(imps)
				graph[rel] = imps
			}
			return nil
		})
		if err != nil {
			t.Fatalf("reading imports under %s: %v", top, err)
		}
	}
	return graph
}

// F-0519 crit. 1
// F-0519 crit. 2
// F-0519 crit. 3
// F-0519 crit. 4
// F-0519 crit. 5
func TestPackageBoundaries(t *testing.T) {
	graph := buildImportGraph(t, "..")
	for _, anchor := range []string{"internal/domain", "internal/cli", "cmd/cerne"} {
		if _, ok := graph[anchor]; !ok {
			t.Fatalf("import graph lacks %s (wrong root or empty graph): %d packages", anchor, len(graph))
		}
	}
	for _, v := range checkBoundaries(graph, boundaryRules, boundaryExceptions) {
		t.Error(v)
	}
	for _, e := range boundaryExceptions {
		if !slices.Contains(graph[e.pkg], e.imp) {
			t.Errorf("stale exception: %s no longer imports %s; remove it from boundaryExceptions", e.pkg, e.imp)
		}
	}
}

// F-0519 crit. 8
func TestClaudeMDCitesBoundaryTest(t *testing.T) {
	b, err := os.ReadFile("../CLAUDE.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "e2e/boundaries_test.go") {
		t.Error("CLAUDE.md must say the layer boundaries are checked by e2e/boundaries_test.go")
	}
}
