package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/henriquesakata/cerne/internal/project"
)

// writeClaudeTranscript writes one Claude Code session file under
// <root>/projects/: an assistant message with a pending Bash tool_use running
// `cerne push`, by model.
func writeClaudeTranscript(t *testing.T, root, sess, model string) {
	t.Helper()
	line, _ := json.Marshal(map[string]any{
		"type": "assistant", "timestamp": "2026-11-12T10:00:00Z", "sessionId": sess,
		"message": map[string]any{"id": "m1", "model": model, "usage": map[string]any{"output_tokens": 1},
			"content": []any{map[string]any{"type": "tool_use", "id": "t1", "name": "Bash", "input": map[string]any{"command": "cerne push"}}}},
	})
	p := filepath.Join(root, "projects", "-w", sess+".jsonl")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, append(line, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

// authorEnv clears every variable docAuthor reads and points the transcript
// roots at an empty temp dir, so a developer's own session never leaks in.
func authorEnv(t *testing.T) (claudeRoot string) {
	t.Helper()
	claudeRoot = t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", claudeRoot)
	t.Setenv("HOME", t.TempDir())
	for _, k := range []string{"CERNE_ACTOR", "CERNE_SESSION", "CERNE_PROVIDER", "CERNE_MODEL", "CERNE_ORIGIN"} {
		t.Setenv(k, "")
	}
	args := os.Args
	os.Args = []string{"cerne", "push"}
	t.Cleanup(func() { os.Args = args })
	return claudeRoot
}

// TestDocAuthor (F-0375 crit. 2, 4, 5, 6, 7): who wrote a document, in the
// order of the criteria — the chat's [llm] config, a human without an agent
// CERNE_ACTOR, the transcript of CERNE_SESSION, the declared
// CERNE_PROVIDER/CERNE_MODEL, and unknown, never inferred.
func TestDocAuthor(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		llm  string // the project's [llm] block; "" for none
		tx   string // model of a transcript for session "s1"; "" for none
		want DocAuthor
	}{
		{"human: no CERNE_ACTOR", nil, "", "", DocAuthor{Source: "human"}},
		{"human: a named actor is not an agent", map[string]string{"CERNE_ACTOR": "alice", "CERNE_SESSION": "s1"}, "", "claude-opus-5-5", DocAuthor{Source: "human"}},
		{"transcript beats the declaration", map[string]string{"CERNE_ACTOR": "agent", "CERNE_SESSION": "s1", "CERNE_PROVIDER": "openai", "CERNE_MODEL": "gpt-x"}, "", "claude-opus-5-5",
			DocAuthor{Provider: "anthropic", Model: "claude-opus-5-5", Source: "transcript"}},
		{"declared without a transcript", map[string]string{"CERNE_ACTOR": "agent", "CERNE_SESSION": "s1", "CERNE_PROVIDER": "openai", "CERNE_MODEL": "gpt-x"}, "", "",
			DocAuthor{Provider: "openai", Model: "gpt-x", Source: "declared"}},
		{"declared provider only", map[string]string{"CERNE_ACTOR": "agent:me@box", "CERNE_PROVIDER": "google"}, "", "",
			DocAuthor{Provider: "google", Source: "declared"}},
		{"unknown: agent with nothing", map[string]string{"CERNE_ACTOR": "agent"}, "", "", DocAuthor{Source: "unknown"}},
		{"chat child: the [llm] config, with the call's model", map[string]string{"CERNE_ACTOR": "agent", "CERNE_ORIGIN": "chat", "CERNE_SESSION": "s1", "CERNE_MODEL": "claude-sonnet-5"},
			"[llm]\nmode = \"byo-key\"\nmodel = \"claude-opus-5-5\"\n", "claude-haiku-5",
			DocAuthor{Provider: "anthropic", Model: "claude-sonnet-5", Source: "llm-config"}},
		{"chat child: claude-cli names no model", map[string]string{"CERNE_ACTOR": "agent", "CERNE_ORIGIN": "chat"},
			"[llm]\nmode = \"claude-cli\"\nmodel = \"claude-opus-5-5\"\n", "",
			DocAuthor{Provider: "claude-cli", Source: "llm-config"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := authorEnv(t)
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			if tc.tx != "" {
				writeClaudeTranscript(t, root, "s1", tc.tx)
			}
			projDir := t.TempDir()
			if err := os.WriteFile(filepath.Join(projDir, ".cernerc"), []byte("backend = \"x\"\n"+tc.llm), 0o644); err != nil {
				t.Fatal(err)
			}
			if got := docAuthor(projDir); got != tc.want {
				t.Fatalf("docAuthor = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestDocAuthorAmbiguousTranscript (F-0375 crit. 3, 5): two subagents running
// this invocation with different models give unknown, not either model.
func TestDocAuthorAmbiguousTranscript(t *testing.T) {
	root := authorEnv(t)
	t.Setenv("CERNE_ACTOR", "agent")
	t.Setenv("CERNE_SESSION", "s1")
	t.Setenv("CERNE_MODEL", "declared-model")
	for i, model := range []string{"claude-sonnet-5", "claude-opus-5-5"} {
		line, _ := json.Marshal(map[string]any{
			"type": "assistant", "timestamp": "2026-11-12T10:00:00Z", "sessionId": "s1", "isSidechain": true,
			"message": map[string]any{"id": model, "model": model, "usage": map[string]any{"output_tokens": 1},
				"content": []any{map[string]any{"type": "tool_use", "id": model, "name": "Bash", "input": map[string]any{"command": "cerne push"}}}},
		})
		p := filepath.Join(root, "projects", "-w", "s1", "subagents", "agent-"+string(rune('a'+i))+".jsonl")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, append(line, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got := docAuthor(t.TempDir()); got != (DocAuthor{Source: "unknown"}) {
		t.Fatalf("docAuthor = %+v, want unknown", got)
	}
}

// TestNamedAuthors (F-0375 crit. 6, 7): the UI editor is a human with no
// model; a draft or the chat confirmation is the [llm] config of the call.
func TestNamedAuthors(t *testing.T) {
	if got := HumanAuthor(); got != (DocAuthor{Source: "human"}) {
		t.Fatalf("HumanAuthor = %+v", got)
	}
	if got := LLMAuthor(project.LLMConfig{Mode: "byo-key", Provider: "openai", Model: "gpt-x"}); got != (DocAuthor{Provider: "openai", Model: "gpt-x", Source: "llm-config"}) {
		t.Fatalf("LLMAuthor byo-key = %+v", got)
	}
	if got := LLMAuthor(project.LLMConfig{Mode: "claude-cli", Model: "claude-opus-5-5"}); got != (DocAuthor{Provider: "claude-cli", Source: "llm-config"}) {
		t.Fatalf("LLMAuthor claude-cli = %+v (the CLI is never passed --model)", got)
	}
}
