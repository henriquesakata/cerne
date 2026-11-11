package server

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/llm"
	"github.com/henriquesakata/cerne/internal/verbs"
)

// runVerb is a test seam over verbs.Run: the package's test binary is not
// cerne, so server tests stub it or run the real executor with a bogus Bin.
var runVerb = verbs.Run

// cerneBin is the binary the executor spawns: this very process's executable.
var cerneBin = sync.OnceValue(func() string {
	bin, _ := os.Executable()
	return bin
})

const (
	chatToolName = "cerne"
	typedPrefix  = "cerne_" // typed tool of a verb: cerne_<path with _>, e.g. cerne_reference_new
)

func typedName(path string) string { return typedPrefix + strings.ReplaceAll(path, " ", "_") }

// chatRun is one tool execution of a confirmed turn, as the UI shows it.
type chatRun struct {
	Tool   string       `json:"tool"`
	Verb   string       `json:"verb"`
	Args   []string     `json:"args"`
	Result verbs.Result `json:"result"`
}

// chatTools renders the verb catalog of tree as the confirmed turn's tools
// (F-0323): the generic tool, whose verb enum is every non-lifecycle verb, plus
// a typed tool generated for each verb marked verbs.MarkChatTool — the tree is
// the only list (crit. 6), and only verbs classified chat are offered.
func chatTools(tree func() *cobra.Command) []llm.Tool {
	var enum, lifecycle []string
	var typed []llm.Tool
	for _, v := range verbs.From(tree()) {
		if v.Lifecycle {
			lifecycle = append(lifecycle, v.Path[strings.LastIndex(v.Path, " ")+1:])
		}
		if v.Class != verbs.ClassChat {
			continue // lifecycle, local and unclassified are never offered (C4)
		}
		enum = append(enum, v.Path)
		if !v.ChatTool {
			continue
		}
		if t, ok := typedTool(v); ok {
			typed = append(typed, t)
		}
	}
	slices.Sort(lifecycle)
	return append([]llm.Tool{genericTool(enum, slices.Compact(lifecycle))}, typed...)
}

// typedTool generates a verb's typed schema from its definition: positionals
// from Use (required strings), every visible flag but --file with its JSON
// type, and stdin for the content --file would carry.
func typedTool(v verbs.Verb) (llm.Tool, bool) {
	pos, err := v.Positionals()
	if err != nil {
		return llm.Tool{}, false // TestChatToolMarksAreSafe keeps marked verbs parseable
	}
	props := map[string]any{"stdin": map[string]any{"type": "string", "description": "conteúdo que o comando leria da entrada padrão"}}
	for _, p := range pos {
		props[p] = map[string]any{"type": "string"}
	}
	for name, typ := range v.Flags {
		if name != "file" {
			props[name] = jsonType(typ)
		}
	}
	return llm.Tool{
		Name:        typedName(v.Path),
		Description: fmt.Sprintf("Executa `cerne %s` (%s) no turno confirmado. Mesmo resultado da ferramenta cerne: relate o id do campo id.", v.Path, v.Short),
		Schema:      map[string]any{"type": "object", "properties": props, "required": append([]string{}, pos...)},
	}, true
}

func jsonType(pflagType string) map[string]any {
	switch pflagType {
	case "bool":
		return map[string]any{"type": "boolean"}
	case "int", "int32", "int64", "count":
		return map[string]any{"type": "integer"}
	case "stringArray", "stringSlice":
		return map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
	}
	return map[string]any{"type": "string"}
}

// genericTool is the tool over the whole offered catalog; the refused
// lifecycle verbs it names come from the marks (C19), never a hand list.
func genericTool(enum, lifecycle []string) llm.Tool {
	return llm.Tool{
		Name: chatToolName,
		Description: "Executa um comando do cerne neste projeto, no turno que o usuário confirmou no card. " +
			"O resultado é JSON: ok, id (a entidade criada ou alterada, quando houver), output (a saída do cerne) " +
			"ou error {code, message}. Relate SEMPRE o id do campo id, nunca um id suposto; em erro, relate o code e a mensagem. " +
			"Verbos de ciclo de vida (" + strings.Join(lifecycle, ", ") + ") e os operacionais não são oferecidos e " +
			"são recusados: devolva o comando para o usuário rodar no terminal.",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"verb": map[string]any{"type": "string", "enum": enum, "description": "o verbo do cerne, sem o prefixo cerne (ex.: reference new)"},
				"args": map[string]any{"type": "array", "items": map[string]any{"type": "string"},
					"description": "argumentos e flags na ordem da linha de comando (ex.: [\"Título\", \"--url\", \"https://…\"]); sem --dir e sem --file"},
				"stdin": map[string]any{"type": "string", "description": "conteúdo que o comando leria da entrada padrão (o corpo de uma spec, nota ou referência)"},
			},
			"required": []string{"verb"},
		},
	}
}

// decodeToolCall reads a tool input into a verbs.Call: the generic tool's
// {verb, args, stdin}, or a typed tool of ANY catalog verb (lifecycle and
// local included, so a forged typed name still meets the executor's refusal).
func decodeToolCall(root *cobra.Command, tc llm.ToolCall) (verbs.Call, error) {
	var c verbs.Call
	if tc.Name == chatToolName {
		if err := json.Unmarshal(tc.Input, &c); err != nil {
			return c, fmt.Errorf("%w: tool input: %v", verbs.ErrInvalidArguments, err)
		}
		return c, nil
	}
	for _, v := range verbs.From(root) {
		if typedName(v.Path) == tc.Name {
			return decodeTyped(v, tc.Input)
		}
	}
	return c, fmt.Errorf("%w: no tool named %q", verbs.ErrUnknownVerb, tc.Name)
}

// decodeTyped turns a typed input into the generic argv: --flag=value sorted
// by name (a bool true is --flag, a list repeats the flag), then "--" and the
// positionals in Use order.
func decodeTyped(v verbs.Verb, input json.RawMessage) (verbs.Call, error) {
	call := verbs.Call{Verb: v.Path}
	if v.Class != verbs.ClassChat {
		return call, nil // verbs.Run refuses it before reading anything else
	}
	bad := func(format string, a ...any) (verbs.Call, error) {
		return call, fmt.Errorf("%w: %s", verbs.ErrInvalidArguments, fmt.Sprintf(format, a...))
	}
	pos, err := v.Positionals()
	if err != nil {
		return bad("%v", err)
	}
	var in map[string]any
	if err := json.Unmarshal(input, &in); err != nil {
		return bad("tool input: %v", err)
	}
	if s, ok := in["stdin"]; ok {
		if call.Stdin, ok = s.(string); !ok {
			return bad("stdin must be a string")
		}
		delete(in, "stdin")
	}
	var positionals []string
	for _, p := range pos {
		s, ok := in[p].(string)
		if !ok {
			return bad("%s is required (string)", p)
		}
		positionals = append(positionals, s)
		delete(in, p)
	}
	for _, name := range slices.Sorted(maps.Keys(in)) {
		if _, ok := v.Flags[name]; !ok || name == "file" {
			return bad("unknown field %q", name)
		}
		switch val := in[name].(type) {
		case bool:
			if val {
				call.Args = append(call.Args, "--"+name)
			} else {
				call.Args = append(call.Args, "--"+name+"=false")
			}
		case string:
			call.Args = append(call.Args, "--"+name+"="+val)
		case float64:
			call.Args = append(call.Args, "--"+name+"="+strconv.FormatFloat(val, 'f', -1, 64))
		case []any:
			for _, e := range val {
				call.Args = append(call.Args, fmt.Sprintf("--%s=%v", name, e))
			}
		default:
			return bad("field %q has an unsupported type", name)
		}
	}
	if len(positionals) > 0 { // C13: after "--", a value is never read as a flag
		call.Args = append(append(call.Args, "--"), positionals...)
	}
	return call, nil
}

// toolRun is the turn's tool callback: decode, run under env's gate on a fresh
// tree, record the run for the UI and hand the structured result to the model
// (isError when not ok). No policy lives here — the gate is verbs.Run's.
func toolRun(env verbs.Env, runs *[]chatRun) func(context.Context, llm.ToolCall) (string, bool) {
	return func(ctx context.Context, tc llm.ToolCall) (string, bool) {
		var res verbs.Result
		root := CommandTree() // fresh per call: Run parses flags into it
		call, err := decodeToolCall(root, tc)
		if err != nil {
			res = verbs.Fail(err)
		} else {
			// C12: the child outlives the request — a write killed mid-flight
			// leaves the backend lock and an unpushed commit; only the
			// executor's own per-call timeout stops it.
			res = runVerb(context.WithoutCancel(ctx), root, call, env)
		}
		*runs = append(*runs, chatRun{Tool: tc.Name, Verb: call.Verb, Args: call.Args, Result: res})
		out, _ := json.Marshal(res)
		return string(out), !res.OK
	}
}

// cardKeys reads the confirmed card — the agent message at confirm_turn — into
// the verbs.Key of each of its commands (F-0323 C3): the only calls the turn
// may run. Lines are read like a shell: quotes, # comments, heredoc bodies
// (stdin, not commands); a line that is not a cerne command, or does not parse,
// authorizes nothing. Never nil: a card with no command binds to none.
func cardKeys(card string) []string {
	keys := []string{}
	open := strings.Index(card, "```"+app.ChatCommandFence)
	if open < 0 {
		return keys
	}
	body := card[open+3+len(app.ChatCommandFence):]
	if end := strings.Index(body, "```"); end >= 0 {
		body = body[:end]
	}
	lines := strings.Split(body, "\n")
	for i := 0; i < len(lines); i++ {
		argv, heredoc, err := shellSplit(lines[i])
		if heredoc != "" { // skip the body: it is the command's stdin
			for i++; i < len(lines) && strings.TrimSpace(lines[i]) != heredoc; i++ {
			}
		}
		if err != nil || len(argv) < 2 || argv[0] != "cerne" {
			continue
		}
		if k, err := verbs.Key(CommandTree(), argv[1:]); err == nil {
			keys = append(keys, k)
		}
	}
	return keys
}

// shellSplit splits one command line the way sh would for a plain command:
// single quotes literal, double quotes with \" \\ \$ \` escapes, backslash
// escapes outside quotes, an unquoted # starting a comment. A <<DELIM / <<'DELIM'
// redirect is removed and its delimiter returned. Anything else (pipes, &&, $(…))
// is left as literal words, which no catalog command accepts — fail closed.
func shellSplit(line string) (argv []string, heredoc string, err error) {
	var word strings.Builder
	inWord, quote := false, byte(0)
	flush := func() {
		if inWord {
			argv = append(argv, word.String())
		}
		word.Reset()
		inWord = false
	}
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case quote == '\'':
			if c == '\'' {
				quote = 0
			} else {
				word.WriteByte(c)
			}
		case quote == '"':
			if c == '"' {
				quote = 0
			} else if c == '\\' && i+1 < len(line) && strings.IndexByte("\"\\$`", line[i+1]) >= 0 {
				i++
				word.WriteByte(line[i])
			} else {
				word.WriteByte(c)
			}
		case c == '\'' || c == '"':
			quote, inWord = c, true
		case c == '\\' && i+1 < len(line):
			i++
			word.WriteByte(line[i])
			inWord = true
		case c == ' ' || c == '\t':
			flush()
		case c == '#' && !inWord:
			i = len(line)
		default:
			word.WriteByte(c)
			inWord = true
		}
	}
	if quote != 0 {
		return nil, "", fmt.Errorf("unterminated quote in %q", line)
	}
	flush()
	for i, w := range argv {
		if !strings.HasPrefix(w, "<<") {
			continue
		}
		delim, end := strings.TrimLeft(strings.TrimPrefix(w, "<<"), "-"), i+1
		if delim == "" && i+1 < len(argv) {
			delim, end = argv[i+1], i+2
		}
		return append(argv[:i:i], argv[end:]...), delim, nil
	}
	return argv, "", nil
}
