package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
)

// linksFake is the LLM the links tests talk to: an Anthropic-shaped HTTP
// server that counts calls, keeps every prompt and answers by source document.
type linksFake struct {
	calls   atomic.Int32
	prompts []string
	answers map[string]string // source id → the JSON array it answers
	usage   string            // the "usage" member of the response, "" for none
	onCall  func(n int32)     // runs when call n arrives, before it answers
}

func (f *linksFake) start(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &req)
		n := f.calls.Add(1)
		if f.onCall != nil {
			f.onCall(n)
		}
		prompt := ""
		if len(req.Messages) > 0 {
			prompt = req.Messages[0].Content
		}
		f.prompts = append(f.prompts, prompt)
		answer := "[]"
		for id, a := range f.answers {
			if strings.Contains(prompt, "Source document: "+id+" ") {
				answer = a
			}
		}
		body, _ := json.Marshal(answer)
		usage := ""
		if f.usage != "" {
			usage = `,"model":"fake-1","usage":` + f.usage
		}
		_, _ = io.WriteString(w, `{"content":[{"type":"text","text":`+string(body)+`}]`+usage+`}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// linksProject builds a project with F-0001 (cites the cache layer in prose)
// and F-0002 (the cache layer), the fake as its byo-key LLM, and
// links.suggest set as asked. Setting it is test code: an agent never does.
func linksProject(t *testing.T, on bool, fake *linksFake) (projDir, repoDir string) {
	t.Helper()
	projDir, _, repoDir = setupProject(t)
	t.Setenv("CERNE_ACTOR", "human")
	newFeature(t, projDir, "Billing", 1)
	newFeature(t, projDir, "Cache layer", 2)
	writeDocRaw(t, repoDir, "F-0001", "spec.md", "# Billing\n\nInvoices are cached,\nusing the cache layer of the platform.\n")
	writeDocRaw(t, repoDir, "F-0002", "spec.md", "# Cache layer\n\nAn in-memory cache.\n")
	commitBackend(t, repoDir)
	t.Setenv("TEST_BYO_KEY", "sk-byo")
	enableByoKey(t, projDir, fake.start(t).URL, "TEST_BYO_KEY")
	if on {
		if err := project.SetSetting(projDir, "links.suggest", project.ScopeProject, "true"); err != nil {
			t.Fatal(err)
		}
	}
	return projDir, repoDir
}

const linksAnswer = `[{"from":"F-0001","to":"F-0002","excerpt":"a sentence the document never had"},` +
	`{"from":"F-0001","to":"F-0002","excerpt":"using the cache layer\nof the platform"},` +
	`{"from":"F-0001","to":"F-9999","excerpt":"Invoices are cached"}]`

// TestLinksSuggestRefusedWhenOff: with links.suggest off the command refuses
// with how to turn it on, builds no prompt and never calls the LLM.
func TestLinksSuggestRefusedWhenOff(t *testing.T) { // F-0502 crit. 2
	fake := &linksFake{}
	projDir, repoDir := linksProject(t, false, fake)
	out, err := execCerne(t, "links", "suggest", "--yes", "--dir", projDir)
	if !errors.Is(err, errLinksSuggestOff) || !strings.Contains(err.Error(), "cerne settings set links.suggest true") {
		t.Fatalf("err = %v, want errLinksSuggestOff naming the command that turns it on\n%s", err, out)
	}
	if n := fake.calls.Load(); n != 0 {
		t.Errorf("the LLM got %d call(s) with the switch off", n)
	}
	if q, _ := store.LoadLinkSuggestions(repoDir); len(q) != 0 {
		t.Errorf("queue = %+v, want empty", q)
	}
}

// TestLinksSuggestShowsEstimateAndAsks: before any call the command shows how
// many documents it reads and a token estimate; without --yes and a terminal
// answer it stops, and only a "y" goes on.
func TestLinksSuggestShowsEstimateAndAsks(t *testing.T) { // F-0502 crit. 3
	fake := &linksFake{answers: map[string]string{"F-0001": linksAnswer}}
	projDir, _ := linksProject(t, true, fake)

	out, err := execCerne(t, "links", "suggest", "--dir", projDir) // stdin is no terminal
	if err != nil {
		t.Fatalf("suggest: %v\n%s", err, out)
	}
	if !strings.Contains(out, "2 document(s), ~") || !strings.Contains(out, "tokens (estimate") || !strings.Contains(out, "--yes") {
		t.Errorf("output lacks the documents, the estimate or the --yes hint:\n%s", out)
	}
	if n := fake.calls.Load(); n != 0 {
		t.Fatalf("the LLM got %d call(s) with no confirmation", n)
	}

	orig := linksInteractive
	t.Cleanup(func() { linksInteractive = orig })
	linksInteractive = func(io.Reader) bool { return true }
	for _, answer := range []string{"\n", "n\n"} {
		root := Root()
		buf := &strings.Builder{}
		root.SetOut(buf)
		root.SetErr(buf)
		root.SetIn(strings.NewReader(answer))
		root.SetArgs([]string{"links", "suggest", "--dir", projDir})
		if err := root.Execute(); err != nil || !strings.Contains(buf.String(), "Proceed? [y/N]") {
			t.Fatalf("answer %q: %v\n%s", answer, err, buf.String())
		}
		if n := fake.calls.Load(); n != 0 {
			t.Fatalf("answer %q reached the LLM (%d call(s))", answer, n)
		}
	}
	root := Root()
	buf := &strings.Builder{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetIn(strings.NewReader("y\n"))
	root.SetArgs([]string{"links", "suggest", "--dir", projDir})
	if err := root.Execute(); err != nil {
		t.Fatalf("y: %v\n%s", err, buf.String())
	}
	if n := fake.calls.Load(); n != 2 {
		t.Errorf("after y: %d call(s), want one per document (2)", n)
	}
}

// TestLinksSuggestQueuesOnlyWhatPassesTheFilter: --yes asks once per document,
// queues the suggestion whose excerpt is in the document and whose target
// exists, drops the other two by reason, and reports and logs the tokens.
func TestLinksSuggestQueuesOnlyWhatPassesTheFilter(t *testing.T) { // F-0502 crit. 4, 10
	fake := &linksFake{answers: map[string]string{"F-0001": linksAnswer}, usage: `{"input_tokens":120,"output_tokens":30}`}
	projDir, repoDir := linksProject(t, true, fake)
	out, err := execCerne(t, "links", "suggest", "--yes", "--dir", projDir)
	if err != nil {
		t.Fatalf("suggest: %v\n%s", err, out)
	}
	if n := fake.calls.Load(); n != 2 {
		t.Errorf("calls = %d, want 2 (one per document)", n)
	}
	for _, want := range []string{"Queued 1 suggestion(s)", "excerpt not in the source: 1", "unknown target: 1", "240 in, 60 out"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	q, _ := store.LoadLinkSuggestions(repoDir)
	if len(q) != 1 || q[0].From != "F-0001" || q[0].To != "F-0002" || q[0].Status != store.LinkOpen || !strings.Contains(q[0].Excerpt, "cache layer") {
		t.Fatalf("queue = %+v", q)
	}
	evs, _ := store.ReadEvents(repoDir)
	found := false
	for _, e := range evs {
		if e.Type == "links.suggested" {
			found = true
			if e.Payload["input_tokens"] != "240" || e.Payload["output_tokens"] != "60" || e.Payload["queued"] != "1" || e.Payload["received"] != "3" {
				t.Errorf("links.suggested payload = %v", e.Payload)
			}
		}
	}
	if !found {
		t.Error("no links.suggested event")
	}

	// the same pair is never suggested twice
	fake.calls.Store(0)
	out, err = execCerne(t, "links", "suggest", "--yes", "--dir", projDir)
	if err != nil || !strings.Contains(out, "Queued 0 suggestion(s)") || !strings.Contains(out, "pair already suggested") {
		t.Errorf("second round: %v\n%s", err, out)
	}
}

// TestLinksSuggestNotesMissingUsage: a provider that reports no usage is said
// to, not shown as zero.
func TestLinksSuggestNotesMissingUsage(t *testing.T) { // F-0502 crit. 10
	fake := &linksFake{answers: map[string]string{"F-0001": "[]"}}
	projDir, _ := linksProject(t, true, fake)
	out, err := execCerne(t, "links", "suggest", "--yes", "--dir", projDir)
	if err != nil || !strings.Contains(out, "usage not reported by byo-key") {
		t.Errorf("%v\n%s", err, out)
	}
}

// TestLinksSuggestToleratesAnUnreadableAnswer: a document whose answer is no
// JSON counts as no suggestions, with a warning, and the round goes on.
func TestLinksSuggestToleratesAnUnreadableAnswer(t *testing.T) { // F-0502 crit. 4
	fake := &linksFake{answers: map[string]string{"F-0001": "sorry, I cannot", "F-0002": "[]"}}
	projDir, _ := linksProject(t, true, fake)
	out, err := execCerne(t, "links", "suggest", "--yes", "--dir", projDir)
	if err != nil || !strings.Contains(out, "F-0001: unreadable answer") || !strings.Contains(out, "Queued 0 suggestion(s)") {
		t.Errorf("%v\n%s", err, out)
	}
}

// TestLinksSuggestDelegation: in delegation mode --yes writes one prompt file
// for the host agent, calls nothing, and --ingest filters the agent's JSON.
func TestLinksSuggestDelegation(t *testing.T) { // F-0502 crit. 10
	fake := &linksFake{}
	projDir, repoDir := linksProject(t, true, fake)
	cfg, dir, _ := project.LoadConfig(projDir)
	cfg.LLM = project.LLMConfig{Mode: "delegation"}
	if err := project.SaveConfig(dir, cfg); err != nil {
		t.Fatal(err)
	}
	if err := project.SetSetting(projDir, "links.suggest", project.ScopeProject, "true"); err != nil { // SaveConfig dropped it
		t.Fatal(err)
	}
	out, err := execCerne(t, "links", "suggest", "--yes", "--dir", projDir)
	if err != nil || !strings.Contains(out, ".docs/links/.pending/prompt.md") || !strings.Contains(out, "--ingest") {
		t.Fatalf("%v\n%s", err, out)
	}
	prompt, err := os.ReadFile(filepath.Join(projDir, ".docs", "links", ".pending", "prompt.md"))
	if err != nil || !strings.Contains(string(prompt), app.LinkPromptMarker) || !strings.Contains(string(prompt), "using the cache layer") {
		t.Fatalf("prompt file: %v\n%s", err, prompt)
	}
	if n := fake.calls.Load(); n != 0 {
		t.Errorf("delegation called the LLM %d time(s)", n)
	}
	if err := os.WriteFile(filepath.Join(projDir, ".docs", "links", ".pending", "suggestions.json"), []byte(linksAnswer), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err = execCerne(t, "links", "suggest", "--ingest", "--dir", projDir)
	if err != nil || !strings.Contains(out, "Queued 1 suggestion(s)") {
		t.Fatalf("ingest: %v\n%s", err, out)
	}
	if q, _ := store.LoadLinkSuggestions(repoDir); len(q) != 1 {
		t.Errorf("queue = %+v", q)
	}
}

// queueOne runs a round that queues the one valid suggestion (#1).
func queueOne(t *testing.T, projDir string) {
	t.Helper()
	if out, err := execCerne(t, "links", "suggest", "--yes", "--dir", projDir); err != nil {
		t.Fatalf("suggest: %v\n%s", err, out)
	}
}

// TestLinksPendingAcceptReject: pending lists the open ones with their
// excerpt; accept turns #1 into a links edge; reject takes it out again, also
// when it was accepted, and the pair is not suggested again.
func TestLinksPendingAcceptReject(t *testing.T) { // F-0502 crit. 5, 6, 7
	fake := &linksFake{answers: map[string]string{"F-0001": linksAnswer}}
	projDir, repoDir := linksProject(t, true, fake)
	if out, _ := execCerne(t, "links", "pending", "--dir", projDir); !strings.Contains(out, "No open link suggestions") {
		t.Errorf("empty pending:\n%s", out)
	}
	queueOne(t, projDir)
	out, err := execCerne(t, "links", "pending", "--dir", projDir)
	if err != nil || !strings.Contains(out, "#1  F-0001 → F-0002") || !strings.Contains(out, `"using the cache layer`) {
		t.Fatalf("pending: %v\n%s", err, out)
	}
	hasEdge := func() bool {
		g, err := app.BuildGraph(repoDir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range g.Edges {
			if e.From == "F-0001" && e.To == "F-0002" && e.Type == app.EdgeLinks {
				return true
			}
		}
		return false
	}
	if hasEdge() {
		t.Fatal("an open suggestion is already an edge")
	}
	if out, err := execCerne(t, "links", "accept", "1", "--dir", projDir); err != nil || !strings.Contains(out, "#1 accepted") {
		t.Fatalf("accept: %v\n%s", err, out)
	}
	if !hasEdge() {
		t.Error("accepted relation is not a links edge")
	}
	if out, _ := execCerne(t, "links", "pending", "--dir", projDir); !strings.Contains(out, "No open link suggestions") {
		t.Errorf("pending after accept:\n%s", out)
	}
	if _, err := execCerne(t, "links", "accept", "1", "--dir", projDir); !errors.Is(err, app.ErrLinkDecided) {
		t.Errorf("accept twice: %v", err)
	}
	if _, err := execCerne(t, "links", "accept", "7", "--dir", projDir); !errors.Is(err, app.ErrLinkUnknown) {
		t.Errorf("accept unknown: %v", err)
	}
	if out, err := execCerne(t, "links", "reject", "1", "--dir", projDir); err != nil || !strings.Contains(out, "#1 rejected") {
		t.Fatalf("reject: %v\n%s", err, out)
	}
	if hasEdge() {
		t.Error("a rejected relation is still an edge")
	}
	out, _ = execCerne(t, "links", "suggest", "--yes", "--dir", projDir)
	if !strings.Contains(out, "Queued 0 suggestion(s)") {
		t.Errorf("a rejected pair came back:\n%s", out)
	}
	if _, err := execCerne(t, "links", "accept", "x", "--dir", projDir); err == nil {
		t.Error("accept x: no error")
	}
}

// TestLinksDecisionsAreHuman: accepting or rejecting a suggestion is a human
// act; an agent actor is refused and the queue stays as it was.
func TestLinksDecisionsAreHuman(t *testing.T) { // F-0502 crit. 6
	fake := &linksFake{answers: map[string]string{"F-0001": linksAnswer}}
	projDir, repoDir := linksProject(t, true, fake)
	queueOne(t, projDir)
	t.Setenv("CERNE_ACTOR", "agent")
	for _, verb := range []string{"accept", "reject"} {
		if _, err := execCerne(t, "links", verb, "1", "--dir", projDir); !errors.Is(err, app.ErrHumanGate) {
			t.Errorf("%s as an agent: %v, want ErrHumanGate", verb, err)
		}
	}
	if q, _ := store.LoadLinkSuggestions(repoDir); q[0].Status != store.LinkOpen {
		t.Errorf("status = %s, want open", q[0].Status)
	}
}

// TestLinksSuggestKeepsWhatWasPaidForWhenCanceled: a cancel in the middle of
// the round (Ctrl-C) still queues and logs what the first documents already
// returned, then reports the cancellation.
func TestLinksSuggestKeepsWhatWasPaidForWhenCanceled(t *testing.T) { // F-0502 crit. 4
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fake := &linksFake{answers: map[string]string{"F-0001": linksAnswer}, usage: `{"input_tokens":120,"output_tokens":30}`}
	fake.onCall = func(n int32) {
		if n == 2 { // the second document: the user hits Ctrl-C while it is in flight
			cancel()
			time.Sleep(300 * time.Millisecond)
		}
	}
	projDir, repoDir := linksProject(t, true, fake)
	root := Root()
	out := &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs([]string{"links", "suggest", "--yes", "--dir", projDir})
	err := root.ExecuteContext(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled\n%s", err, out)
	}
	if q, _ := store.LoadLinkSuggestions(repoDir); len(q) != 1 {
		t.Fatalf("queue = %+v, want the one suggestion of the first document", q)
	}
	evs, _ := store.ReadEvents(repoDir)
	for _, e := range evs {
		if e.Type == "links.suggested" && e.Payload["documents"] == "1" && e.Payload["input_tokens"] == "120" {
			return
		}
	}
	t.Errorf("no links.suggested event with the one document and its tokens: %+v", evs)
}

// TestLinksOnlySuggestAsksForSuggestions: with the switch on and an LLM
// configured, push, pull, lint, compile and ask never send a link-suggestion
// prompt; links suggest does (so the marker the test looks for is real).
func TestLinksOnlySuggestAsksForSuggestions(t *testing.T) { // F-0502 crit. 9
	fake := &linksFake{}
	projDir, _ := linksProject(t, true, fake)
	for _, args := range [][]string{
		{"push"}, {"pull"}, {"lint"}, {"compile"}, {"ask", "what is the cache layer?"}, {"sync"},
	} {
		_, _ = execCerne(t, append(args, "--dir", projDir)...) // their own outcome is not under test
	}
	for _, p := range fake.prompts {
		if strings.Contains(p, app.LinkPromptMarker) {
			t.Fatalf("a command other than links suggest asked for link suggestions:\n%.300s", p)
		}
	}
	if _, err := execCerne(t, "links", "suggest", "--yes", "--dir", projDir); err != nil {
		t.Fatal(err)
	}
	if n := len(fake.prompts); n == 0 || !strings.Contains(fake.prompts[n-1], app.LinkPromptMarker) {
		t.Errorf("links suggest sent no marked prompt: %d prompt(s)", n)
	}
}

// TestLinksSuggestEstimateIsHonest: a CLI agent adds its own context to every
// call, so its estimate line says it is not counted; the event records the
// estimate, and the next round shows how far the last one was off.
func TestLinksSuggestEstimateIsHonest(t *testing.T) { // F-0502 crit. 3, 10
	fake := &linksFake{answers: map[string]string{"F-0001": linksAnswer}, usage: `{"input_tokens":120,"output_tokens":30}`}
	projDir, repoDir := linksProject(t, true, fake)

	out, err := execCerne(t, "links", "suggest", "--yes", "--dir", projDir)
	if err != nil {
		t.Fatalf("suggest: %v\n%s", err, out)
	}
	if strings.Contains(out, "adds its own context") || strings.Contains(out, "Last round") {
		t.Errorf("an HTTP mode with no history must print neither note:\n%s", out)
	}
	evs, _ := store.ReadEvents(repoDir)
	est := ""
	for _, e := range evs {
		if e.Type == "links.suggested" {
			est = e.Payload["estimate"]
		}
	}
	if est == "" || est == "0" {
		t.Fatalf("links.suggested has no estimate: %v", evs)
	}

	out, err = execCerne(t, "links", "suggest", "--dir", projDir)
	if err != nil || !strings.Contains(out, "Last round used ") || !strings.Contains(out, "240 in vs "+est+" estimated") {
		t.Errorf("no last-round ratio (est %s): %v\n%s", est, err, out)
	}

	cfg, dir, _ := project.LoadConfig(projDir)
	cfg.LLM = project.LLMConfig{Mode: "claude-cli"}
	if err := project.SaveConfig(dir, cfg); err != nil {
		t.Fatal(err)
	}
	if err := project.SetSetting(projDir, "links.suggest", project.ScopeProject, "true"); err != nil {
		t.Fatal(err)
	}
	out, err = execCerne(t, "links", "suggest", "--dir", projDir) // not confirmed: nothing is sent
	if err != nil || !strings.Contains(out, "claude-cli adds its own context to each call, not counted") {
		t.Errorf("no CLI-agent note: %v\n%s", err, out)
	}
}
