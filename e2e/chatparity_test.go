package e2e_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/project"
	"gitlab.com/rockt/cerne/internal/server"
	"gitlab.com/rockt/cerne/internal/spectmpl"
	"gitlab.com/rockt/cerne/internal/store"
)

// paritySteps is the fixed non-lifecycle script of F-0323 crit. 9, run in
// both modes.
type parityStep struct {
	Verb  string   `json:"verb"`
	Args  []string `json:"args"`
	Stdin string   `json:"stdin,omitempty"`
}

func paritySteps() []parityStep {
	var spec strings.Builder
	for _, s := range spectmpl.Sections {
		fmt.Fprintf(&spec, "## %s\n\nConteúdo real da seção %s.\n\n", s, s)
	}
	return []parityStep{
		{"reference new", []string{"Ref paridade", "--url", "https://example.com/p", "--facet", "code", "--component", "chat"}, "conteúdo"},
		{"note new", []string{"Nota paridade", "--facet", "process", "--component", "chat"}, "corpo"},
		{"feature new", []string{"Feature paridade", "--context", "ctx", "--facet", "code", "--component", "chat", "--module", "core"}, ""},
		{"feature spec", []string{"F-0001"}, spec.String()},
		{"feature retag", []string{"F-0001", "--facet", "process", "--component", "chat"}, ""},
		{"classify", []string{"F-0001", "--module", "ui"}, ""},
	}
}

// fakeClaude emulates only Claude Code's execution channel under the chat's
// grant: when --allowedTools carries Bash(cerne:*) it runs each scripted step
// with the real cerne (this binary, CERNE_TEST_RUN_CLI) in its cwd — the
// project dir the cliProvider chose — and prints each stdout, the way the
// subscription agent reads command output.
func fakeClaude(scriptPath string) int {
	_, _ = io.Copy(io.Discard, os.Stdin) // the prompt
	allowed := ""
	for i, a := range os.Args {
		if a == "--allowedTools" && i+1 < len(os.Args) {
			allowed = os.Args[i+1]
		}
	}
	if !strings.Contains(allowed, "Bash(cerne:*)") {
		fmt.Println("sem permissão de escrita neste turno")
		return 0
	}
	raw, err := os.ReadFile(scriptPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	var steps []parityStep
	if err := json.Unmarshal(raw, &steps); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	bin, _ := os.Executable()
	for i, s := range steps {
		cmd := exec.Command(bin, append(strings.Fields(s.Verb), s.Args...)...)
		cmd.Stdin = strings.NewReader(s.Stdin)
		var out, errOut bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errOut
		runErr := cmd.Run()
		fmt.Printf("### passo %d\n%s", i+1, out.String())
		if runErr != nil {
			fmt.Printf("ERRO: %v %s\n", runErr, errOut.String())
		}
	}
	return 0
}

type parityProject struct {
	proj, repo, pid string
}

func newParityProject(t *testing.T, name string) parityProject {
	t.Helper()
	origin := filepath.Join(t.TempDir(), "origin.git")
	if err := gitback.InitBare(origin); err != nil {
		t.Fatal(err)
	}
	proj := t.TempDir()
	mustCerne(t, "init", "--name", name, "--backend", origin, "--module", "core", "--module", "ui", "--dir", proj)
	cfg, _, err := project.LoadConfig(proj)
	if err != nil {
		t.Fatal(err)
	}
	cache, err := project.CacheDir(cfg.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	return parityProject{proj: proj, repo: filepath.Join(cache, "repo"), pid: cfg.ProjectID}
}

func (p parityProject) setLLM(t *testing.T, llm project.LLMConfig) {
	t.Helper()
	cfg, _, err := project.LoadConfig(p.proj)
	if err != nil {
		t.Fatal(err)
	}
	cfg.LLM = llm
	if err := project.SaveConfig(p.proj, cfg); err != nil {
		t.Fatal(err)
	}
}

type parityTerminal struct {
	Answered bool   `json:"answered"`
	Reply    string `json:"reply"`
	Runs     []struct {
		Verb   string `json:"verb"`
		Result struct {
			OK    bool   `json:"ok"`
			ID    string `json:"id"`
			Error *struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		} `json:"result"`
	} `json:"runs"`
}

// confirmedChat posts the confirmed turn whose card lists the script.
func confirmedChat(t *testing.T, p parityProject, steps []parityStep) parityTerminal {
	t.Helper()
	var card strings.Builder
	card.WriteString("Vou rodar:\n```cerne-comando\n")
	for _, s := range steps {
		line := append(strings.Fields(s.Verb), s.Args...)
		for i, a := range line {
			line[i] = shQuote(a)
		}
		fmt.Fprintf(&card, "cerne %s\n", strings.Join(line, " "))
	}
	card.WriteString("```")
	body, _ := json.Marshal(map[string]any{
		"messages": []map[string]string{
			{"role": "user", "content": "registra o roteiro de paridade"},
			{"role": "assistant", "content": card.String()},
			{"role": "user", "content": "Confirmado: execute."},
		},
		"conversation_id": "c-paridade",
		"confirm_turn":    1,
		"confirm":         true,
	})
	rec := httptest.NewRecorder()
	server.New(p.repo, p.proj, p.pid).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/chat", bytes.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/chat = %d: %s", rec.Code, rec.Body.String())
	}
	var term parityTerminal
	lines := strings.Split(strings.TrimSpace(rec.Body.String()), "\n")
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &term); err != nil {
		t.Fatalf("terminal %q: %v", lines[len(lines)-1], err)
	}
	return term
}

// shQuote quotes a word for the card the way the agent would: the server binds
// the confirmation to the card's commands, read like a shell (C3).
func shQuote(w string) string {
	if w != "" && !strings.ContainsAny(w, " \t'\"\\$`#;&|<>()*?[]{}~!") {
		return w
	}
	return "'" + strings.ReplaceAll(w, "'", `'\''`) + "'"
}

// entitySet is what both modes must produce: every reference, note and
// feature by id, title, state, tags and module.
func entitySet(t *testing.T, repo string) []string {
	t.Helper()
	refs, err1 := store.LoadAllReferences(repo)
	notes, err2 := store.LoadAllNotes(repo)
	feats, err3 := store.LoadAllFeatures(repo)
	if err1 != nil || err2 != nil || err3 != nil {
		t.Fatalf("load: %v %v %v", err1, err2, err3)
	}
	var out []string
	for _, r := range refs {
		out = append(out, fmt.Sprintf("%s|%s|%s|%v|%v", r.ID, r.Title, r.State, r.Facets, r.Components))
	}
	for _, n := range notes {
		out = append(out, fmt.Sprintf("%s|%s|%s|%v|%v", n.ID, n.Title, n.State, n.Facets, n.Components))
	}
	for _, f := range feats {
		out = append(out, fmt.Sprintf("%s|%s|%s|%v|%v|%s", f.ID, f.Title, f.State, f.Facets, f.Components, f.Module))
	}
	slices.Sort(out)
	return out
}

var parityID = regexp.MustCompile(`\b[A-Z]{1,2}-\d{4}\b`)

// TestChatParitySubscriptionOracle is F-0323 criterion 9: the fixed script run
// through the subscription mode (claude-cli, the oracle) and through the API
// token mode's tool channel produces the same entities and reports the same id
// per step.
func TestChatParitySubscriptionOracle(t *testing.T) {
	t.Setenv("CERNE_CACHE_DIR", t.TempDir())
	t.Setenv("CERNE_TEST_RUN_CLI", "1")
	steps := paritySteps()
	scriptPath := filepath.Join(t.TempDir(), "roteiro.json")
	raw, _ := json.Marshal(steps)
	if err := os.WriteFile(scriptPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CERNE_TEST_FAKE_CLAUDE", scriptPath)
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	// A — the oracle: claude-cli runs the script through its Bash grant.
	a := newParityProject(t, "Paridade A")
	a.setLLM(t, project.LLMConfig{Mode: "claude-cli", ClaudeBin: bin})
	termA := confirmedChat(t, a, steps)
	known := map[string]bool{}
	evsA, err := store.ReadEvents(a.repo)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range evsA {
		known[ev.EntityID] = true
	}
	blocks := strings.Split(termA.Reply, "### passo ")[1:]
	if len(blocks) != len(steps) || strings.Contains(termA.Reply, "ERRO:") {
		t.Fatalf("oracle reply does not cover the script:\n%s", termA.Reply)
	}
	oracle := make([]string, len(steps))
	for i, b := range blocks {
		for _, id := range parityID.FindAllString(b, -1) {
			if known[id] {
				oracle[i] = id
				break
			}
		}
	}

	// B — the API token mode: a stub model calls the generic tool once per step.
	var bodies int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		round := bodies
		bodies++
		w.Header().Set("Content-Type", "application/json")
		if round >= len(steps) {
			_, _ = io.WriteString(w, `{"stop_reason":"end_turn","content":[{"type":"text","text":"Roteiro executado."}]}`)
			return
		}
		input, _ := json.Marshal(steps[round])
		fmt.Fprintf(w, `{"stop_reason":"tool_use","content":[{"type":"tool_use","id":"tu_%d","name":"cerne","input":%s}]}`, round, input)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("TEST_PARITY_KEY", "sk-test")
	b := newParityProject(t, "Paridade B")
	b.setLLM(t, project.LLMConfig{Mode: "byo-key", BaseURL: srv.URL, KeyEnv: "TEST_PARITY_KEY"})
	termB := confirmedChat(t, b, steps)

	// (a) same entities
	if ea, eb := entitySet(t, a.repo), entitySet(t, b.repo); !slices.Equal(ea, eb) {
		t.Fatalf("entities differ:\noracle %v\napi    %v", ea, eb)
	}
	// (b) same id per step, (c) every step ok
	if len(termB.Runs) != len(steps) {
		t.Fatalf("api mode ran %d steps, want %d: %+v", len(termB.Runs), len(steps), termB.Runs)
	}
	for i, run := range termB.Runs {
		if !run.Result.OK {
			t.Fatalf("step %d (%s) failed: %+v", i+1, run.Verb, run.Result.Error)
		}
		if run.Result.ID != oracle[i] || oracle[i] == "" {
			t.Fatalf("step %d (%s): api id %q, oracle id %q", i+1, run.Verb, run.Result.ID, oracle[i])
		}
	}
	// (d) creation events carry the chat provenance
	evsB, err := store.ReadEvents(b.repo)
	if err != nil {
		t.Fatal(err)
	}
	created := 0
	for _, ev := range evsB {
		if !strings.HasSuffix(ev.Type, ".created") {
			continue
		}
		created++
		if p := ev.Payload; p["origin"] != "chat" || p["conversation"] != "c-paridade" || p["turn"] != "1" {
			t.Fatalf("%s %s payload = %v, want origin=chat conversation=c-paridade turn=1", ev.Type, ev.EntityID, p)
		}
	}
	if created != 3 {
		t.Fatalf("api mode recorded %d creation events, want 3", created)
	}
}
