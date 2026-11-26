package e2e_test

import (
	"fmt"
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
