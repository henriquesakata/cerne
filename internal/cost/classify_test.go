package cost

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/doclang"
	"github.com/henriquesakata/cerne/internal/gauntlet"
)

// jline is one synthetic transcript line of any type, content as Claude Code
// writes it (a string or a block list).
func jline(sess, typ string, ts time.Time, content any) string {
	o := map[string]any{"type": typ, "timestamp": ts.Format(time.RFC3339Nano), "sessionId": sess, "isSidechain": true, "agentId": "x",
		"message": map[string]any{"role": typ, "content": content}}
	raw, _ := json.Marshal(o)
	return string(raw)
}

func ask(sess string, ts time.Time, text string) string { return jline(sess, "user", ts, text) }

func toolResult(sess string, ts time.Time, body string) string {
	return jline(sess, "user", ts, []map[string]any{{"type": "tool_result", "tool_use_id": "t1", "content": body}})
}

func reply(sess string, ts time.Time, text string) string {
	return jline(sess, "assistant", ts, []map[string]any{{"type": "text", "text": text}})
}

const diffFence = "=== DIFF (início) ===\ndiff --git a/x b/x\n=== DIFF (fim) ==="

// classOf writes one subagent transcript (kind is its agentType) and returns
// the class readSession gives its messages.
func classOf(t *testing.T, kind string, lines ...string) string {
	t.Helper()
	root := t.TempDir()
	usage := tmsg{ts: at(9), id: "u", req: "r", agent: "x", c: cc(1, 0, 0, 1)}.line("s")
	p := writeLines(t, root, "-tmp-p/s/subagents/agent-x.jsonl", append(lines, usage)...)
	if kind != "" {
		writeLines(t, root, "-tmp-p/s/subagents/agent-x.meta.json", `{"agentType":"`+kind+`"}`)
	}
	msgs, err := readSession("s", []tfile{{rel: "-tmp-p/s/subagents/agent-x.jsonl", abs: p}})
	if err != nil || len(msgs) != 1 {
		t.Fatalf("readSession: %v, %d msgs", err, len(msgs))
	}
	return msgs[0].class
}

// TestClassifyRole (F-0396 crit. 1): judge by the verdict line ending its last
// reply or asked in its first message; critic by agentType code-reviewer or by
// the diff fence in the first message or in a tool_result; both signals, judge.
func TestClassifyRole(t *testing.T) {
	askVerdict := "Compare A e B. Termine com a linha fixa `VEREDITO: A`, `VEREDITO: B` ou `VEREDITO: EMPATE`."
	for _, c := range []struct {
		name, kind string
		lines      []string
		want       string
	}{
		{"judge, verdict asked in the first message", "general-purpose",
			[]string{ask("s", at(1), askVerdict), reply("s", at(2), "Penso.")}, "judge"},
		{"judge, only 'read this file', verdict ends the last reply", "general-purpose",
			[]string{ask("s", at(1), "Leia /tmp/a.md e /tmp/b.md."), toolResult("s", at(2), "texto"), reply("s", at(3), "Porque sim.\n\nVEREDITO: B")}, "judge"},
		{"critic, diff fence inline", "general-purpose",
			[]string{ask("s", at(1), "Ataque o diff:\n"+diffFence), reply("s", at(2), "Achei um bug.")}, "critic"},
		{"critic, reads the package from a file (cat -n prefix)", "general-purpose",
			[]string{ask("s", at(1), "Leia /tmp/pacote.md"), toolResult("s", at(2), "     1→# Pacote\n     7→=== DIFF (start) ===\n     8→+x"), reply("s", at(3), "Achado 1.")}, "critic"},
		{"critic, code-reviewer", "code-reviewer", []string{ask("s", at(1), "Revise."), reply("s", at(2), "ok")}, "critic"},
		{"both signals: judge", "general-purpose",
			[]string{ask("s", at(1), "Leia o pacote."), toolResult("s", at(2), diffFence), reply("s", at(3), "VERDICT: TIE")}, "judge"},
		{"no signal", "general-purpose", []string{ask("s", at(1), "Faça X."), reply("s", at(2), "Feito.")}, ""},
		{"source code that mentions the fence is no diff", "general-purpose",
			[]string{ask("s", at(1), "Leia artifact.go"), toolResult("s", at(2), "     36\t\"=== DIFF (início) ===\", \"=== DIFF (fim) ===\","), reply("s", at(3), "ok")}, ""},
		{"a brief citing the verdict shape is not asking for it", "general-purpose",
			[]string{ask("s", at(1), "O juiz fecha com VEREDITO: A|B|EMPATE."), reply("s", at(2), "ok")}, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := classOf(t, c.kind, c.lines...); got != c.want {
				t.Fatalf("class = %q, want %q", got, c.want)
			}
		})
	}
}

// TestFenceMatchesTheArtifact pins the classifier's fence to the one the real
// gauntlet artifact writes, in both languages.
func TestFenceMatchesTheArtifact(t *testing.T) {
	for _, l := range doclang.Langs() {
		art, err := gauntlet.Assemble(gauntlet.Input{Lang: l, Spec: "## " + doclang.Sections(doclang.Spec, l)[1] + "\n\n1. x\n", Diff: "diff --git a/x b/x\n"})
		if err != nil {
			t.Fatal(err)
		}
		if got := classOf(t, "general-purpose", ask("s", at(1), "Leia."), toolResult("s", at(2), art)); got != "critic" {
			t.Errorf("lang %v: artifact read gives class %q, want critic", l, got)
		}
	}
}
