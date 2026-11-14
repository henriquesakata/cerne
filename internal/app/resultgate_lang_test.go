package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
)

// TestResultRefusal_EnglishHeader (F-0418 crit. 4): the complete's refusal
// reads English in every project, its findings cite the project's forms, and
// ErrResultIncomplete still matches.
func TestResultRefusal_EnglishHeader(t *testing.T) {
	for lang, cite := range map[string]string{"en": `cite "criterion N"`, "pt-BR": `cite "critério N"`} {
		dir := t.TempDir()
		f := store.Feature{ID: "F-0001", Title: "t", State: domain.StateInDevelopment}
		if err := store.SaveProject(dir, store.Project{DocsLanguage: lang}); err != nil {
			t.Fatal(err)
		}
		if err := store.SaveFeature(dir, f); err != nil {
			t.Fatal(err)
		}
		docs := filepath.Join(dir, "content", "features", f.ID)
		if err := os.MkdirAll(docs, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, body := range map[string]string{
			"spec.md":   "## Goal and acceptance criteria\n\nGoal.\n\n1. a\n2. b\n",
			"result.md": "## Result\n\ncriterion 1 / critério 1 proved.\n",
		} {
			if err := os.WriteFile(filepath.Join(docs, name), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		err := CheckResultRecord(dir, f, project.GatesConfig{})
		if !errors.Is(err, ErrResultIncomplete) {
			t.Fatalf("%s: err = %v, want ErrResultIncomplete", lang, err)
		}
		msg := err.Error()
		if !strings.HasPrefix(msg, "incomplete result — 2 problem(s):") || !strings.Contains(msg, cite) {
			t.Fatalf("%s: refusal = %q", lang, msg)
		}
		for _, pt := range []string{"incompleto", "pendência", "sem ", "edite", "Forma"} {
			if strings.Contains(msg, pt) {
				t.Fatalf("%s: refusal carries %q: %s", lang, pt, msg)
			}
		}
	}
}
