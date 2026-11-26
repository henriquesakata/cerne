package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
)

// riskGit runs git in dir with a fixed identity.
func riskGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "commit.gpgsign=false"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func riskWrite(t *testing.T, dir, rel, body string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// riskRepo makes dir a git repository whose default branch main holds
// base.txt plus base, and checks out a delivery branch that commits change
// (a path → its content; "" removes it, "->new" renames it to new). An empty
// change leaves HEAD on main.
func riskRepo(t *testing.T, dir string, base, change map[string]string) {
	t.Helper()
	riskGit(t, dir, "init", "-q", "-b", "main")
	riskWrite(t, dir, "base.txt", "base\n")
	for rel, body := range base {
		riskWrite(t, dir, rel, body)
	}
	riskGit(t, dir, "add", "-A")
	riskGit(t, dir, "commit", "-q", "-m", "base")
	if len(change) == 0 {
		return
	}
	riskGit(t, dir, "checkout", "-q", "-b", "feat/x")
	for rel, body := range change {
		switch {
		case body == "":
			riskGit(t, dir, "rm", "-q", rel)
		case strings.HasPrefix(body, "->"):
			riskGit(t, dir, "mv", rel, strings.TrimPrefix(body, "->"))
		default:
			riskWrite(t, dir, rel, body)
		}
	}
	riskGit(t, dir, "add", "-A")
	riskGit(t, dir, "commit", "-q", "-m", "feat: the delivery (F-0001)")
}

// TestRiskSensitiveArea: the first condition holds only when no changed file
// matches a sensitive path and no facet of the card is sensitive; a card with
// no facet, or a diff that cannot be read, never holds; the diff's size does
// not count.
// F-0516 crit. 3
func TestRiskSensitiveArea(t *testing.T) {
	big := map[string]string{}
	for i := range 300 {
		big[fmt.Sprintf("internal/big/f%03d.go", i)] = "package big\n"
	}
	for _, tc := range []struct {
		name         string
		setup        func(t *testing.T, dir string)
		facets       []string
		holds        bool
		paths, found []string // found: what the line must say
	}{
		{"nothing sensitive", func(t *testing.T, d string) {
			riskRepo(t, d, nil, map[string]string{"internal/x/x.go": "package x\n", "internal/x/x_test.go": "package x\n"})
		}, []string{"code"}, true, nil, []string{"no sensitive path or facet"}},
		{"one sensitive path among many", func(t *testing.T, d string) {
			riskRepo(t, d, nil, map[string]string{"a.go": "a", "b.go": "b", ".github/workflows/ci.yml": "on: push\n", "c.go": "c"})
		}, []string{"code"}, false, []string{".github/workflows/ci.yml"}, []string{"sensitive path", ".github/workflows/ci.yml"}},
		{"a manifest and a migration deep in the tree", func(t *testing.T, d string) {
			riskRepo(t, d, nil, map[string]string{"ui/package.json": "{}", "db/migrations/001.sql": "x", "src/requirements-dev.txt": "x", "docs/x.md": "x"})
		}, []string{"code"}, false, []string{"db/migrations/001.sql", "src/requirements-dev.txt", "ui/package.json"}, []string{"ui/package.json"}},
		{"a sensitive file renamed away", func(t *testing.T, d string) {
			riskRepo(t, d, map[string]string{".github/workflows/ci.yml": "on: push\n"}, map[string]string{".github/workflows/ci.yml": "->ci-old.yml"})
		}, []string{"code"}, false, []string{".github/workflows/ci.yml"}, []string{".github/workflows/ci.yml"}},
		{"a sensitive file deleted", func(t *testing.T, d string) {
			riskRepo(t, d, map[string]string{"CLAUDE.md": "x"}, map[string]string{"CLAUDE.md": ""})
		}, []string{"code"}, false, []string{"CLAUDE.md"}, []string{"CLAUDE.md"}},
		{"sensitive facet with clean paths", func(t *testing.T, d string) {
			riskRepo(t, d, nil, map[string]string{"a.go": "a"})
		}, []string{"code", "auth"}, false, nil, []string{"sensitive facet", "auth"}},
		{"card with no facet", func(t *testing.T, d string) {
			riskRepo(t, d, nil, map[string]string{"a.go": "a"})
		}, nil, false, nil, []string{"no facet"}},
		{"big diff with nothing sensitive", func(t *testing.T, d string) {
			riskRepo(t, d, nil, big)
		}, []string{"code"}, true, nil, []string{"no sensitive path or facet"}},
		{"not a git repository", func(t *testing.T, d string) {}, []string{"code"}, false, nil, []string{"cannot be read", "not a git repository"}},
		{"no default branch", func(t *testing.T, d string) {
			riskGit(t, d, "init", "-q", "-b", "trunk")
			riskWrite(t, d, "a.go", "a")
			riskGit(t, d, "add", "-A")
			riskGit(t, d, "commit", "-q", "-m", "a")
		}, []string{"code"}, false, nil, []string{"cannot be read", "default branch"}},
		{"HEAD on the default branch", func(t *testing.T, d string) {
			riskRepo(t, d, nil, nil)
		}, []string{"code"}, false, nil, []string{"empty", "worktree"}},
		// The checkout must be this card's delivery (review of #228).
		{"a clean checkout of another card", func(t *testing.T, d string) {
			riskRepo(t, d, nil, nil)
			riskGit(t, d, "checkout", "-q", "-b", "feat/other")
			riskWrite(t, d, "a.go", "a")
			riskGit(t, d, "add", "-A")
			riskGit(t, d, "commit", "-q", "-m", "feat: other work (F-0010)")
		}, []string{"code"}, false, nil, []string{"no commit", "cites F-0001"}},
		// The FeatureCommits rule: a body mentioning the id in passing, under a
		// subject that cites another card, is that card's commit (review 2 of #228).
		{"another card's commit mentions the id in passing", func(t *testing.T, d string) {
			riskRepo(t, d, nil, nil)
			riskGit(t, d, "checkout", "-q", "-b", "feat/other")
			riskWrite(t, d, "a.go", "a")
			riskGit(t, d, "add", "-A")
			riskGit(t, d, "commit", "-q", "-m", "feat: other work (F-0010)", "-m", "Unlike F-0001, this keeps the old flag.")
		}, []string{"code"}, false, nil, []string{"no commit", "cites F-0001"}},
		// Uncommitted work cannot be measured; cerne's own docs.lock pin is not work.
		{"a tracked file changed and not committed", func(t *testing.T, d string) {
			riskRepo(t, d, nil, map[string]string{"a.go": "a"})
			riskWrite(t, d, "a.go", "a changed\n")
		}, []string{"code"}, false, nil, []string{"uncommitted", "a.go"}},
		{"only docs.lock changed and not committed", func(t *testing.T, d string) {
			riskRepo(t, d, map[string]string{"docs.lock": "{}\n"}, map[string]string{"a.go": "a"})
			riskWrite(t, d, "docs.lock", "{\"snapshot\":\"x\"}\n")
		}, []string{"code"}, true, nil, []string{"no sensitive path or facet"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			tc.setup(t, dir)
			f := store.Feature{ID: "F-0001", Facets: tc.facets}
			c, paths, _, _ := riskArea("", dir, "", f, project.GatesConfig{})
			if c.Holds != tc.holds {
				t.Errorf("holds = %v, want %v (%q)", c.Holds, tc.holds, c.Found)
			}
			slices.Sort(paths)
			if !slices.Equal(paths, tc.paths) {
				t.Errorf("paths = %v, want %v", paths, tc.paths)
			}
			for _, w := range tc.found {
				if !strings.Contains(c.Found, w) {
					t.Errorf("found = %q, want it to say %q", c.Found, w)
				}
			}
		})
	}
}

// TestRiskSensitiveAreaFromASubdirectory: a project in a subdirectory of its
// repository matches each changed path from the repository root and from the
// project dir, and a diff.relative config does not hide the files outside it.
// F-0516 crit. 3
func TestRiskSensitiveAreaFromASubdirectory(t *testing.T) {
	for name, change := range map[string]string{
		"the agents' config under the project dir": "sub/.claude/settings.json",
		"CI at the repository root":                ".github/workflows/ci.yml",
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			riskRepo(t, dir, nil, map[string]string{change: "x\n", "sub/a.go": "a"})
			riskGit(t, dir, "config", "diff.relative", "true")
			c, paths, _, _ := riskArea("", filepath.Join(dir, "sub"), "", store.Feature{ID: "F-0001", Facets: []string{"code"}}, project.GatesConfig{})
			if c.Holds || !slices.Equal(paths, []string{change}) {
				t.Errorf("holds %v (%q), paths %v; want held by %s", c.Holds, c.Found, paths, change)
			}
		})
	}
}

// TestRiskSensitiveAreaUsesConfiguredLists: the owner's lists replace the
// defaults; an empty list holds nothing back by itself.
// F-0516 crit. 14
func TestRiskSensitiveAreaUsesConfiguredLists(t *testing.T) {
	dir := t.TempDir()
	riskRepo(t, dir, nil, map[string]string{"go.mod": "module x\n", "src/billing/pay.go": "package billing\n"})
	f := store.Feature{ID: "F-0001", Facets: []string{"auth", "payments"}}

	c, paths, facets, _ := riskArea("", dir, "", f, project.GatesConfig{RiskPaths: []string{"src/billing/**"}, RiskFacets: []string{"payments"}})
	if c.Holds || !slices.Equal(paths, []string{"src/billing/pay.go"}) || !slices.Equal(facets, []string{"payments"}) {
		t.Errorf("owner's lists: holds %v, paths %v, facets %v; want held by src/billing/pay.go and payments only (go.mod and auth left the lists)", c.Holds, paths, facets)
	}
	c, paths, facets, _ = riskArea("", dir, "", f, project.GatesConfig{RiskPaths: []string{}, RiskFacets: []string{}})
	if !c.Holds || paths != nil || facets != nil {
		t.Errorf("empty lists: holds %v (%q), paths %v, facets %v; want it to hold", c.Holds, c.Found, paths, facets)
	}
	c, _, facets, _ = riskArea("", dir, "", f, project.GatesConfig{})
	if c.Holds || !slices.Equal(facets, []string{"auth"}) {
		t.Errorf("defaults: holds %v, facets %v; want held by auth", c.Holds, facets)
	}
}

// TestRiskSensitiveAreaReadsParentFacets: a card's parent chain counts — a
// child tagged code under an epic tagged auth, two levels up included, holds.
// F-0516 crit. 3
func TestRiskSensitiveAreaReadsParentFacets(t *testing.T) {
	repoDir, dir := t.TempDir(), t.TempDir()
	riskRepo(t, dir, nil, map[string]string{"a.go": "a"})
	for _, f := range []store.Feature{
		{ID: "E-0003", Facets: []string{"auth"}},
		{ID: "E-0002", Parent: "E-0003", Facets: []string{"code"}},
	} {
		if err := store.SaveFeature(repoDir, f); err != nil {
			t.Fatal(err)
		}
	}
	c, _, facets, _ := riskArea(repoDir, dir, "", store.Feature{ID: "F-0001", Parent: "E-0002", Facets: []string{"code"}}, project.GatesConfig{})
	if c.Holds || !slices.Equal(facets, []string{"auth"}) || !strings.Contains(c.Found, "auth") {
		t.Errorf("holds %v (%q), facets %v; want held by the grandparent's auth", c.Holds, c.Found, facets)
	}
}

// TestRiskSensitivePatternsNormalized: a pattern an owner writes the way
// gitignore or Windows would — a leading slash, a trailing slash for a
// folder, backslashes — still matches instead of silently matching nothing.
// F-0516 crit. 14
func TestRiskSensitivePatternsNormalized(t *testing.T) {
	files := []string{"go.mod", "infra/main.tf", "deploy/k8s/app.yml", ".github/workflows/ci.yml", "src/app.go"}
	got := sensitivePaths(files, []string{"/go.mod", "infra/", "/deploy/**", `.github\workflows\**`}, "")
	if want := files[:4]; !slices.Equal(got, want) {
		t.Errorf("matched %v, want %v", got, want)
	}
}

// TestRiskGauntletWin: only a last round won against a real bar holds; a
// tie, a loss, an exempt bar, no verdict or the Gauntlet off never does.
// F-0516 crit. 4
func TestRiskGauntletWin(t *testing.T) {
	const bar = "## Gauntlet\n\nBarra: Mergify — pull_request_rules\n"
	for _, tc := range []struct {
		name, result string
		on, holds    bool
		verdict      string
	}{
		{"win", bar + "Rodada 1 — vitória: \"o nosso\"\n", true, true, "win"},
		{"tie", bar + "Rodada 1 — empate: \"iguais\"\n", true, false, "tie"},
		{"loss", bar + "Rodada 1 — derrota: \"o deles\"\n", true, false, "loss"},
		{"loss then win", bar + "Rodada 1 — derrota: \"o deles\"\nRodada 2 — vitória: \"o nosso\"\n", true, true, "win"},
		{"win then loss", bar + "Rodada 1 — vitória: \"o nosso\"\nRodada 2 — derrota: \"o deles\"\n", true, false, "loss"},
		{"bar waived", "## Gauntlet\n\nBarra: dispensada por decisão humana (\"sem tempo\")\n", true, false, "exempt"},
		{"bar not applicable", "## Gauntlet\n\nBarra: não aplicável (sem comparável) — padrão alternativo: testes\n", true, false, "exempt"},
		{"bar not applicable with a won round", "## Gauntlet\n\nBarra: não aplicável (sem comparável) — padrão alternativo: testes\nRodada 1 — vitória: \"o nosso\"\n", true, false, "exempt"},
		{"no record", "## Resultado\n\nX.\n", true, false, "none"},
		// The highest round decides even when it cannot be read (review of #228).
		{"win then an unreadable round", bar + "Rodada 1 — vitória: \"o nosso\"\nRodada 2 — o juiz respondeu: \"VEREDITO: B\"\n", true, false, "none"},
		{"Gauntlet off", bar + "Rodada 1 — vitória: \"o nosso\"\n", false, false, "off"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, verdict := riskGauntlet([]byte(tc.result), tc.on)
			if c.Holds != tc.holds || verdict != tc.verdict || c.Found == "" {
				t.Errorf("= holds %v, verdict %q, found %q; want holds %v, verdict %q and a reason", c.Holds, verdict, c.Found, tc.holds, tc.verdict)
			}
		})
	}
}

// TestRiskCriteriaTested: the third condition holds only when every numbered
// criterion has a marked test; one unmarked, one verified by hand, the rule
// off, no test file or no numbered criterion never holds.
// F-0516 crit. 5
func TestRiskCriteriaTested(t *testing.T) {
	const spec = "# T\n\n## Objetivo e critérios de aceitação\n\n1. Um.\n2. Dois.\n3. Três.\n"
	for _, tc := range []struct {
		name, spec, tests, cernerc string
		holds                      bool
		tested                     string
		found                      string
	}{
		{"all marked", spec, "// F-0001 crit. 1, 2\n// F-0001 crit. 3\n", "", true, "3/3", "3/3 criteria with a test"},
		{"one unmarked", spec, "// F-0001 crit. 1, 2\n", "", false, "2/3", "criterion 3"},
		{"one verified by hand", strings.Replace(spec, "3. Três.", "3. Três (verificação manual).", 1), "// F-0001 crit. 1, 2\n", "", false, "2/3", "criterion 3"},
		{"rule off", spec, "// F-0001 crit. 1, 2, 3\n", "[lint]\ncriterion_tests = false\n", false, "", "lint.criterion_tests is off"},
		{"no test file", spec, "", "", false, "0/3", "no test file"},
		{"no numbered criterion", "# T\n\n## Objetivo e critérios de aceitação\n\nTudo.\n", "// F-0001 crit. 1\n", "", false, "0/0", "no numbered criterion"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repoDir, projDir := t.TempDir(), t.TempDir()
			riskWrite(t, repoDir, filepath.Join("content", "features", "F-0001", "spec.md"), tc.spec)
			if tc.tests != "" {
				riskWrite(t, projDir, "x/x_test.go", "package x\n\n"+tc.tests)
			}
			if tc.cernerc != "" {
				riskWrite(t, projDir, ".cernerc", "project_id = \"p\"\nbackend = \"b\"\n"+tc.cernerc)
			}
			c, tested := riskTests(repoDir, projDir, store.Feature{ID: "F-0001"})
			if c.Holds != tc.holds || tested != tc.tested || !strings.Contains(c.Found, tc.found) {
				t.Errorf("= holds %v, tested %q, found %q; want holds %v, tested %q, found with %q", c.Holds, tested, c.Found, tc.holds, tc.tested, tc.found)
			}
		})
	}
}
