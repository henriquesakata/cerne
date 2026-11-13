package cost

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/doclang"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gauntlet"
)

// jline is one synthetic transcript line of any type, content as Claude Code
// writes it (a string or a block list).
func jline(sess, agent, typ string, ts time.Time, content any) string {
	o := map[string]any{"type": typ, "timestamp": ts.Format(time.RFC3339Nano), "sessionId": sess, "isSidechain": true, "agentId": agent,
		"message": map[string]any{"role": typ, "content": content}}
	raw, _ := json.Marshal(o)
	return string(raw)
}

func ask(sess, agent string, ts time.Time, text string) string {
	return jline(sess, agent, "user", ts, text)
}

func toolResult(sess, agent string, ts time.Time, body string) string {
	return jline(sess, agent, "user", ts, []map[string]any{{"type": "tool_result", "tool_use_id": "t1", "content": body}})
}

func reply(sess, agent string, ts time.Time, text string) string {
	return jline(sess, agent, "assistant", ts, []map[string]any{{"type": "text", "text": text}})
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
			[]string{ask("s", "x", at(1), askVerdict), reply("s", "x", at(2), "Penso.")}, "judge"},
		{"judge, only 'read this file', verdict ends the last reply", "general-purpose",
			[]string{ask("s", "x", at(1), "Leia /tmp/a.md e /tmp/b.md."), toolResult("s", "x", at(2), "texto"), reply("s", "x", at(3), "Porque sim.\n\nVEREDITO: B")}, "judge"},
		{"critic, diff fence inline", "general-purpose",
			[]string{ask("s", "x", at(1), "Ataque o diff:\n"+diffFence), reply("s", "x", at(2), "Achei um bug.")}, "critic"},
		{"critic, reads the package from a file (cat -n prefix)", "general-purpose",
			[]string{ask("s", "x", at(1), "Leia /tmp/pacote.md"), toolResult("s", "x", at(2), "     1→# Pacote\n     7→=== DIFF (start) ===\n     8→+x"), reply("s", "x", at(3), "Achado 1.")}, "critic"},
		{"critic, code-reviewer", "code-reviewer", []string{ask("s", "x", at(1), "Revise."), reply("s", "x", at(2), "ok")}, "critic"},
		{"both signals: judge", "general-purpose",
			[]string{ask("s", "x", at(1), "Leia o pacote."), toolResult("s", "x", at(2), diffFence), reply("s", "x", at(3), "VERDICT: TIE")}, "judge"},
		{"no signal", "general-purpose", []string{ask("s", "x", at(1), "Faça X."), reply("s", "x", at(2), "Feito.")}, ""},
		{"source code that mentions the fence is no diff", "general-purpose",
			[]string{ask("s", "x", at(1), "Leia artifact.go"), toolResult("s", "x", at(2), "     36\t\"=== DIFF (início) ===\", \"=== DIFF (fim) ===\","), reply("s", "x", at(3), "ok")}, ""},
		{"a brief citing the verdict shape is not asking for it", "general-purpose",
			[]string{ask("s", "x", at(1), "O juiz fecha com VEREDITO: A|B|EMPATE."), reply("s", "x", at(2), "ok")}, ""},
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
		if got := classOf(t, "general-purpose", ask("s", "x", at(1), "Leia."), toolResult("s", "x", at(2), art)); got != "critic" {
			t.Errorf("lang %v: artifact read gives class %q, want critic", l, got)
		}
	}
}

// TestStampAttributesGauntletRolesWithoutAttribute (F-0396 crit. 9): the cost
// of a feature whose critic and judge ran as general-purpose subagents, with
// no `cost attribute`, comes out by role critic and judge — and a role bound
// to a subagent still wins over what its transcript says.
func TestStampAttributesGauntletRolesWithoutAttribute(t *testing.T) {
	in := fixture(t)
	saveFeature(t, in.RepoDir, "F-0001", domain.StateImplemented)
	writeLines(t, in.Roots[0], "-tmp-p/s.jsonl", tmsg{ts: at(2), id: "m", req: "r", c: cc(1, 0, 0, 1)}.line("s"))
	use := func(agent string, n int, c Counters) string {
		return tmsg{ts: at(n), id: agent, req: "r", agent: agent, c: c}.line("s")
	}
	writeLines(t, in.Roots[0], "-tmp-p/s/subagents/agent-c.jsonl",
		ask("s", "c", at(3), "Ataque o diff:\n"+diffFence), use("c", 4, cc(2, 0, 0, 2)))
	writeLines(t, in.Roots[0], "-tmp-p/s/subagents/agent-j.jsonl",
		ask("s", "j", at(5), "Leia /tmp/a.md"), reply("s", "j", at(6), "Razão.\nVEREDITO: A"), use("j", 7, cc(4, 0, 0, 4)))
	writeLines(t, in.Roots[0], "-tmp-p/s/subagents/agent-o.jsonl",
		ask("s", "o", at(8), "Leia /tmp/a.md"), reply("s", "o", at(9), "VEREDITO: B"), use("o", 10, cc(8, 0, 0, 8)))
	bindAt(t, in.CacheDir, "s", "", "F-0001", at(1))
	if err := AppendBind(in.CacheDir, Bind{Session: "s", Agent: "o", Entity: "F-0001", Harness: HarnessClaudeCode, Role: "builder", Retro: true, TS: at(11)}); err != nil {
		t.Fatal(err)
	}
	want := map[string]Counters{"main": cc(1, 0, 0, 1), "critic": cc(2, 0, 0, 2), "judge": cc(4, 0, 0, 4), "builder": cc(8, 0, 0, 8)}
	if r := compute(t, in, "F-0001"); !reflect.DeepEqual(r.ByRole, want) {
		t.Fatalf("ByRole = %+v, want %+v", r.ByRole, want)
	}
}
