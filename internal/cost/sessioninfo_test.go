package cost

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestSessionInfosReadsOnlyLocation (F-0486 crit. 4, 5): a session's location
// comes from its transcript — the last title, folder, branch and PR, the last
// activity, each subagent's type — and nothing of the conversation does.
func TestSessionInfosReadsOnlyLocation(t *testing.T) {
	root := t.TempDir()
	p := writeLines(t, root, "-tmp-p/s1.jsonl",
		`{"type":"custom-title","customTitle":"first title","sessionId":"s1"}`,
		`{"type":"user","cwd":"/old","gitBranch":"main","sessionId":"s1","message":{"role":"user","content":"SEGREDO-NA-CONVERSA do humano"}}`,
		`{"type":"assistant","cwd":"/w/tree","gitBranch":"feat/x","sessionId":"s1","message":{"content":[{"type":"text","text":"SEGREDO-NA-CONVERSA do agente"}]}}`,
		`{"type":"pr-link","prUrl":"https://example.test/pull/7","prNumber":7,"sessionId":"s1"}`,
		`{"type":"custom-title","customTitle":"F-0001 dev","sessionId":"s1"}`,
		`{"type":"last-prompt","lastPrompt":"SEGREDO-NA-CONVERSA no prompt","sessionId":"s1"}`,
	)
	last := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	if err := os.Chtimes(p, last, last); err != nil {
		t.Fatal(err)
	}
	sub := writeLines(t, root, "-tmp-p/s1/subagents/agent-a1.jsonl", `{"type":"user","message":{"content":"SEGREDO-NA-CONVERSA do subagente"}}`)
	if err := os.WriteFile(strings.TrimSuffix(sub, ".jsonl")+".meta.json", []byte(`{"agentType":"dev"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(sub, last.Add(-time.Hour), last.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}

	infos, err := SessionInfos([]string{root}, []string{"s1", "gone"})
	if err != nil {
		t.Fatal(err)
	}
	got := infos["s1"]
	want := SessionInfo{Title: "F-0001 dev", Cwd: "/w/tree", Branch: "feat/x", PR: "https://example.test/pull/7", Last: last, Found: true, Agents: map[string]string{"a1": "dev"}}
	if got.Title != want.Title || got.Cwd != want.Cwd || got.Branch != want.Branch || got.PR != want.PR || !got.Last.Equal(want.Last) || !got.Found || got.Agents["a1"] != "dev" {
		t.Fatalf("s1 = %+v, want %+v", got, want)
	}
	raw, _ := json.Marshal(infos)
	if strings.Contains(string(raw), "SEGREDO") {
		t.Fatalf("conversation leaked into the session info: %s", raw)
	}
	if g := infos["gone"]; g.Found || g.Title != "" {
		t.Fatalf("missing transcript = %+v, want Found=false and nothing else", g)
	}
}

// TestSessionInfosWidensPastTheTail: the location is read from the end of the
// transcript (they run to 150+ MB); when the tail holds no folder, the window
// widens until it finds one or reads the whole file.
func TestSessionInfosWidensPastTheTail(t *testing.T) {
	root := t.TempDir()
	lines := []string{`{"type":"custom-title","customTitle":"old session"}`, `{"type":"user","cwd":"/early","gitBranch":"b1","message":{"content":"x"}}`}
	filler := `{"type":"queue-operation","operation":"enqueue","pad":"` + strings.Repeat("z", 1000) + `"}`
	for range 3000 { // ~3 MB of lines with no location
		lines = append(lines, filler)
	}
	writeLines(t, root, "-tmp-p/s2.jsonl", lines...)
	infos, err := SessionInfos([]string{root}, []string{"s2"})
	if err != nil {
		t.Fatal(err)
	}
	if g := infos["s2"]; g.Cwd != "/early" || g.Branch != "b1" || g.Title != "old session" {
		t.Fatalf("s2 = %+v, want the early location found by widening", g)
	}
}

// TestSessionInfosNoRoots: no transcript root at all is no session found, not
// an error.
func TestSessionInfosNoRoots(t *testing.T) {
	infos, err := SessionInfos([]string{filepath.Join(t.TempDir(), "none")}, []string{"s"})
	if err != nil || infos["s"].Found {
		t.Fatalf("infos = %+v, err = %v", infos, err)
	}
}
