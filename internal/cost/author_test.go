package cost

import (
	"encoding/json"
	"testing"
	"time"
)

// tline is one synthetic transcript line for AuthorModel: an assistant message
// (optionally carrying a tool_use) or, with result set, the user line
// answering tool_use id.
type tline struct {
	ts     time.Time
	agent  string // subagent id; "" on the main thread
	model  string
	id     string // tool_use id
	cmd    string // command of the tool_use; "" for a message without one
	tool   string // tool name; "" means Bash
	result bool   // the tool_result for id instead of an assistant message
}

func (l tline) json(sess string) string {
	o := map[string]any{"timestamp": l.ts.Format(time.RFC3339Nano), "sessionId": sess}
	if l.agent != "" {
		o["isSidechain"], o["agentId"] = true, l.agent
	}
	if l.result {
		o["type"] = "user"
		o["message"] = map[string]any{"role": "user", "content": []any{
			map[string]any{"type": "tool_result", "tool_use_id": l.id, "content": "ok"}}}
	} else {
		content := []any{map[string]any{"type": "text", "text": "thinking about it"}}
		if l.cmd != "" {
			tool := l.tool
			if tool == "" {
				tool = "Bash"
			}
			content = []any{map[string]any{"type": "tool_use", "id": l.id, "name": tool, "input": map[string]any{"command": l.cmd}}}
		}
		o["type"] = "assistant"
		o["message"] = map[string]any{"id": "msg-" + l.id, "model": l.model, "role": "assistant",
			"content": content, "usage": map[string]any{"input_tokens": 1, "output_tokens": 1}}
	}
	raw, _ := json.Marshal(o)
	return string(raw)
}

// writeTranscript writes sess's main file and its subagent files from lines.
func writeTranscript(t *testing.T, root, sess string, lines ...tline) {
	t.Helper()
	byFile := map[string][]string{}
	for _, l := range lines {
		rel := "-tmp-p/" + sess + ".jsonl"
		if l.agent != "" {
			rel = "-tmp-p/" + sess + "/subagents/agent-" + l.agent + ".jsonl"
		}
		byFile[rel] = append(byFile[rel], l.json(sess))
	}
	for rel, ls := range byFile {
		writeLines(t, root, rel, ls...)
	}
}

// TestAuthorModel (F-0375 crit. 2, 3): the model is the one of the assistant
// whose Bash tool_use running this cerne invocation has no tool_result yet —
// in the main file or a subagent's; two such pending calls with different
// models are ambiguous ("" but found); none pending falls back to the last
// assistant message of the session; no transcript is not found.
func TestAuthorModel(t *testing.T) {
	const sess = "s-author"
	push := []string{"push"}
	cases := []struct {
		name      string
		lines     []tline
		inv       []string
		wantModel string
		wantFound bool
	}{
		{"main session, pending push", []tline{
			{ts: at(0), model: "claude-sonnet-5", id: "t0", cmd: "cerne pull"},
			{ts: at(1), id: "t0", result: true},
			{ts: at(2), model: "claude-opus-5-5", id: "t1", cmd: "cd /w && cerne push 2>&1 | tail -3"},
		}, push, "claude-opus-5-5", true},
		{"subagent pending, main thread waits on its Agent call", []tline{
			{ts: at(0), model: "claude-opus-5-5", id: "a1", cmd: "cerne push for me", tool: "Agent"},
			{ts: at(1), agent: "dev", model: "claude-sonnet-5", id: "d1", cmd: "cerne feature spec F-0001 --file s.md"},
		}, []string{"feature", "spec"}, "claude-sonnet-5", true},
		{"answered call is not the running one", []tline{
			{ts: at(0), agent: "old", model: "claude-haiku-5", id: "o1", cmd: "cerne push"},
			{ts: at(1), agent: "old", id: "o1", result: true},
			{ts: at(2), agent: "dev", model: "claude-sonnet-5", id: "d1", cmd: "/usr/local/bin/cerne push"},
		}, push, "claude-sonnet-5", true},
		{"two subagents pending with different models: ambiguous", []tline{
			{ts: at(0), agent: "dev1", model: "claude-sonnet-5", id: "d1", cmd: "cerne push"},
			{ts: at(1), agent: "dev2", model: "claude-opus-5-5", id: "d2", cmd: "cerne push"},
		}, push, "", true},
		{"two pending with the same model agree", []tline{
			{ts: at(0), agent: "dev1", model: "claude-sonnet-5", id: "d1", cmd: "cerne push"},
			{ts: at(1), agent: "dev2", model: "claude-sonnet-5", id: "d2", cmd: "cerne sync;cerne push"},
		}, push, "claude-sonnet-5", true},
		{"another verb is not this invocation: last message wins", []tline{
			{ts: at(0), agent: "dev", model: "claude-sonnet-5", id: "d1", cmd: "cerne pull"},
			{ts: at(1), model: "claude-opus-5-5", id: "m1"},
		}, push, "claude-opus-5-5", true},
		{"a longer verb does not match a prefix", []tline{
			{ts: at(0), agent: "dev", model: "claude-sonnet-5", id: "d1", cmd: "cerne pushx"},
			{ts: at(1), model: "claude-opus-5-5", id: "m1"},
		}, push, "claude-opus-5-5", true},
		{"no transcript for the session", nil, push, "", false},
		// Critic round 1, item 2: the verb words skip cerne's flags and their
		// values on both sides; only a call that starts a command segment
		// counts; no verb words match nothing.
		{"global flags and their values are skipped", []tline{
			{ts: at(0), agent: "dev", model: "claude-sonnet-5", id: "d1", cmd: "cerne --dir /q push --dry-run"},
			{ts: at(1), model: "claude-opus-5-5", id: "m1"},
		}, []string{"--dir", "/p", "push"}, "claude-sonnet-5", true},
		{"a quoted mention is not a call", []tline{
			{ts: at(0), agent: "dev", model: "claude-sonnet-5", id: "d1", cmd: `git commit -m "fix: cerne push now records"`},
			{ts: at(1), model: "claude-opus-5-5", id: "m1"},
		}, push, "claude-opus-5-5", true},
		{"an argument of another command is not a call", []tline{
			{ts: at(0), agent: "dev", model: "claude-sonnet-5", id: "d1", cmd: "echo cerne push"},
			{ts: at(1), model: "claude-opus-5-5", id: "m1"},
		}, push, "claude-opus-5-5", true},
		{"env and VAR= prefixes still start the call", []tline{
			{ts: at(0), agent: "dev", model: "claude-sonnet-5", id: "d1", cmd: "cd /w && (FOO=1 env BAR=2 ./bin/cerne push)"},
			{ts: at(1), model: "claude-opus-5-5", id: "m1"},
		}, push, "claude-sonnet-5", true},
		{"no verb words match nothing", []tline{
			{ts: at(0), agent: "dev", model: "claude-sonnet-5", id: "d1", cmd: "cerne push"},
			{ts: at(1), model: "claude-opus-5-5", id: "m1"},
		}, []string{"--verbose"}, "claude-opus-5-5", true},
	}
	// A request blob repeats the running call with no model and another type:
	// neither a pending call nor the last message.
	blob := `{"type":"api-request-blob","sessionId":"s-author","message":{"role":"assistant","content":[{"type":"tool_use","id":"b1","name":"Bash","input":{"command":"cerne push"}}]}}`
	t.Run("a request blob is not a message", func(t *testing.T) {
		root := t.TempDir()
		writeLines(t, root, "-tmp-p/"+sess+".jsonl",
			tline{ts: at(0), model: "claude-opus-5-5", id: "m1"}.json(sess), blob)
		if model, found := AuthorModel([]string{root}, sess, push); model != "claude-opus-5-5" || !found {
			t.Fatalf("AuthorModel = (%q, %v), want the last real message's model", model, found)
		}
	})
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeTranscript(t, root, sess, tc.lines...)
			writeTranscript(t, root, "other-session", tline{ts: at(9), model: "claude-haiku-5", id: "x", cmd: "cerne push"})
			model, found := AuthorModel([]string{root}, sess, tc.inv)
			if model != tc.wantModel || found != tc.wantFound {
				t.Fatalf("AuthorModel = (%q, %v), want (%q, %v)", model, found, tc.wantModel, tc.wantFound)
			}
		})
	}
}
