package cost

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

// TestClassifyActivity (F-0528 crit. 3): one row per rule of the spec's table,
// plus the Bash prefixes that are not the command.
func TestClassifyActivity(t *testing.T) { // F-0528 crit. 3
	bash := func(c string) (string, map[string]any) { return "Bash", map[string]any{"command": c} }
	file := func(tool, p string) (string, map[string]any) { return tool, map[string]any{"file_path": p} }
	type in struct {
		name  string
		input map[string]any
	}
	mk := func(n string, i map[string]any) in { return in{n, i} }
	cases := []struct {
		in   in
		want string
	}{
		{mk("Read", nil), actResearch},
		{mk("Grep", nil), actResearch},
		{mk("Glob", nil), actResearch},
		{mk("WebSearch", nil), actResearch},
		{mk("WebFetch", nil), actResearch},
		{mk("ToolSearch", nil), actResearch},
		{mk("Agent", map[string]any{"subagent_type": "Explore"}), actResearch},
		{mk("Agent", map[string]any{"subagent_type": "dev"}), actDelegate},
		{mk("Agent", nil), actDelegate},
		{mk(file("Write", "/w/.docs/current/features/F-0001-x/spec.md")), actSpec},
		{mk(file("Edit", "/w/.docs/current/features/F-0001-x/plan.md")), actPlan},
		{mk(file("Write", "/w/.docs/current/features/F-0001-x/result.md")), actResult},
		{mk(file("Write", "internal/x.go")), actCode},
		{mk(file("MultiEdit", "internal/x.go")), actCode},
		{mk(file("NotebookEdit", "n.ipynb")), actCode},
		{mk(bash("cerne feature new \"T\" --file spec.md")), actSpec},
		{mk(bash("cerne feature spec F-1 --file s.md")), actSpec},
		{mk(bash("cerne feature amend F-1 --doc spec x")), actSpec},
		{mk(bash("cerne feature amend F-1 --doc plan …")), actPlan},
		{mk(bash("cerne feature amend F-1 --doc result x")), actResult},
		{mk(bash("go test ./internal/cost/ -count=1")), actTest},
		{mk(bash("go vet ./...")), actTest},
		{mk(bash("go build ./...")), actTest},
		{mk(bash("npm run build")), actTest},
		{mk(bash("npm test")), actTest},
		{mk(bash("npm ci")), actTest},
		{mk(bash("npx vitest run")), actTest},
		{mk(bash("grep -rn x .")), actResearch},
		{mk(bash("rg x")), actResearch},
		{mk(bash("cat a")), actResearch},
		{mk(bash("sed -n 1,5p a")), actResearch},
		{mk(bash("head a")), actResearch},
		{mk(bash("tail a")), actResearch},
		{mk(bash("ls")), actResearch},
		{mk(bash("find . -name x")), actResearch},
		{mk(bash("wc -l a")), actResearch},
		{mk(bash("git log --oneline")), actResearch},
		{mk(bash("git diff")), actResearch},
		{mk(bash("git status")), actResearch},
		{mk(bash("git blame a")), actResearch},
		{mk(bash("cerne cost F-1")), actResearch},
		{mk(bash("cerne note list")), actResearch},
		{mk(bash("cerne next")), actResearch},
		{mk(bash("git commit -m x")), actOther},
		{mk(bash("mkdir x")), actOther},
		{mk("Skill", nil), actOther},
		// a heredoc writes a file: the target decides (Gauntlet round 1 — the
		// agent wrote its plan and result this way, read as research)
		{mk(bash("cat > .docs/current/features/F-0001-x/plan.md <<'EOF'\n---\n> quoted\nEOF")), actPlan},
		{mk(bash("cat <<'EOF' > .docs/current/features/F-0001-x/result.md\nbody\nEOF")), actResult},
		{mk(bash("d=$(echo .docs/current/features/F-0001-*); cat > $d/spec.md <<'EOF'\nx\nEOF")), actSpec},
		{mk(bash("cat > src/calc/calc.go <<'EOF'\npackage calc\nEOF")), actCode},
		{mk(bash("tee -a internal/x.go <<EOF\nx\nEOF")), actCode},
		{mk(bash("set -e\ncat > .docs/current/features/F-0001-x/result.md <<'EOF'\nbody << not a heredoc\nEOF")), actResult},
		{mk(bash("set -e\nd=$(echo .docs/current/features/F-0001-*)\ncat > \"$d/result.md\" <<'EOF'\nx\nEOF")), actResult},
		// the answer to these calls is the person's: waiting, not working
		{mk("AskUserQuestion", nil), actWait},
		{mk("ExitPlanMode", nil), actWait},
		// a test or build anywhere in the command line, not only at its start
		{mk(bash("set -e; go test ./internal/cost/")), actTest},
		{mk(bash("go build -o bin/cerne ./cmd/cerne && ./bin/cerne cost F-1")), actTest},
		{mk(bash("(cd ui && npm run build)")), actTest},
		{mk(bash(`docker run -w /app/ui node:24 sh -c "npm ci && npm run build"`)), actTest},
		// prefixes that are not the command
		{mk(bash("VAR=x go test ./...")), actTest},
		{mk(bash("A=1 B=2 go test ./...")), actTest},
		{mk(bash("A=\"x y\" rg q")), actResearch},
		{mk(bash("cd dir && go build ./...")), actTest},
		{mk(bash("rtk proxy go test ./...")), actTest},
		{mk(bash("cd /w && A=1 rtk proxy git diff")), actResearch},
	}
	for _, c := range cases {
		raw, _ := json.Marshal(c.in.input)
		if got := classify(c.in.name, raw); got != c.want {
			t.Errorf("%s %v = %q, want %q", c.in.name, c.in.input, got, c.want)
		}
	}
}

func userLine(sess string, ts time.Time, content any, extra map[string]any) string {
	o := map[string]any{"type": "user", "timestamp": ts.Format(time.RFC3339Nano), "sessionId": sess,
		"message": map[string]any{"role": "user", "content": content}}
	for k, v := range extra {
		o[k] = v
	}
	raw, _ := json.Marshal(o)
	return string(raw)
}

func resultContent(id string) any {
	return []any{map[string]any{"type": "tool_result", "tool_use_id": id, "content": "big output"}}
}

// TestReadSessionTicks (F-0528 crit. 2, 3): one tick per assistant message at
// its last line, with the activity of its first tool_use; one per
// tool_result (the answered call's activity) and per prompt — a meta line
// included: another session's message or a task notification wakes the
// session as a prompt does; compact-summary, foreign-session user lines and
// the meta line a tool call produced (a skill's body) are out.
func TestReadSessionTicks(t *testing.T) { // F-0528 crit. 2, 3
	root := t.TempDir()
	t1 := ttool{id: "t1", name: "Read", input: map[string]any{"file_path": "a.go"}}
	t2 := ttool{id: "t2", name: "Bash", input: map[string]any{"command": "go test ./..."}}
	p := writeLines(t, root, "-tmp-p/s.jsonl",
		userLine("s", at(0), "hello", nil),
		tmsg{ts: at(1), id: "m1", req: "r1", c: cc(1, 0, 0, 1)}.line("s"),
		tmsg{ts: at(2), id: "m1", req: "r1", c: cc(1, 0, 0, 1), tools: []ttool{t1}}.line("s"),
		tmsg{ts: at(3), id: "m1", req: "r1", c: cc(1, 0, 0, 1), tools: []ttool{t2}}.line("s"),
		userLine("s", at(4), resultContent("t2"), nil),
		userLine("s", at(5), resultContent("t1"), map[string]any{"sourceToolAssistantUUID": "u1"}), // Claude Code marks a result with its call's line
		tmsg{ts: at(6), id: "m2", req: "r2", c: cc(1, 0, 0, 1)}.line("s"),
		userLine("s", at(7), "meta", map[string]any{"isMeta": true}),
		userLine("s", at(8), "summary", map[string]any{"isCompactSummary": true}),
		userLine("s", at(8), "Base directory for this skill", map[string]any{"isMeta": true, "sourceToolUseID": "t9"}),
		userLine("s", at(8), "skill body", map[string]any{"isMeta": true, "sourceToolAssistantUUID": "u9"}),
		userLine("other", at(9), "resume copy", nil),
		userLine("s", at(10), []any{map[string]any{"type": "text", "text": "again"}}, nil),
	)
	msgs, ticks, err := readSession("s", []tfile{{rel: "-tmp-p/s.jsonl", abs: p}})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Fatalf("msgs = %d, want 2 (unchanged by the tick reading)", len(msgs))
	}
	type want struct {
		min      int
		typ      tickType
		activity string
	}
	wants := []want{
		{0, tickPrompt, ""},
		{3, tickAssistant, actResearch},
		{4, tickResult, actTest},
		{5, tickResult, actResearch},
		{6, tickAssistant, actReply},
		{7, tickPrompt, ""},
		{10, tickPrompt, ""},
	}
	var got []string
	for _, k := range ticks {
		got = append(got, fmt.Sprintf("%d/%d/%s", int(k.m.ts.Sub(base)/time.Minute), k.typ, k.activity))
	}
	var exp []string
	for _, w := range wants {
		exp = append(exp, fmt.Sprintf("%d/%d/%s", w.min, w.typ, w.activity))
	}
	if fmt.Sprint(got) != fmt.Sprint(exp) {
		t.Fatalf("ticks = %v\nwant    %v", got, exp)
	}
}
