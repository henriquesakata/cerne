package cli

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode"

	"github.com/spf13/cobra"
)

// TestCommandChromeIsEnglish is the F-0184 lock: CLI chrome is English for
// agents (F-0124, N-0013), and the `work` command proved that without a
// tripwire new commands drift back to pt-BR. Cobra metadata is the guarded
// surface — authoring prompts are runtime strings, not command metadata, so
// the separation has no false positives by construction. Example is exempt:
// it may carry user-typed pt content (a --reason someone would write).
func TestCommandChromeIsEnglish(t *testing.T) {
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		for name, text := range map[string]string{
			"Use":   c.Use,
			"Short": c.Short,
			"Long":  c.Long,
		} {
			for _, r := range text {
				if unicode.Is(unicode.Latin, r) && r > 127 {
					t.Errorf("%s: %s carries a diacritic (%q) — CLI chrome is English (F-0124/F-0184): %s",
						c.CommandPath(), name, r, firstLine(text))
					break
				}
			}
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(Root())
}

// TestRuntimeOutputIsEnglish extends the F-0184 lock from cobra metadata to
// what commands print at runtime (F-0124): `cerne feature accept
// --all-implemented` shipped "N feature(s) aceita(s)." and the missing-id
// refusal "informe um <id> …" because the metadata guard cannot see fmt
// strings, and neither carries a diacritic. It walks every non-test Go file
// under internal/ and cmd/ and flags a pt-BR string literal passed to
// errors.New / fmt.Errorf (errors surface in the terminal) or to fmt.Print* /
// fmt.Fprint*. Fprint* into a strings.Builder or bytes.Buffer is exempt: that
// is how documents and LLM prompts are built, and those are pt-BR on purpose.
func TestRuntimeOutputIsEnglish(t *testing.T) {
	fset := token.NewFileSet()
	for _, root := range []string{"..", filepath.Join("..", "..", "cmd")} {
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			f, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				return err
			}
			for _, h := range ptOutputHits(fset, f) {
				t.Errorf("%s — CLI output is English (F-0124); build pt-BR documents/prompts in a strings.Builder or bytes.Buffer", h)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

// TestPtOutputHitsDetector pins the detector on synthetic source, so a clean
// tree means clean output and not a blind guard.
func TestPtOutputHitsDetector(t *testing.T) {
	const src = `package x
func f(cmd C, w io.Writer, b *strings.Builder, id string) error {
	fmt.Fprintf(cmd.OutOrStdout(), "%d feature(s) aceita(s).\n", 1)
	fmt.Fprintln(os.Stderr, "cerne: clone de cache ausente")
	fmt.Fprintf(w, "accepting %s: " + "plano do épico\n", id)
	_ = errors.New("prioridade " + "inválida")
	var sb strings.Builder
	fmt.Fprintf(&sb, "- título: %s\n", id)
	fmt.Fprintf(b, "Sintetize a página de wiki do projeto\n")
	fmt.Fprintln(cmd.OutOrStdout(), "no such feature: do it via gitlab.com — " + id + " → done")
	return fmt.Errorf("informe um <id> ou use --all-implemented")
}`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	hits := ptOutputHits(fset, f)
	wantLines := []string{"x.go:3:", "x.go:4:", "x.go:5:", "x.go:6:", "x.go:11:"}
	if len(hits) != len(wantLines) {
		t.Fatalf("hits = %d, want %d:\n%s", len(hits), len(wantLines), strings.Join(hits, "\n"))
	}
	for i, want := range wantLines {
		if !strings.HasPrefix(hits[i], want) {
			t.Errorf("hit %d = %q, want prefix %q", i, hits[i], want)
		}
	}
}

// ptWords are Portuguese function words and verbs that an English message
// never carries as a whole token — the diacritic check alone misses
// unaccented pt-BR like "aceita(s)" or "informe um <id>". English homographs
// ("no", "do", "com" of ".com") are left out on purpose.
var ptWords = map[string]bool{
	"um": true, "uma": true, "ou": true, "para": true, "sem": true, "nenhum": true, "nenhuma": true,
	"de": true, "da": true, "das": true, "dos": true, "na": true, "em": true, "que": true,
	"pelo": true, "pela": true, "seu": true, "sua": true, "deve": true, "pode": true, "ainda": true,
	"foi": true, "ser": true, "informe": true, "erro": true, "falha": true, "arquivo": true,
	"aceita": true, "aceitando": true,
}

// ptReason says why s reads as pt-BR, or "" when it does not.
func ptReason(s string) string {
	for _, r := range s {
		if unicode.Is(unicode.Latin, r) && r > 127 {
			return "diacritic " + strconv.QuoteRune(r)
		}
	}
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) }) {
		if ptWords[w] {
			return "pt-BR word " + strconv.Quote(w)
		}
	}
	return ""
}

// ptOutputHits lists, as "file:line:col: call: reason: literal", every
// pt-BR literal f passes to an output or error call.
func ptOutputHits(fset *token.FileSet, f *ast.File) []string {
	builders := builderNames(f)
	var hits []string
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		name := pkg.Name + "." + sel.Sel.Name
		args := call.Args
		switch {
		case name == "errors.New" || name == "fmt.Errorf" || (pkg.Name == "fmt" && strings.HasPrefix(sel.Sel.Name, "Print")):
		case pkg.Name == "fmt" && strings.HasPrefix(sel.Sel.Name, "Fprint") && len(args) > 0:
			if isBuilderExpr(args[0], builders) {
				return true
			}
			args = args[1:]
		default:
			return true
		}
		for _, a := range args {
			for _, lit := range stringLits(a) {
				s, err := strconv.Unquote(lit.Value)
				if err != nil {
					continue
				}
				if why := ptReason(s); why != "" {
					hits = append(hits, fset.Position(lit.Pos()).String()+": "+name+": "+why+": "+strconv.Quote(s))
				}
			}
		}
		return true
	})
	return hits
}

// stringLits returns the string literals of e, following "a" + "b" chains.
func stringLits(e ast.Expr) []*ast.BasicLit {
	switch x := e.(type) {
	case *ast.BasicLit:
		if x.Kind == token.STRING {
			return []*ast.BasicLit{x}
		}
	case *ast.ParenExpr:
		return stringLits(x.X)
	case *ast.BinaryExpr:
		if x.Op == token.ADD {
			return append(stringLits(x.X), stringLits(x.Y)...)
		}
	}
	return nil
}

// builderNames collects the identifiers f declares as a strings.Builder or
// bytes.Buffer (value or pointer, by var, :=, or parameter/field).
func builderNames(f *ast.File) map[string]bool {
	names := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.ValueSpec:
			for i, nm := range x.Names {
				if (x.Type != nil && isBuilderType(x.Type)) || (i < len(x.Values) && isBuilderType(x.Values[i])) {
					names[nm.Name] = true
				}
			}
		case *ast.AssignStmt:
			for i, rhs := range x.Rhs {
				if i >= len(x.Lhs) {
					break
				}
				if id, ok := x.Lhs[i].(*ast.Ident); ok && isBuilderType(rhs) {
					names[id.Name] = true
				}
			}
		case *ast.Field:
			if isBuilderType(x.Type) {
				for _, nm := range x.Names {
					names[nm.Name] = true
				}
			}
		}
		return true
	})
	return names
}

// isBuilderType reports whether e is (a pointer to, a literal of, or new of)
// strings.Builder or bytes.Buffer.
func isBuilderType(e ast.Expr) bool {
	for {
		switch x := e.(type) {
		case *ast.StarExpr:
			e = x.X
		case *ast.UnaryExpr:
			e = x.X
		case *ast.CompositeLit:
			e = x.Type
		case *ast.CallExpr:
			id, ok := x.Fun.(*ast.Ident)
			if !ok || id.Name != "new" || len(x.Args) != 1 {
				return false
			}
			e = x.Args[0]
		case *ast.SelectorExpr:
			p, ok := x.X.(*ast.Ident)
			return ok && ((p.Name == "strings" && x.Sel.Name == "Builder") || (p.Name == "bytes" && x.Sel.Name == "Buffer"))
		default:
			return false
		}
	}
}

// isBuilderExpr reports whether an Fprint* writer is a known builder (b or &b).
func isBuilderExpr(e ast.Expr, builders map[string]bool) bool {
	if u, ok := e.(*ast.UnaryExpr); ok {
		e = u.X
	}
	id, ok := e.(*ast.Ident)
	return ok && builders[id.Name]
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i] + " …"
	}
	return s
}
