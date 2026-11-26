package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/cost"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
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
	for _, k := range []string{"CERNE_ACTOR", "CERNE_SESSION", "CERNE_PROVIDER", "CERNE_MODEL", "CERNE_ORIGIN", "CURSOR_AGENT"} {
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
		// The chat's model selection is never passed to claude-cli either:
		// recording it would invent the model (critic round 1, I1).
		{"chat child: claude-cli ignores the call's model", map[string]string{"CERNE_ACTOR": "agent", "CERNE_ORIGIN": "chat", "CERNE_MODEL": "claude-sonnet-5"},
			"[llm]\nmode = \"claude-cli\"\n", "",
			DocAuthor{Provider: "claude-cli", Source: "llm-config"}},
		// F-0440: a turn switched to another installed CLI writes as that CLI.
		{"chat child: a switched turn names the CLI that answered", map[string]string{"CERNE_ACTOR": "agent", "CERNE_ORIGIN": "chat", "CERNE_PROVIDER": "openai", "CERNE_MODEL": "gpt-x"},
			"[llm]\nmode = \"claude-cli\"\n", "",
			DocAuthor{Provider: "codex-cli", Model: "gpt-x", Source: "llm-config"}},
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
	// F-0440: the other subscription CLIs answer as themselves.
	if got := LLMAuthor(project.LLMConfig{Mode: "codex-cli", Model: "gpt-x"}); got != (DocAuthor{Provider: "codex-cli", Model: "gpt-x", Source: "llm-config"}) {
		t.Fatalf("LLMAuthor codex-cli = %+v", got)
	}
	// F-0445: copilot-cli credits the provider and the model the call named,
	// and only the provider when none was (the CLI picked one itself).
	if got := LLMAuthor(project.LLMConfig{Mode: "copilot-cli", Model: "claude-sonnet-5.5"}); got != (DocAuthor{Provider: "copilot-cli", Model: "claude-sonnet-5.5", Source: "llm-config"}) {
		t.Fatalf("LLMAuthor copilot-cli = %+v", got)
	}
	if got := LLMAuthor(project.LLMConfig{Mode: "copilot-cli"}); got != (DocAuthor{Provider: "copilot-cli", Source: "llm-config"}) {
		t.Fatalf("LLMAuthor copilot-cli without a model = %+v", got)
	}
}

// F-0445 critic: a copilot-cli document credits the model the CLI reported
// (auto picks its own), then the configured one, then none.
func TestLLMAuthorAnsweredModel(t *testing.T) {
	cfg := project.LLMConfig{Mode: "copilot-cli"}
	if got := LLMAuthorAnswered(cfg, "mai-code-1.1-flash"); got != (DocAuthor{Provider: "copilot-cli", Model: "mai-code-1.1-flash", Source: "llm-config"}) {
		t.Fatalf("auto: %+v", got)
	}
	cfg.Model = "asked"
	if got := LLMAuthorAnswered(cfg, "ran"); got.Model != "ran" {
		t.Fatalf("reported must win over the configured one: %+v", got)
	}
	if got := LLMAuthorAnswered(cfg, ""); got.Model != "asked" {
		t.Fatalf("configured fallback: %+v", got)
	}
	// Only copilot-cli reads it: claude-cli never names a model, byo-key keeps its own.
	if got := LLMAuthorAnswered(project.LLMConfig{Mode: "claude-cli"}, "x"); got.Model != "" {
		t.Fatalf("claude-cli: %+v", got)
	}
	if got := LLMAuthorAnswered(project.LLMConfig{Mode: "byo-key", Provider: "openai", Model: "gpt-x"}, "x"); got.Model != "gpt-x" {
		t.Fatalf("byo-key: %+v", got)
	}
}

// TestDocWriteMarksSession (F-0486): an agent session's write of a document
// marks that session as the document's writer on this machine — the agent
// that wrote the spec is often not the one that wrote the plan or the result —
// and a human's write marks nothing.
func TestDocWriteMarksSession(t *testing.T) {
	cacheDir := t.TempDir()
	repoDir := filepath.Join(cacheDir, "repo")
	if err := store.SaveFeature(repoDir, store.Feature{ID: "F-0001", Title: "x", State: domain.StateApproved, CreatedAt: time.Now(), UpdatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CERNE_ORIGIN", "")
	t.Setenv("CERNE_SESSION", "s-plan")
	if err := RecordDocWrite(repoDir, "F-0001", "plan", DocAuthor{Provider: "anthropic", Model: "m", Source: "transcript"}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CERNE_SESSION", "s-human")
	if err := RecordDocWrite(repoDir, "F-0001", "spec", HumanAuthor()); err != nil {
		t.Fatal(err)
	}
	marks, err := cost.ReadMarks(cacheDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(marks) != 1 || marks[0].Session != "s-plan" || marks[0].Entity != "F-0001" || marks[0].Role != "plan" {
		t.Fatalf("marks = %+v, want only s-plan wrote the plan of F-0001", marks)
	}
}

// cursorProject is a project with a cache dir of its own and a live feature
// F-0001 in its clone: what a Cursor session's hooks record into and bind to.
func cursorProject(t *testing.T) (projDir, cacheDir, repoDir string) {
	t.Helper()
	t.Setenv("CERNE_CACHE_DIR", t.TempDir())
	projDir = t.TempDir()
	if err := os.WriteFile(filepath.Join(projDir, ".cernerc"), []byte("project_id = \"p\"\nbackend = \"x\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cacheDir, err := project.CacheDir("p")
	if err != nil {
		t.Fatal(err)
	}
	repoDir = filepath.Join(cacheDir, "repo")
	if err := store.SaveFeature(repoDir, store.Feature{ID: "F-0001", Title: "x", State: domain.StateInDevelopment, CreatedAt: time.Now(), UpdatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	return projDir, cacheDir, repoDir
}

// cursorSeen writes what the hooks of Cursor session sess recorded: model,
// age ago; bound binds the session to the live F-0001.
func cursorSeen(t *testing.T, cacheDir, sess, model string, age time.Duration, bound bool) {
	t.Helper()
	raw, _ := json.Marshal(cursorSession{Model: model, UpdatedAt: time.Now().Add(-age).UTC()})
	dir := filepath.Join(cacheDir, cursorSessionsSubdir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, sess+".json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if bound {
		if err := BindSession(cacheDir, sess, "F-0001"); err != nil {
			t.Fatal(err)
		}
	}
}

// TestDocAuthorCursor (F-0513 crit. 2): a Cursor agent shell's write names
// provider cursor and the model its hooks recorded when every Cursor session
// in play agrees on one, and no model when they disagree or none recorded one
// — never a guess between concurrent sessions. In play: every session
// recorded in the last 12 h, bound or not. That unambiguous evidence beats a
// declared CERNE_PROVIDER/CERNE_MODEL, which is honored only without it. The
// chat, a Claude Code transcript and a human are as before.
func TestDocAuthorCursor(t *testing.T) {
	type seen struct {
		sess, model string
		age         time.Duration
		bound       bool
	}
	none := DocAuthor{Provider: "cursor", Source: "hook"}
	cases := []struct {
		name string
		env  map[string]string
		seen []seen
		tx   string // model of a Claude Code transcript for session "s1"
		want DocAuthor
	}{
		{"one session", nil, []seen{{"ca", "grok-4.7", time.Minute, false}}, "", DocAuthor{Provider: "cursor", Model: "grok-4.7", Source: "hook"}},
		{"two sessions, one model", nil, []seen{{"ca", "grok-4.7", time.Minute, false}, {"cb", "grok-4.7", time.Hour, true}}, "", DocAuthor{Provider: "cursor", Model: "grok-4.7", Source: "hook"}},
		{"two sessions, two models", nil, []seen{{"ca", "grok-4.7", time.Minute, false}, {"cb", "gpt-6", time.Hour, false}}, "", none},
		{"two bound sessions, two models", nil, []seen{{"ca", "grok-4.7", time.Minute, true}, {"cb", "gpt-6", time.Hour, true}}, "", none},
		{"a session with no model", nil, []seen{{"ca", "grok-4.7", time.Minute, false}, {"cb", "", time.Minute, false}}, "", none},
		{"no session recorded", nil, nil, "", none},
		{"a session out of the window", nil, []seen{{"ca", "grok-4.7", time.Minute, false}, {"cb", "gpt-6", 13 * time.Hour, false}}, "", DocAuthor{Provider: "cursor", Model: "grok-4.7", Source: "hook"}},
		// Review of PR #221: cb may be the writer (feature new --file runs
		// before any feature start binds), so the binding of ca decides nothing.
		{"a bound and an unbound session, two models", nil, []seen{{"ca", "grok-4.7", time.Hour, true}, {"cb", "gpt-6", time.Minute, false}}, "", none},
		// The owner's amendment of 2026-11-26: unambiguous hook evidence beats
		// a declaration, as the transcript does on Claude Code (F-0375).
		{"declared, one agreeing session: the hook wins", map[string]string{"CERNE_PROVIDER": "xai", "CERNE_MODEL": "grok-x"}, []seen{{"ca", "grok-4.7", time.Minute, false}}, "", DocAuthor{Provider: "cursor", Model: "grok-4.7", Source: "hook"}},
		{"declared, two models: declared", map[string]string{"CERNE_PROVIDER": "xai", "CERNE_MODEL": "grok-x"}, []seen{{"ca", "grok-4.7", time.Minute, false}, {"cb", "gpt-6", time.Minute, true}}, "", DocAuthor{Provider: "xai", Model: "grok-x", Source: "declared"}},
		{"declared, no record: declared", map[string]string{"CERNE_MODEL": "grok-x"}, nil, "", DocAuthor{Model: "grok-x", Source: "declared"}},
		{"declared, a session with no model: declared", map[string]string{"CERNE_PROVIDER": "xai"}, []seen{{"ca", "", time.Minute, false}}, "", DocAuthor{Provider: "xai", Source: "declared"}},
		{"chat unchanged", map[string]string{"CERNE_ORIGIN": "chat"}, []seen{{"ca", "grok-4.7", time.Minute, false}}, "", DocAuthor{Provider: "anthropic", Model: "claude-opus-5-5", Source: "llm-config"}},
		{"claude code transcript unchanged", map[string]string{"CERNE_SESSION": "s1"}, []seen{{"ca", "grok-4.7", time.Minute, false}}, "claude-opus-5-5", DocAuthor{Provider: "anthropic", Model: "claude-opus-5-5", Source: "transcript"}},
		{"no CURSOR_AGENT unchanged", map[string]string{"CURSOR_AGENT": "", "CERNE_ACTOR": "agent"}, []seen{{"ca", "grok-4.7", time.Minute, false}}, "", DocAuthor{Source: "unknown"}},
		{"human unchanged", map[string]string{"CERNE_ACTOR": "human"}, []seen{{"ca", "grok-4.7", time.Minute, false}}, "", DocAuthor{Source: "human"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := authorEnv(t)
			projDir, cacheDir, _ := cursorProject(t)
			t.Setenv("CURSOR_AGENT", "1")
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			if tc.env["CERNE_ORIGIN"] == "chat" {
				if err := os.WriteFile(filepath.Join(projDir, ".cernerc"), []byte("project_id = \"p\"\nbackend = \"x\"\n[llm]\nmode = \"byo-key\"\nmodel = \"claude-opus-5-5\"\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if tc.tx != "" {
				writeClaudeTranscript(t, root, "s1", tc.tx)
			}
			for _, s := range tc.seen {
				cursorSeen(t, cacheDir, s.sess, s.model, s.age, s.bound)
			}
			if got := docAuthor(projDir); got != tc.want {
				t.Fatalf("docAuthor = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestRecordDocEditedCursorMark (F-0513 crit. 3): with no CERNE_SESSION, a
// Cursor agent shell's write marks the only recent Cursor session as the
// document's writer, under harness cursor, bound or not — the spec and the
// plan are written before any feature start binds — and nobody when another
// one, bound or not, is recent too.
func TestRecordDocEditedCursorMark(t *testing.T) {
	by := DocAuthor{Provider: "cursor", Model: "grok-4.7", Source: "hook"}
	cases := []struct {
		name  string
		bound []string
		free  []string
		want  string // the marked session; "" for no mark
	}{
		{"the only recent session, bound", []string{"ca"}, nil, "ca"},
		// Gauntlet round 1: the sole session is the writer, bound or not.
		{"the only recent session, unbound", nil, []string{"ca"}, "ca"},
		// Review of PR #221: the unbound cb may be the writer.
		{"a bound session beside an unbound one", []string{"ca"}, []string{"cb"}, ""},
		{"two bound sessions", []string{"ca", "cb"}, nil, ""},
		{"two unbound sessions", nil, []string{"ca", "cb"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			authorEnv(t)
			_, cacheDir, repoDir := cursorProject(t)
			t.Setenv("CURSOR_AGENT", "1")
			for _, s := range tc.bound {
				cursorSeen(t, cacheDir, s, "grok-4.7", time.Minute, true)
			}
			for _, s := range tc.free {
				cursorSeen(t, cacheDir, s, "grok-4.7", time.Minute, false)
			}
			if err := RecordDocWrite(repoDir, "F-0001", "spec", by); err != nil {
				t.Fatal(err)
			}
			marks, err := cost.ReadMarks(cacheDir)
			if err != nil {
				t.Fatal(err)
			}
			switch {
			case tc.want == "" && len(marks) != 0:
				t.Fatalf("marks = %+v, want none", marks)
			case tc.want != "" && (len(marks) != 1 || marks[0].Session != tc.want || marks[0].Harness != "cursor" || marks[0].Role != "spec"):
				t.Fatalf("marks = %+v, want %s wrote the spec under harness cursor", marks, tc.want)
			}
		})
	}
}

// cursorPending sets the pending shell command of the recorded Cursor
// session sess, age ago.
func cursorPending(t *testing.T, cacheDir, sess, command string, age time.Duration) {
	t.Helper()
	path := filepath.Join(cacheDir, cursorSessionsSubdir, sess+".json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var rec cursorSession
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatal(err)
	}
	rec.Pending, rec.PendingAt = command, time.Now().Add(-age).UTC()
	if raw, err = json.Marshal(rec); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestDocAuthorCursorPendingCommand (F-0513 crit. 6): among concurrent Cursor
// sessions on different models, the one whose pending shell command is this
// process's own cerne call (cost.Runs, Claude Code's rule) is the writer: its
// model, ahead of criterion 2 and of a declaration, and the local mark. Two
// such sessions are ambiguous, and criterion 2 decides; a pending command
// older than the window, or another cerne call, counts for nothing.
func TestDocAuthorCursorPendingCommand(t *testing.T) {
	type pending struct {
		sess, command string
		age           time.Duration
	}
	none := DocAuthor{Provider: "cursor", Source: "hook"}
	declared := map[string]string{"CERNE_PROVIDER": "xai", "CERNE_MODEL": "grok-x"}
	cases := []struct {
		name    string
		env     map[string]string
		pending []pending
		want    DocAuthor
		mark    string // the marked session; "" for no mark
	}{
		{"only cb runs this push", nil, []pending{{"cb", "cerne push", time.Minute}}, DocAuthor{Provider: "cursor", Model: "gpt-6", Source: "hook"}, "cb"},
		{"only cb runs it, ahead of a declaration", declared, []pending{{"cb", "CERNE_X=1 cerne push --dir .", time.Minute}}, DocAuthor{Provider: "cursor", Model: "gpt-6", Source: "hook"}, "cb"},
		{"both run it: criterion 2", nil, []pending{{"ca", "cerne push", time.Minute}, {"cb", "cerne push", time.Minute}}, none, ""},
		{"both run it, declared: criterion 2", declared, []pending{{"ca", "cerne push", time.Minute}, {"cb", "cerne push", time.Minute}}, DocAuthor{Provider: "xai", Model: "grok-x", Source: "declared"}, ""},
		{"a pending command out of the window", nil, []pending{{"cb", "cerne push", 13 * time.Hour}}, none, ""},
		{"another cerne call", nil, []pending{{"cb", "cerne pull", time.Minute}}, none, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			authorEnv(t) // os.Args: cerne push
			projDir, cacheDir, repoDir := cursorProject(t)
			t.Setenv("CURSOR_AGENT", "1")
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			cursorSeen(t, cacheDir, "ca", "grok-4.7", time.Minute, false)
			cursorSeen(t, cacheDir, "cb", "gpt-6", time.Minute, false)
			for _, p := range tc.pending {
				cursorPending(t, cacheDir, p.sess, p.command, p.age)
			}
			got := docAuthor(projDir)
			if got != tc.want {
				t.Fatalf("docAuthor = %+v, want %+v", got, tc.want)
			}
			if err := RecordDocWrite(repoDir, "F-0001", "spec", got); err != nil {
				t.Fatal(err)
			}
			marks, err := cost.ReadMarks(cacheDir)
			if err != nil {
				t.Fatal(err)
			}
			switch {
			case tc.mark == "" && len(marks) != 0:
				t.Fatalf("marks = %+v, want none", marks)
			case tc.mark != "" && (len(marks) != 1 || marks[0].Session != tc.mark || marks[0].Harness != "cursor"):
				t.Fatalf("marks = %+v, want %s under harness cursor", marks, tc.mark)
			}
		})
	}
}
