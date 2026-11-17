package cli

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
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
	"github.com/henriquesakata/cerne/internal/importer"
	"github.com/henriquesakata/cerne/internal/index"
	"github.com/henriquesakata/cerne/internal/llm"
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
	covers []string // the exported builders/constants this case renders; the completeness test reads it
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
}

// promptWords extends doccheck.PTReason with the function words the B-0015
// guard also refuses in agent-facing text.
//
// The boundary is Unicode-aware: Go's \b is ASCII-only, so it never matched a
// word that ends in an accented letter ("você", "não").
var promptWords = regexp.MustCompile(`(?i)(?:^|[^\p{L}])(janela|passos?|palavra|por|evidencia|proposta|decidida|altera|consolidado|substituir|sem|nota|esperada|fundida|vincule|corrija|acrescente|execução|para|com|não|você|escreva|responda|sua|seu|uma)(?:[^\p{L}]|$)`)

// ptWord is the first pt-BR function word of prose, "" when there is none.
func ptWord(prose string) string {
	if m := promptWords.FindStringSubmatch(prose); m != nil {
		return m[1]
	}
	return ""
}

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
	// The free text doclang hands the model to copy (the Gauntlet examples, the
	// roast words) is the document's language, not the prompt's.
	ex := doclang.Examples(l)
	forms = append(forms, ex.Method, ex.Exit, ex.BarExample, ex.Alternative, ex.Parallel, ex.Severities, ex.NoFindings,
		ex.Covered, ex.Uncovered, ex.WhereIs, ex.HowToAdjust, ex.SourcesRead)
	// Longest first: a heading is stripped whole before the words inside it.
	sort.Slice(forms, func(i, j int) bool { return len(forms[i]) > len(forms[j]) })
	for _, f := range forms {
		prompt = strings.ReplaceAll(prompt, f, "")
	}
	return prompt
}

func promptCases() []promptCase {
	feature := store.Feature{ID: "F-0001", Title: "Sign in with Google"}
	// chat renders the system prompt of one provider mode, so the paragraphs a
	// provider adds (the claude-cli read access, the ideas policies) reach the guard.
	chat := func(mode string, channel bool) func(*testing.T, doclang.Lang) string {
		return func(t *testing.T, l doclang.Lang) string {
			projDir, _, repoDir := setupProjectIn(t, string(l))
			cfg := project.Config{}
			cfg.LLM.Mode = mode
			p, err := app.BuildChatSystemPrompt(repoDir, projDir, cfg, channel)
			if err != nil {
				t.Fatal(err)
			}
			return p
		}
	}
	cases := []promptCase{
		{name: "draft spec", covers: []string{"BuildSpecDraftPrompt"}, closes: closesDirective, render: func(_ *testing.T, l doclang.Lang) string { return BuildSpecDraftPrompt(l, feature, "") }},
		{name: "draft plan", covers: []string{"BuildPlanDraftPrompt"}, closes: closesDirective, render: func(_ *testing.T, l doclang.Lang) string {
			return BuildPlanDraftPrompt(l, feature, "spec", "", []store.Feature{{ID: "T-0001", Title: "Task", State: domain.StateCandidate}})
		}},
		{name: "draft result", covers: []string{"BuildResultDraftPrompt"}, closes: closesDirective, render: func(_ *testing.T, l doclang.Lang) string {
			return BuildResultDraftPrompt(l, feature, "spec", "plan", "")
		}},
		{name: "draft decision", covers: []string{"BuildDecisionDraftPrompt"}, closes: closesDirective, render: func(_ *testing.T, l doclang.Lang) string {
			return BuildDecisionDraftPrompt(l, store.Decision{ID: "D-0001", Title: "Use Postgres"}, "")
		}},
		{name: "draft runbook", covers: []string{"BuildRunbookDraftPrompt"}, closes: closesDirective, render: func(_ *testing.T, l doclang.Lang) string {
			return BuildRunbookDraftPrompt(l, store.Runbook{ID: "RB-0001", Title: "Rotate the key"}, "")
		}},
		{name: "chat claude-cli with channel", covers: []string{"BuildChatSystemPrompt", "GauntletContract"}, closes: closesDirective, render: chat(llm.ModeClaudeCLI, true)},
		{name: "chat claude-cli without channel", closes: closesDirective, render: chat(llm.ModeClaudeCLI, false)},
		{name: "chat byo-key with channel", closes: closesDirective, render: chat(llm.ModeByoKey, true)},
		{name: "chat delegation without channel", closes: closesDirective, render: chat(llm.ModeDelegation, false)},
		{name: "chat codex live query", covers: []string{"ChatCodexLiveQuery"}, closes: closesNothing, why: "appended after the chat prompt on a codex-cli turn; names its own answer headings in the project's language", render: func(_ *testing.T, l doclang.Lang) string {
			return app.ChatCodexLiveQuery(l)
		}},
		{name: "chat refine block", covers: []string{"BuildRefineBlock"}, closes: closesNothing, why: "appended inside the chat prompt, before its closing directive", render: func(t *testing.T, l doclang.Lang) string {
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
		{name: "chat transcript", covers: []string{"BuildChatPrompt"}, closes: closesNothing, why: "the transcript wrapper sits after the system prompt, which carries the directive", render: func(_ *testing.T, _ doclang.Lang) string {
			return app.BuildChatPrompt("system", []app.ChatMessage{{Role: "user", Content: "hello"}, {Role: "assistant", Content: "hi"}})
		}},
		{name: "turn context", covers: []string{"BuildTurnContext"}, closes: closesNothing, why: "context block injected into the chat turn, which carries the directive", render: func(t *testing.T, l doclang.Lang) string {
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
		{name: "ask", covers: []string{"BuildAskPrompt"}, closes: closesReply, render: func(_ *testing.T, l doclang.Lang) string {
			return ask.BuildAskPrompt(l, "what is X?", []ask.Snippet{{Source: "F-0001", Kind: "feature", State: "implemented", Historical: true, URL: "https://example.org", Text: "t"}}, true)
		}},
		{name: "ask without preamble", closes: closesReply, render: func(_ *testing.T, l doclang.Lang) string {
			return ask.BuildAskPrompt(l, "what is X?", nil, false)
		}},
		{name: "ask one-shot rule", covers: []string{"OneShotRule"}, closes: closesNothing, why: "prefixed to the ask prompt, which carries the reply line", render: func(_ *testing.T, _ doclang.Lang) string { return ask.OneShotRule }},
		{name: "compile page", covers: []string{"BuildPagePrompt"}, closes: closesReply, render: func(_ *testing.T, l doclang.Lang) string {
			return compile.BuildPagePrompt(l, compile.Page{Name: "overview", Sources: []compile.EntitySource{{ID: "F-0001", Title: "Sign in", Kind: "feature", State: "implemented", Content: "c"}}})
		}},
		{name: "capability bootstrap", covers: []string{"CapabilityBootstrapPrompt"}, closes: closesReply, render: func(t *testing.T, l doclang.Lang) string {
			_, _, repoDir := setupProjectIn(t, string(l))
			_ = store.SaveFeature(repoDir, store.Feature{ID: "F-0001", Title: "Sign in", State: domain.StateImplemented})
			p, err := app.CapabilityBootstrapPrompt(repoDir)
			if err != nil {
				t.Fatal(err)
			}
			return p
		}},
		{name: "sequence diagram", covers: []string{"BuildSequenceDiagramPrompt"}, closes: closesReply, render: func(_ *testing.T, l doclang.Lang) string {
			return app.BuildSequenceDiagramPrompt(l, feature, "the spec", "internal/a/a.go")
		}},
		{name: "drift errata", covers: []string{"DriftDraftPrompt"}, closes: closesReply, render: func(t *testing.T, l doclang.Lang) string {
			_, _, repoDir := setupProjectIn(t, string(l))
			dir := filepath.Join(repoDir, "content", "features", "F-0001")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "spec.md"), []byte("# spec\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			p, err := app.DriftDraftPrompt(repoDir, "F-0001", "abcdef1234", []string{"a.go"})
			if err != nil {
				t.Fatal(err)
			}
			return p
		}},
		{name: "import plan", covers: []string{"BuildPlanPrompt"}, closes: closesNothing, why: "asks for JSON only; the synthesized bodies follow the language of the sources, whose project has no language yet at import", render: func(_ *testing.T, _ doclang.Lang) string {
			return importer.BuildPlanPrompt([]importer.Source{{RelPath: "docs/a.md", Content: "text"}})
		}},
		{name: "draft output contract", covers: []string{"DraftOutputContract"}, closes: closesNothing, why: "a sentence embedded in every draft prompt, before the directive", render: func(_ *testing.T, _ doclang.Lang) string { return app.DraftOutputContract }},
	}
	// Every persona of both roasts, not only the first: each lens is its own text.
	for _, p := range app.RoastPersonas {
		cases = append(cases, promptCase{name: "roast spec " + p.Key, covers: []string{"BuildRoastPrompt", "BuildRoastPromptFor"}, closes: closesReply, render: func(_ *testing.T, l doclang.Lang) string {
			return app.BuildRoastPrompt(l, p, "F-0001", "Sign in", "the spec")
		}})
	}
	for _, p := range app.PlanRoastPersonas {
		cases = append(cases, promptCase{name: "roast plan " + p.Key, closes: closesReply, render: func(_ *testing.T, l doclang.Lang) string {
			return app.BuildRoastPromptFor(l, p, "F-0001", "Sign in", app.PlanRoastIntro, []app.RoastDoc{{Label: "SPEC", Body: "s"}, {Label: "PLAN", Body: "p"}})
		}})
	}
	return cases
}

// F-0447: ptWord sees a pt-BR word that ends in an accented letter, which
// Go's ASCII \b boundary never did.
func TestPtWordSeesAccentedWordEnds(t *testing.T) {
	for _, in := range []string{"Você precisa", "isso não serve", "para casa"} {
		if ptWord(in) == "" {
			t.Errorf("ptWord(%q) found nothing", in)
		}
	}
	for _, in := range []string{"the voice is clear", "a nonprofit"} {
		if w := ptWord(in); w != "" {
			t.Errorf("ptWord(%q) = %q, want none", in, w)
		}
	}
}

// notPrompts are exported names the scan below matches by word that build no
// LLM prompt, each with the reason.
var notPrompts = map[string]string{
	"SeenPromptNotes":     "the hook's seen-notes ledger",
	"MarkPromptNotesSeen": "the hook's seen-notes ledger",
	"PromptNotes":         "a settings getter",
	"PromptTitles":        "an index lookup of titles in a prompt the user typed",
}

// F-0447: a new prompt builder cannot ship without a case in the guard. The
// scan reads every non-test file of internal/ for an exported function or
// constant that is, or names, a prompt; each must be covered by a case above or
// be listed with a reason in notPrompts.
func TestEveryPromptBuilderHasAGuardCase(t *testing.T) {
	covered := map[string]bool{}
	for _, c := range promptCases() {
		for _, n := range c.covers {
			covered[n] = true
		}
	}
	nameRe := regexp.MustCompile(`Prompt|GauntletContract|LiveQuery|RefineBlock|TurnContext|OneShotRule`)
	var found []string
	err := filepath.WalkDir("..", func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		f, perr := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if perr != nil {
			return perr
		}
		for _, decl := range f.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil && d.Name.IsExported() && nameRe.MatchString(d.Name.Name) {
					found = append(found, d.Name.Name)
				}
			case *ast.GenDecl:
				if d.Tok == token.CONST || d.Tok == token.VAR {
					for _, sp := range d.Specs {
						for _, n := range sp.(*ast.ValueSpec).Names {
							if n.IsExported() && nameRe.MatchString(n.Name) {
								found = append(found, n.Name)
							}
						}
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) < 10 {
		t.Fatalf("the scan found only %v: it is not reading internal/", found)
	}
	for _, name := range found {
		if _, skip := notPrompts[name]; !covered[name] && !skip {
			t.Errorf("%s builds or names an LLM prompt and has no case in promptCases (or an entry in notPrompts)", name)
		}
	}
}

func TestPromptsAreEnglish(t *testing.T) {
	for _, c := range promptCases() {
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
				if w := ptWord(prose); w != "" {
					t.Errorf("the prompt carries the pt-BR word %s: %s", strconv.Quote(w), firstPT(prose))
				}
			})
		}
	}
}

// firstPT is the first line of prose that carries a pt-BR hit, trimmed.
func firstPT(prose string) string {
	for _, line := range strings.Split(prose, "\n") {
		if doccheck.PTReason(line) != "" || ptWord(line) != "" {
			if len(line) > 140 {
				line = line[:140] + "…"
			}
			return strconv.Quote(line)
		}
	}
	return ""
}
