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

	"gitlab.com/rockt/cerne/internal/llm"
	"gitlab.com/rockt/cerne/internal/verbs"
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
// the only list (crit. 6).
func chatTools(tree func() *cobra.Command) []llm.Tool {
	var enum []string
	var typed []llm.Tool
	for _, v := range verbs.From(tree()) {
		if v.Lifecycle {
			continue
		}
		enum = append(enum, v.Path)
		if !v.ChatTool {
			continue
		}
		if t, ok := typedTool(v); ok {
			typed = append(typed, t)
		}
	}
	return append([]llm.Tool{genericTool(enum)}, typed...)
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

func genericTool(enum []string) llm.Tool {
	return llm.Tool{
		Name: chatToolName,
		Description: "Executa um comando do cerne neste projeto, no turno que o usuário confirmou no card. " +
			"O resultado é JSON: ok, id (a entidade criada ou alterada, quando houver), output (a saída do cerne) " +
			"ou error {code, message}. Relate SEMPRE o id do campo id, nunca um id suposto; em erro, relate o code e a mensagem. " +
			"Verbos de ciclo de vida (approve, start, complete, accept, reject, revert, substituir, deliver, retire) " +
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
// {verb, args, stdin}, or a typed tool of ANY catalog verb (lifecycle
// included, so a forged typed name still meets the executor's refusal).
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

// decodeTyped turns a typed input into the generic argv: positionals in Use
// order, then --flag=value sorted by name (a bool true is --flag, a list
// repeats the flag).
func decodeTyped(v verbs.Verb, input json.RawMessage) (verbs.Call, error) {
	call := verbs.Call{Verb: v.Path}
	if v.Lifecycle {
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
	for _, p := range pos {
		s, ok := in[p].(string)
		if !ok {
			return bad("%s is required (string)", p)
		}
		call.Args = append(call.Args, s)
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
			res = runVerb(ctx, root, call, env)
		}
		*runs = append(*runs, chatRun{Tool: tc.Name, Verb: call.Verb, Args: call.Args, Result: res})
		out, _ := json.Marshal(res)
		return string(out), !res.OK
	}
}
