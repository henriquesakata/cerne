package cli

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/doccheck"
	"github.com/henriquesakata/cerne/internal/verbs"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// ptSurfaceAllowed lists whole visible cobra fields that may carry pt-BR on
// purpose, keyed by "<command path>|<field>". Empty: quoted forms are stripped
// by ptSurfacePhrases instead, so new pt-BR prose in a field still fails.
var ptSurfaceAllowed = map[string]string{}

// ptSurfacePhrases are the pt-BR forms visible cobra text may quote, stripped
// before the check.
var ptSurfacePhrases = map[string]string{
	"critério N": "the protocol form a docs_language=pt-BR result must carry (F-0359/F-0418)",
	"Altera:":    "the stored label written inside a runbook errata document",
	"`pare`":     "the stored stop marker of a runbook run entry",
}

// ptFindingPhrases are the same for lint/hook/harness message texts.
var ptFindingPhrases = map[string]string{
	"`pare`": "the stored stop marker of a runbook run entry",
}

// stripQuotedForms removes each allowed phrase from text.
func stripQuotedForms(text string, phrases map[string]string) string {
	for p := range phrases {
		text = strings.ReplaceAll(text, p, " ")
	}
	return text
}

// TestCommandSurfaceIsEnglish (B-0015) walks the whole visible cobra tree —
// Use, Short, Long, Example, aliases, flag names and flag usages — and fails
// on a pt-BR word or diacritic. Hidden commands and flags are exempt: that is
// where the legacy pt-BR names live as aliases for installed scripts.
func TestCommandSurfaceIsEnglish(t *testing.T) {
	// ptStopWords are pt-BR words with no English reading, matched on word
	// boundaries (a hyphen splits: "sem-passo" holds "sem" and "passo").
	ptStopWords := regexp.MustCompile(`(?i)\b(janela|passos?|palavra|pare|por|evidencia|proposta|decidida|altera|consolidado|substituir|sem|nota|esperada|fundida|aceita|rodada)\b`)
	check := func(path, field, text string) {
		if _, ok := ptSurfaceAllowed[path+"|"+field]; ok {
			return
		}
		text = stripQuotedForms(text, ptSurfacePhrases)
		why := doccheck.PTReason(text)
		if m := ptStopWords.FindString(text); m != "" {
			why = "pt-BR word " + strconv.Quote(m)
		}
		if why != "" {
			t.Errorf("%s: %s reads as pt-BR (%s): %s", path, field, why, firstLine(text))
		}
	}
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		if c.Hidden {
			return
		}
		p := c.CommandPath()
		check(p, "Use", c.Use)
		check(p, "Short", c.Short)
		check(p, "Long", c.Long)
		check(p, "Example", c.Example)
		check(p, "Aliases", strings.Join(c.Aliases, " "))
		c.LocalFlags().VisitAll(func(f *pflag.Flag) {
			if !f.Hidden {
				check(p, "--"+f.Name, f.Name+" "+f.Usage)
			}
		})
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(Root())
}

// legacyFlagOf maps each hidden pt-BR flag to the English flag it aliases.
var legacyFlagOf = map[string]string{
	"altera": "step", "sem-passo": "no-step", "consolidado": "consolidated",
	"janela": "window", "passos": "steps", "palavra": "word",
	"pare": "stop", "evidencia": "evidence", "por": "by", "esperada": "expected",
}

// TestLegacyTwinsMirrorTheirEnglishSibling (B-0015): every hidden pt-BR flag
// has the same type and default as its English flag, and every hidden
// `substituir` command carries the same flag set as the visible `supersede`.
// An unregistered hidden flag fails too, so a new alias must join the map.
func TestLegacyTwinsMirrorTheirEnglishSibling(t *testing.T) {
	flagSet := func(c *cobra.Command) map[string]string {
		m := map[string]string{}
		c.LocalFlags().VisitAll(func(f *pflag.Flag) {
			if !f.Hidden {
				m[f.Name] = f.Value.Type() + "|" + f.DefValue + "|" + f.NoOptDefVal
			}
		})
		return m
	}
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		c.LocalFlags().VisitAll(func(f *pflag.Flag) {
			if !f.Hidden || f.Name == "dir" {
				return
			}
			eng, ok := legacyFlagOf[f.Name]
			if !ok {
				t.Errorf("%s: hidden flag --%s is not in legacyFlagOf", c.CommandPath(), f.Name)
				return
			}
			e := c.LocalFlags().Lookup(eng)
			if e == nil || e.Hidden || e.Value.Type() != f.Value.Type() || e.DefValue != f.DefValue || e.NoOptDefVal != f.NoOptDefVal ||
				e.Shorthand != f.Shorthand || !reflect.DeepEqual(e.Annotations, f.Annotations) {
				t.Errorf("%s: --%s does not mirror the visible --%s", c.CommandPath(), f.Name, eng)
			}
		})
		if c.Hidden && strings.HasPrefix(c.Use, "substituir") {
			sib := c.Parent().Commands()
			var vis *cobra.Command
			for _, s := range sib {
				if !s.Hidden && s.Name() == "supersede" {
					vis = s
				}
			}
			if vis == nil {
				t.Errorf("%s: no visible supersede sibling", c.CommandPath())
			} else if a, b := flagSet(c), flagSet(vis); !reflect.DeepEqual(a, b) {
				t.Errorf("%s flags %v differ from supersede %v", c.CommandPath(), a, b)
			}
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(Root())
}

// TestChatCatalogHasNoPortugueseAliases (B-0015): the hidden pt-BR twin never
// reaches the chat catalog the LLM reads, and the chat cannot run it.
func TestChatCatalogHasNoPortugueseAliases(t *testing.T) {
	for _, v := range verbs.From(Root()) {
		if strings.Contains(v.Path, "substituir") {
			t.Errorf("chat catalog lists the pt-BR alias %q", v.Path)
		}
	}
	res := verbs.Run(context.Background(), Root(), verbs.Call{Verb: "feature substituir", Args: []string{"F-0001"}}, verbs.Env{Confirmed: true})
	if !errors.Is(res.Err, verbs.ErrUnknownVerb) {
		t.Errorf("running the hidden alias through the chat = %v, want ErrUnknownVerb", res.Err)
	}
}

// TestFindingTextsAreEnglish (B-0015) closes the gap TestRuntimeOutputIsEnglish
// leaves: a lint finding, hook or harness message is built with fmt.Sprintf
// and often assigned to a variable first (problem = ..., why = ...), which the
// output-call guard never sees. In the non-test files of internal/lint,
// internal/harness and internal/hooks it checks every string literal that is
// a fmt.Sprintf/Errorf/errors.New argument, the right side of an assignment,
// or a value of a Detail/Fix/Message/Title/Summary/Hint field. Phrases a text
// quotes on purpose are stripped first (ptFindingPhrases, each with its reason).
func TestFindingTextsAreEnglish(t *testing.T) {
	ptWords := regexp.MustCompile(`(?i)\b(janela|passos?|palavra|por|evidencia|proposta|decidida|altera|consolidado|substituir|sem|nota|esperada|fundida|vincule|corrija|acrescente|execução|para|com|não)\b`)
	fields := map[string]bool{"Detail": true, "Fix": true, "Message": true, "Title": true, "Summary": true, "Hint": true}
	check := func(fset *token.FileSet, lit *ast.BasicLit) {
		if lit.Kind != token.STRING {
			return
		}
		s, err := strconv.Unquote(lit.Value)
		if err != nil {
			return
		}
		s = stripQuotedForms(s, ptFindingPhrases)
		why := doccheck.PTReason(s)
		if w := ptWords.FindString(s); w != "" {
			why = "pt-BR word " + strconv.Quote(w)
		}
		if why != "" {
			t.Errorf("%s: message text reads as pt-BR (%s): %q", fset.Position(lit.Pos()), why, s)
		}
	}
	fset := token.NewFileSet()
	for _, root := range []string{"lint", "harness", "hooks"} {
		_ = filepath.WalkDir(filepath.Join("..", root), func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			f, perr := parser.ParseFile(fset, path, nil, 0)
			if perr != nil {
				t.Fatal(perr)
			}
			ast.Inspect(f, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.CallExpr:
					sel, ok := x.Fun.(*ast.SelectorExpr)
					if !ok {
						return true
					}
					pkg, ok2 := sel.X.(*ast.Ident)
					if ok2 && ((pkg.Name == "fmt" && (sel.Sel.Name == "Sprintf" || sel.Sel.Name == "Errorf")) || (pkg.Name == "errors" && sel.Sel.Name == "New")) && len(x.Args) > 0 {
						for _, l := range stringLits(x.Args[0]) {
							check(fset, l)
						}
					}
				case *ast.AssignStmt:
					for _, r := range x.Rhs {
						for _, l := range stringLits(r) {
							check(fset, l)
						}
					}
				case *ast.KeyValueExpr:
					if k, ok := x.Key.(*ast.Ident); ok && fields[k.Name] {
						ast.Inspect(x.Value, func(m ast.Node) bool {
							if l, ok := m.(*ast.BasicLit); ok {
								check(fset, l)
							}
							return true
						})
					}
				}
				return true
			})
			return nil
		})
	}
}

// legacyReaders are the non-test files allowed to quote a hidden pt-BR name
// because they read it, each with its reason.
var legacyReaders = map[string]string{
	"hookprovenancecmd.go": "the provenance hook still recognizes a typed `decision substituir`",
}

// TestNoTextQuotesALegacyPortugueseName (B-0036): the hidden pt-BR names of
// B-0015 exist only so installed scripts keep running, so no text cerne ships
// quotes one as the command to run (N-0110). It checks every string literal of
// the non-test Go files under internal/ and every embedded text file outside
// testdata: the word substituir and a --<legacy flag> fail. Exempt are the
// literal that registers the alias (exactly "substituir") and legacyReaders.
func TestNoTextQuotesALegacyPortugueseName(t *testing.T) {
	alts := []string{"substituir"}
	for f := range legacyFlagOf {
		alts = append(alts, "--"+regexp.QuoteMeta(f))
	}
	sort.Slice(alts, func(i, j int) bool { return len(alts[i]) > len(alts[j]) })
	legacy := regexp.MustCompile(`(?:^|[^\p{L}-])(` + strings.Join(alts, "|") + `)(?:[^\p{L}-]|$)`)
	check := func(where, s string) {
		if m := legacy.FindStringSubmatch(s); m != nil {
			t.Errorf("%s quotes the hidden pt-BR name %q (use the English command): %q", where, m[1], firstLine(s))
		}
	}
	fset := token.NewFileSet()
	err := filepath.WalkDir("..", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "testdata" || d.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if _, ok := legacyReaders[d.Name()]; ok || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		switch filepath.Ext(path) {
		case ".go":
			f, perr := parser.ParseFile(fset, path, nil, 0)
			if perr != nil {
				return perr
			}
			ast.Inspect(f, func(n ast.Node) bool {
				if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
					if s, uerr := strconv.Unquote(lit.Value); uerr == nil && s != "substituir" {
						check(fset.Position(lit.Pos()).String(), s)
					}
				}
				return true
			})
		case ".md", ".sh", ".ps1", ".tmpl", ".txt", ".toml":
			b, rerr := os.ReadFile(path)
			if rerr != nil {
				return rerr
			}
			for i, line := range strings.Split(string(b), "\n") {
				check(path+":"+strconv.Itoa(i+1), line)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
