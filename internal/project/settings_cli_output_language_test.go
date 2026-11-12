package project

import (
	"errors"
	"path/filepath"
	"slices"
	"testing"
)

// TestRepoDeclaresEnglishCLI (F-0353 criterion 7): this repo's own .cernerc
// sets lint.cli_output_language = "en" — its CLI is English (F-0124).
func TestRepoDeclaresEnglishCLI(t *testing.T) {
	t.Setenv("CERNE_CONFIG_DIR", t.TempDir())
	if got := CLIOutputLanguage(filepath.Join("..", "..")); got != "en" {
		t.Errorf("repo .cernerc: lint.cli_output_language = %q, want en", got)
	}
}

// TestCLIOutputLanguageSetting (F-0353 criteria 1, 3, 7): the key is declared
// in the registry — project scope, values "" and "en" — and CLIOutputLanguage
// is its one read: "" unless a project stores "en".
func TestCLIOutputLanguageSetting(t *testing.T) {
	const id = "lint.cli_output_language"
	var decl Setting
	for _, s := range settingsRegistry {
		if s.ID == id {
			decl = s
		}
	}
	if decl.Type != "string" || decl.Default != "" || !slices.Equal(decl.Scopes, []string{ScopeProject}) ||
		!slices.Equal(decl.Enum, []string{"en"}) || decl.Title == "" || decl.Description == "" {
		t.Fatalf("%s = %+v, want string, default \"\", scopes [project], enum [en], a title and a description", id, decl)
	}

	for _, c := range []struct{ name, project, want string }{
		{"unset", "", ""},
		{"en", "[lint]\ncli_output_language = \"en\"\n", "en"},
		{"value outside the enum is skipped", "[lint]\ncli_output_language = \"fr\"\n", ""},
		{"wrong type is skipped", "[lint]\ncli_output_language = true\n", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			projDir, _ := settingsFixture(t, c.project, "", false)
			if got := CLIOutputLanguage(projDir); got != c.want {
				t.Errorf("CLIOutputLanguage = %q, want %q", got, c.want)
			}
		})
	}

	t.Run("no project", func(t *testing.T) {
		projDir, _ := settingsFixture(t, "", "", true)
		if got := CLIOutputLanguage(projDir); got != "" {
			t.Errorf("CLIOutputLanguage = %q, want \"\"", got)
		}
	})

	t.Run("set refuses a value outside the enum, writes en", func(t *testing.T) {
		projDir, _ := settingsFixture(t, "", "", false)
		if err := SetSetting(projDir, id, ScopeProject, "pt"); !errors.Is(err, ErrInvalidSettingValue) {
			t.Fatalf("SetSetting pt: err = %v, want ErrInvalidSettingValue", err)
		}
		if err := SetSetting(projDir, id, ScopeProject, "en"); err != nil {
			t.Fatalf("SetSetting en: %v", err)
		}
		if got := CLIOutputLanguage(projDir); got != "en" {
			t.Errorf("after set, CLIOutputLanguage = %q, want en", got)
		}
		if err := SetSetting(projDir, id, ScopeHost, "en"); !errors.Is(err, ErrSettingScope) {
			t.Errorf("host scope: err = %v, want ErrSettingScope", err)
		}
	})
}
