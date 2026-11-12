package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/doclang"
	"github.com/henriquesakata/cerne/internal/store"
)

// initIn runs "cerne init" for a fresh project dir against origin and returns
// that dir and the backend clone it produced.
func initIn(t *testing.T, origin string, flags ...string) (proj, repoDir string, err error) {
	t.Helper()
	cacheRoot := t.TempDir()
	t.Setenv("CERNE_CACHE_DIR", cacheRoot)
	proj = t.TempDir()
	out, err := execInit(t, append([]string{"--name", "demo", "--backend", origin, "--dir", proj}, flags...)...)
	if err != nil {
		t.Logf("init output: %s", out)
	}
	return proj, filepath.Join(cacheRoot, "demo", "repo"), err
}

// F-0359 criterion 3: a new backend is born English; the flag picks pt-BR.
func TestInit_WritesDocsLanguage(t *testing.T) {
	tests := []struct {
		name string
		flag []string
		want doclang.Lang
	}{
		{"default is English", nil, doclang.EN},
		{"explicit English", []string{"--docs-language", "en"}, doclang.EN},
		{"explicit Portuguese", []string{"--docs-language", "pt-BR"}, doclang.PT},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, repoDir, err := initIn(t, newOrigin(t), tt.flag...)
			if err != nil {
				t.Fatal(err)
			}
			if got := store.DocsLanguage(repoDir); got != tt.want {
				t.Errorf("backend docs language = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestInit_RejectsUnknownDocsLanguage(t *testing.T) {
	proj, _, err := initIn(t, newOrigin(t), "--docs-language", "fr")
	if !errors.Is(err, doclang.ErrUnknownLang) {
		t.Fatalf("err = %v, want ErrUnknownLang", err)
	}
	if _, statErr := os.Stat(filepath.Join(proj, ".cernerc")); !errors.Is(statErr, os.ErrNotExist) {
		t.Error("a refused init left a .cernerc behind")
	}
}

// The language belongs to the backend: a second checkout inherits it, and
// asking for another one is refused instead of silently splitting the project.
func TestInit_JoinInheritsDocsLanguage(t *testing.T) {
	origin := newOrigin(t)
	if _, _, err := initIn(t, origin, "--docs-language", "pt-BR"); err != nil {
		t.Fatal(err)
	}
	_, repoDir, err := initIn(t, origin)
	if err != nil {
		t.Fatalf("join without the flag: %v", err)
	}
	if got := store.DocsLanguage(repoDir); got != doclang.PT {
		t.Errorf("joined backend docs language = %q, want the backend's pt-BR", got)
	}
	proj, _, err := initIn(t, origin, "--docs-language", "en")
	if !errors.Is(err, ErrDocsLanguageMismatch) {
		t.Fatalf("join asking for en: err = %v, want ErrDocsLanguageMismatch", err)
	}
	if _, statErr := os.Stat(filepath.Join(proj, ".cernerc")); !errors.Is(statErr, os.ErrNotExist) {
		t.Error("a refused join left a .cernerc behind")
	}
}

// F-0359 criteria 1 and 2, end to end through the CLI: the scaffold a new
// feature gets follows the backend's language.
func TestFeatureNew_ScaffoldFollowsDocsLanguage(t *testing.T) {
	tests := []struct {
		flag            string
		want, mustNotBe string
	}{
		{"en", "## Goal and acceptance criteria", "## Objetivo e critérios de aceitação"},
		{"pt-BR", "## Objetivo e critérios de aceitação", "## Goal and acceptance criteria"},
	}
	for _, tt := range tests {
		t.Run(tt.flag, func(t *testing.T) {
			proj, repoDir, err := initIn(t, newOrigin(t), "--docs-language", tt.flag)
			if err != nil {
				t.Fatal(err)
			}
			if out, err := execCerne(t, "feature", "new", "Sign in with Google", "--context", "Users asked for social login.", "--dir", proj); err != nil {
				t.Fatalf("feature new: %v\n%s", err, out)
			}
			spec, err := os.ReadFile(filepath.Join(repoDir, "content", "features", "F-0001", "spec.md"))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(spec), tt.want) || strings.Contains(string(spec), tt.mustNotBe) {
				t.Errorf("spec scaffold for %s:\n%s", tt.flag, spec)
			}
		})
	}
}

func TestDocsLanguage_MissingOrInvalidIsPortuguese(t *testing.T) {
	dir := t.TempDir()
	if got := store.DocsLanguage(dir); got != doclang.PT {
		t.Errorf("no project.json: %q, want pt-BR", got)
	}
	if err := store.InitLayout(dir, store.Project{Name: "x", DocsLanguage: "klingon"}); err != nil {
		t.Fatal(err)
	}
	if got := store.DocsLanguage(dir); got != doclang.PT {
		t.Errorf("invalid value: %q, want pt-BR", got)
	}
}
