package cli

import (
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/ask"
	"github.com/henriquesakata/cerne/internal/compile"
	"github.com/henriquesakata/cerne/internal/doccheck"
	"github.com/henriquesakata/cerne/internal/doclang"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/index"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
)

// F-0447 criterion 3: every LLM prompt of cerne is written in English; what the
// document must carry comes from doclang in the project's language. The guard
// renders each prompt builder for a pt-BR and an English project and fails on
// pt-BR text outside the forms doclang gives for that language and the
// explicit, justified exceptions below.

// promptCloses says how a prompt ends: the language line that names the
// project's language ("directive" for a document draft, "reply" for an
// answer), or nothing because the output language is decided elsewhere.
type promptCloses string

const (
	closesDirective promptCloses = "directive"
	closesReply     promptCloses = "reply"
	closesNothing   promptCloses = "none"
)

type promptCase struct {
	name   string
	closes promptCloses
	why    string // explains closesNothing
	render func(t *testing.T, l doclang.Lang) string
}

// promptExceptions are the pt-BR passages a prompt keeps on purpose, each
// with the reason. A new entry needs a reason a reviewer accepts.
var promptExceptions = []struct{ re, why string }{
	{"(?s)```handoff.*?```", "the handoff block is the copy-paste form a human hands to a code agent; N-0087 keeps its labels as written"},
	{`Etapa N: ID \(t[ií]tulo\)`, "the execution-map line is parsed by the plan readers (N-0087)"},
	{`(?m)^\s*Etapa \d.*$`, "the execution-map example lines are parsed by the plan readers (N-0087)"},
	{`the words Etapa and paralelas are parsed, keep them exactly`, "names the two parsed words of the execution map"},
	{`cerne-(proposta|pergunta|nota|comando)`, "fence info strings the UI parses to draw the cards; renaming one breaks the UI"},
	{`\bsubstituir\b`, "a cerne verb name (feature substituir) the chat lists among the lifecycle verbs the harness refuses"},
	{`with the entity "não registrada"`, "the literal value of the handoff's Entidade line, named in the instruction that teaches the handoff"},
	{`"Onde está", "Para ajustar", "Fontes consultadas"`, "headings the codex live-query block asks for in a Portuguese project, quoted as data"},
	{`fase means stage or level, cenário means scene, backdrop or background, inimigo means enemy`, "a word-translation example inside an English instruction: the words are the data"},
}

// promptsPending lists the surfaces not converted yet; each is removed by the
// commit that converts it, and the list is empty when F-0447 is done.
var promptsPending = map[string]string{
	"turn context":         "converted with the other prompts",
	"roast spec":           "converted with the other prompts",
	"roast plan":           "converted with the other prompts",
	"ask":                  "converted with the other prompts",
	"compile page":         "converted with the other prompts",
	"capability bootstrap": "converted with the other prompts",
}

// promptWords extends doccheck.PTReason with the function words the B-0015
// guard also refuses in agent-facing text.
var promptWords = regexp.MustCompile(`(?i)\b(janela|passos?|palavra|por|evidencia|proposta|decidida|altera|consolidado|substituir|sem|nota|esperada|fundida|vincule|corrija|acrescente|execução|para|com|não|você|escreva|responda|sua|seu|uma)\b`)

func projectProse(l doclang.Lang, prompt string) string {
	for _, e := range promptExceptions {
		prompt = regexp.MustCompile(e.re).ReplaceAllString(prompt, "")
	}
	// The forms doclang gives for l are the document's, not the prompt's.
	var forms []string
	for term := doclang.Term(0); term < 64; term++ {
		if w := doclang.Display(term, l); w != "" {
			forms = append(forms, w)
		}
	}
	for _, k := range []doclang.Kind{doclang.Spec, doclang.Plan, doclang.Result, doclang.Decision, doclang.Bug, doclang.Runbook} {
		forms = append(forms, doclang.Sections(k, l)...)
	}
	forms = append(forms, doclang.AmbiguityMarker(l))
	// Longest first: a heading is stripped whole before the words inside it.
	sort.Slice(forms, func(i, j int) bool { return len(forms[i]) > len(forms[j]) })
	for _, f := range forms {
		prompt = strings.ReplaceAll(prompt, f, "")
	}
	return prompt
}

func promptCases() []promptCase {
	feature := store.Feature{ID: "F-0001", Title: "Sign in with Google"}
	chat := func(channel bool) func(*testing.T, doclang.Lang) string {
		return func(t *testing.T, l doclang.Lang) string {
			projDir, _, repoDir := setupProjectIn(t, string(l))
			p, err := app.BuildChatSystemPrompt(repoDir, projDir, project.Config{}, channel)
			if err != nil {
				t.Fatal(err)
			}
			return p
		}
	}
	return []promptCase{
		{name: "draft spec", closes: closesDirective, render: func(_ *testing.T, l doclang.Lang) string { return BuildSpecDraftPrompt(l, feature, "") }},
		{name: "draft plan", closes: closesDirective, render: func(_ *testing.T, l doclang.Lang) string {
			return BuildPlanDraftPrompt(l, feature, "spec", "", []store.Feature{{ID: "T-0001", Title: "Task", State: domain.StateCandidate}})
		}},
		{name: "draft result", closes: closesDirective, render: func(_ *testing.T, l doclang.Lang) string {
			return BuildResultDraftPrompt(l, feature, "spec", "plan", "")
		}},
		{name: "draft decision", closes: closesDirective, render: func(_ *testing.T, l doclang.Lang) string {
			return BuildDecisionDraftPrompt(l, store.Decision{ID: "D-0001", Title: "Use Postgres"}, "")
		}},
		{name: "draft runbook", closes: closesDirective, render: func(_ *testing.T, l doclang.Lang) string {
			return BuildRunbookDraftPrompt(l, store.Runbook{ID: "RB-0001", Title: "Rotate the key"}, "")
		}},
		{name: "chat with channel", closes: closesDirective, render: chat(true)},
		{name: "chat without channel", closes: closesDirective, render: chat(false)},
		{name: "chat refine block", closes: closesNothing, why: "appended inside the chat prompt, before its closing directive", render: func(t *testing.T, l doclang.Lang) string {
			_, _, repoDir := setupProjectIn(t, string(l))
			if err := store.SaveFeature(repoDir, store.Feature{ID: "F-0001", Title: "Sign in", State: domain.StateApproved}); err != nil {
				t.Fatal(err)
			}
			p, err := app.BuildRefineBlock(repoDir, "F-0001")
			if err != nil {
				t.Fatal(err)
			}
			return p
		}},
		{name: "chat transcript", closes: closesNothing, why: "the transcript wrapper sits after the system prompt, which carries the directive", render: func(_ *testing.T, _ doclang.Lang) string {
			return app.BuildChatPrompt("system", []app.ChatMessage{{Role: "user", Content: "hello"}, {Role: "assistant", Content: "hi"}})
		}},
		{name: "turn context", closes: closesNothing, why: "context block injected into the chat turn, which carries the directive", render: func(t *testing.T, l doclang.Lang) string {
			_, _, repoDir := setupProjectIn(t, string(l))
			_ = store.SaveDecision(repoDir, store.Decision{ID: "D-0001", Title: "Git is the source of truth", State: domain.DecisionActive})
			_ = store.SaveNote(repoDir, store.Note{ID: "N-0001", Title: "Specs use EARS", State: domain.NoteActive, Components: []string{"convention"}})
			_ = store.SaveFeature(repoDir, store.Feature{ID: "F-0001", Title: "Bank reconciliation", State: domain.StateImplemented})
			db := filepath.Join(t.TempDir(), "index.db")
			if err := index.Rebuild(repoDir, db, time.Time{}); err != nil {
				t.Fatal(err)
			}
			tc, err := app.BuildTurnContext(repoDir, db, "bank reconciliation", 2000)
			if err != nil {
				t.Fatal(err)
			}
			return tc.Prompt
		}},
		{name: "roast spec", closes: closesReply, render: func(_ *testing.T, _ doclang.Lang) string {
			return app.BuildRoastPrompt(app.RoastPersonas[0], "F-0001", "Sign in", "the spec")
		}},
		{name: "roast plan", closes: closesReply, render: func(_ *testing.T, _ doclang.Lang) string {
			return app.BuildRoastPromptFor(app.PlanRoastPersonas[0], "F-0001", "Sign in", app.PlanRoastIntro, []app.RoastDoc{{Label: "SPEC", Body: "s"}, {Label: "PLAN", Body: "p"}})
		}},
		{name: "ask", closes: closesReply, render: func(_ *testing.T, _ doclang.Lang) string {
			return ask.BuildAskPrompt("what is X?", []ask.Snippet{{Source: "F-0001", Kind: "feature", State: "implemented", Historical: true, URL: "https://example.com", Text: "t"}}, true)
		}},
		{name: "compile page", closes: closesReply, render: func(_ *testing.T, _ doclang.Lang) string {
			return compile.BuildPagePrompt(compile.Page{Name: "overview", Sources: []compile.EntitySource{{ID: "F-0001", Title: "Sign in", Kind: "feature", State: "implemented", Content: "c"}}})
		}},
		{name: "capability bootstrap", closes: closesReply, render: func(t *testing.T, l doclang.Lang) string {
			_, _, repoDir := setupProjectIn(t, string(l))
			_ = store.SaveFeature(repoDir, store.Feature{ID: "F-0001", Title: "Sign in", State: domain.StateImplemented})
			p, err := app.CapabilityBootstrapPrompt(repoDir)
			if err != nil {
				t.Fatal(err)
			}
			return p
		}},
		{name: "draft output contract", closes: closesNothing, why: "a sentence embedded in every draft prompt, before the directive", render: func(_ *testing.T, _ doclang.Lang) string { return app.DraftOutputContract }},
	}
}

func TestPromptsAreEnglish(t *testing.T) {
	for _, c := range promptCases() {
		if why, ok := promptsPending[c.name]; ok {
			t.Run(c.name, func(t *testing.T) { t.Skip("pending: " + why) })
			continue
		}
		for _, l := range doclang.Langs() {
			t.Run(c.name+"/"+string(l), func(t *testing.T) {
				prompt := c.render(t, l)
				switch c.closes {
				case closesDirective:
					if !strings.HasSuffix(prompt, doclang.Directive(l)) {
						t.Errorf("the prompt does not end with the %s directive", l)
					}
					prompt = strings.TrimSuffix(prompt, doclang.Directive(l))
				case closesReply:
					if !strings.HasSuffix(prompt, doclang.Reply(l)) {
						t.Errorf("the prompt does not end with the %s reply line", l)
					}
					prompt = strings.TrimSuffix(prompt, doclang.Reply(l))
				}
				prose := projectProse(l, prompt)
				if why := doccheck.PTReason(prose); why != "" {
					t.Errorf("the prompt reads as pt-BR (%s): %s", why, firstPT(prose))
				}
				if w := promptWords.FindString(prose); w != "" {
					t.Errorf("the prompt carries the pt-BR word %s: %s", strconv.Quote(w), firstPT(prose))
				}
			})
		}
	}
}

// firstPT is the first line of prose that carries a pt-BR hit, trimmed.
func firstPT(prose string) string {
	for _, line := range strings.Split(prose, "\n") {
		if doccheck.PTReason(line) != "" || promptWords.MatchString(line) {
			if len(line) > 140 {
				line = line[:140] + "…"
			}
			return strconv.Quote(line)
		}
	}
	return ""
}
