package cli

import (
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/doclang"
	"github.com/henriquesakata/cerne/internal/hooks"
)

// TestHookSessionContext_NamesTheDocsLanguage (F-0475 crit. 3, 4): in every
// provider envelope the session text carries the project's documentation
// language and its forms, and an English project reads no Portuguese
// planning word.
func TestHookSessionContext_NamesTheDocsLanguage(t *testing.T) {
	for _, tt := range []struct {
		lang      doclang.Lang
		want, not []string
	}{
		{doclang.EN, []string{"DOCS LANGUAGE: en", "Problem", "`Bar:`", "[CLARIFY:", "— planning"}, []string{doclang.Examples(doclang.PT).Planning}},
		{doclang.PT, []string{"DOCS LANGUAGE: pt-BR", "Problema", "`Barra:`", "[ESCLARECER:", "— planejamento"}, nil},
	} {
		projDir, _, _ := setupProjectIn(t, string(tt.lang))
		payload := hookJSON(t, "s", projDir, "", nil, nil)
		for _, format := range []string{"claude", "codex", "cursor", "gemini", "copilot"} {
			out, err := execCerneStdin(t, payload, "hook", "session-context", "--format", format)
			if err != nil {
				t.Fatalf("%s/%s: %v\n%s", tt.lang, format, err, out)
			}
			for _, w := range tt.want {
				if !strings.Contains(out, w) {
					t.Errorf("%s/%s: output lacks %q\n%s", tt.lang, format, w, out)
				}
			}
			for _, w := range tt.not {
				if strings.Contains(out, w) {
					t.Errorf("%s/%s: output carries %q\n%s", tt.lang, format, w, out)
				}
			}
		}
	}
}

// TestHookLangRules (F-0475 crit. 3, 4): what Claude Code's SessionStart
// script asks for is the project's two lines; outside a project, only the
// neutral planning item.
func TestHookLangRules(t *testing.T) {
	for _, l := range []doclang.Lang{doclang.EN, doclang.PT} {
		projDir, _, _ := setupProjectIn(t, string(l))
		t.Setenv("CLAUDE_PROJECT_DIR", projDir)
		out, err := execCerne(t, "hook", "lang-rules")
		if err != nil || out != hooks.LangRules(l)+"\n" {
			t.Fatalf("%s: err = %v, out = %q, want %q", l, err, out, hooks.LangRules(l)+"\n")
		}
	}
	t.Setenv("CLAUDE_PROJECT_DIR", t.TempDir())
	out, err := execCerne(t, "hook", "lang-rules")
	if err != nil || out != hooks.LangRules("")+"\n" {
		t.Fatalf("outside a project: err = %v, out = %q", err, out)
	}
}
