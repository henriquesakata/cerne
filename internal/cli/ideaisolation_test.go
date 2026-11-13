package cli

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/compile"
)

// TestIdeasLeaveFunnelOutputsUnchanged (F-0312 crit. 2 and 3): ideas are raw
// material, never a demand — every funnel output (lint, next, status, stats,
// brief, search, graph, the .docs/ projection incl. CONTEXT.md and the OKF
// log.md, the compiled wiki's pages) is byte-identical before and after
// capturing and dropping ideas. A pin: isolation holds by construction (typed
// loaders + the funnel log without idea.*); the teeth were proven by
// reverting store.ReadEvents to the raw log (fails on .docs/log.md).
func TestIdeasLeaveFunnelOutputsUnchanged(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	for _, args := range [][]string{
		{"feature", "new", "Exportar relatorio", "--facet", "code", "--context", "conciliação bancária mensal"},
		{"feature", "new", "Sem tag nenhuma", "--context", "ctx"},
	} {
		if out, err := execCerne(t, append(args, "--dir", projDir)...); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}
	snap := func() map[string]string {
		t.Helper()
		got := map[string]string{}
		for _, args := range [][]string{
			{"lint"}, {"next", "--json"}, {"status", "--json"}, {"stats", "--json"}, {"brief", "--json"},
			{"search", "zebra", "--json"}, {"search", "conciliação", "--json"}, {"graph"},
		} {
			out, err := execCerne(t, append(args, "--dir", projDir)...)
			// index_synced_at moves with every write, ideas included, by design (F-0394).
			out = syncedAtLine.ReplaceAllString(out, "")
			got[strings.Join(args, " ")] = fmt.Sprintf("%s\nerr=%v", out, err)
		}
		if out, err := execCerne(t, "pull", "--okf", "--dir", projDir); err != nil {
			t.Fatalf("pull --okf: %v\n%s", err, out)
		}
		docs := filepath.Join(projDir, ".docs")
		_ = filepath.WalkDir(docs, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			rel, _ := filepath.Rel(docs, p)
			// Sidecars, not projection: the manifest and the snapshot pin the
			// backend HEAD, which every capture advances by design.
			if rel == ".manifest.json" || rel == ".snapshot" {
				return nil
			}
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			got[".docs/"+filepath.ToSlash(rel)] = fmt.Sprintf("%x", sha256.Sum256(data))
			return nil
		})
		pages, err := compile.Pages(repoDir)
		if err != nil {
			t.Fatalf("compile.Pages: %v", err)
		}
		for _, p := range pages {
			got["wiki "+p.Name] = p.InputHash()
		}
		return got
	}
	diff := func(label string, a, b map[string]string) {
		t.Helper()
		keys := slices.Sorted(func(yield func(string) bool) {
			for k := range a {
				if !yield(k) {
					return
				}
			}
			for k := range b {
				if _, ok := a[k]; !ok && !yield(k) {
					return
				}
			}
		})
		for _, k := range keys {
			if a[k] != b[k] {
				t.Errorf("%s: %q changed\nbefore: %s\nafter:  %s", label, k, a[k], b[k])
			}
		}
	}

	control := snap()
	diff("control (no ideas yet)", control, snap())
	if t.Failed() {
		t.FailNow()
	}
	ideaCmd(t, projDir, "new", "zebra listrada no backlog")
	ideaCmd(t, projDir, "new", "conciliação automática do OFX", "--component", "lint")
	ideaCmd(t, projDir, "new", "terceira")
	ideaCmd(t, projDir, "drop", "I-0003", "--reason", "teste")
	after := snap()
	diff("after ideas", control, after)
	for k, v := range after {
		if strings.Contains(v, "I-0") || strings.Contains(v, "idea.") {
			t.Errorf("%q mentions an idea:\n%s", k, v)
		}
	}
}

// syncedAtLine matches the index_synced_at member of a search/ask JSON payload.
var syncedAtLine = regexp.MustCompile(`,\n\s*"index_synced_at": [^\n]*`)
