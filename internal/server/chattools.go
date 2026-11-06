package server

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
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

const chatToolName = "cerne"

// chatRun is one tool execution of a confirmed turn, as the UI shows it.
type chatRun struct {
	Tool   string       `json:"tool"`
	Verb   string       `json:"verb"`
	Args   []string     `json:"args"`
	Result verbs.Result `json:"result"`
}

// chatTools renders the verb catalog of tree as the confirmed turn's tools
// (F-0323): the generic tool, whose verb enum is every non-lifecycle verb —
// the tree is the only list (crit. 6).
func chatTools(tree func() *cobra.Command) []llm.Tool {
	var enum []string
	for _, v := range verbs.From(tree()) {
		if !v.Lifecycle {
			enum = append(enum, v.Path)
		}
	}
	return []llm.Tool{{
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
	}}
}

// decodeToolCall reads the generic tool's input into a verbs.Call.
func decodeToolCall(tc llm.ToolCall) (verbs.Call, error) {
	var c verbs.Call
	if tc.Name != chatToolName {
		return c, fmt.Errorf("%w: no tool named %q", verbs.ErrUnknownVerb, tc.Name)
	}
	if err := json.Unmarshal(tc.Input, &c); err != nil {
		return c, fmt.Errorf("%w: tool input: %v", verbs.ErrInvalidArguments, err)
	}
	return c, nil
}

// toolRun is the turn's tool callback: decode, run under env's gate on a fresh
// tree, record the run for the UI and hand the structured result to the model
// (isError when not ok). No policy lives here — the gate is verbs.Run's.
func toolRun(env verbs.Env, runs *[]chatRun) func(context.Context, llm.ToolCall) (string, bool) {
	return func(ctx context.Context, tc llm.ToolCall) (string, bool) {
		var res verbs.Result
		call, err := decodeToolCall(tc)
		if err != nil {
			res = verbs.Fail(err)
		} else {
			res = runVerb(ctx, CommandTree(), call, env)
		}
		*runs = append(*runs, chatRun{Tool: tc.Name, Verb: call.Verb, Args: call.Args, Result: res})
		out, _ := json.Marshal(res)
		return string(out), !res.OK
	}
}
