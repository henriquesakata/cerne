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
	"strconv"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/doccheck"
	"github.com/henriquesakata/cerne/internal/verbs"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// ptSurfaceAllowed lists visible cobra text that may carry pt-BR on purpose,
// keyed by "<command path>|<field>". Each entry needs a reason.
var ptSurfaceAllowed = map[string]string{
	"cerne feature complete|Example": "quotes the pt-BR protocol form \"critério N\" a docs_language=pt-BR result must carry (F-0359/F-0418)",
	"cerne runbook merge|Long":       "quotes the stored 'Altera:' label written inside a runbook errata document",
	"cerne runbook report|Long":      "quotes the stored 'Altera:' label written inside a runbook errata document",
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
			if e == nil || e.Hidden || e.Value.Type() != f.Value.Type() || e.DefValue != f.DefValue || e.NoOptDefVal != f.NoOptDefVal {
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
// leaves: a lint finding's Detail/Fix (and any message a hook or harness item
// carries) is built with fmt.Sprintf into a struct field, which the output-call
// guard never sees. Every string literal under a Detail:/Fix: field in
// internal/lint, internal/harness and internal/hooks must read as English.
// There is no allowlist: none of these texts needs to quote a pt-BR form.
func TestFindingTextsAreEnglish(t *testing.T) {
	ptWords := regexp.MustCompile(`(?i)\b(janela|passos?|palavra|por|evidencia|proposta|decidida|altera|consolidado|substituir|sem|nota|esperada|fundida|vincule|corrija|acrescente|execução)\b`)
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
				kv, ok := n.(*ast.KeyValueExpr)
				if !ok {
					return true
				}
				if k, ok := kv.Key.(*ast.Ident); !ok || (k.Name != "Detail" && k.Name != "Fix") {
					return true
				}
				ast.Inspect(kv.Value, func(m ast.Node) bool {
					lit, ok := m.(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						return true
					}
					s, _ := strconv.Unquote(lit.Value)
					why := doccheck.PTReason(s)
					if w := ptWords.FindString(s); w != "" {
						why = "pt-BR word " + strconv.Quote(w)
					}
					if why != "" {
						t.Errorf("%s: finding text reads as pt-BR (%s): %q", fset.Position(lit.Pos()), why, s)
					}
					return true
				})
				return true
			})
			return nil
		})
	}
}
