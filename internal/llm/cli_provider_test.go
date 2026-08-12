package llm

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"reflect"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/project"
)

// TestCLIProviderCompleteSuccess drives cliProvider with a fake runner: it
// returns the runner's stdout (trimmed), the runner receives the exact argv the
// Claude Code headless invocation needs — "-p --output-format text" with no
// --bare and no --model — and the PROMPT travels on stdin, not argv.
func TestCLIProviderCompleteSuccess(t *testing.T) {
	var gotName, gotStdin string
	var gotArgs []string
	fake := func(_ context.Context, _, name string, args []string, stdin string) (string, string, error) {
		gotName = name
		gotArgs = args
		gotStdin = stdin
		return "  resposta da assinatura\n", "", nil
	}
	p := &cliProvider{bin: "claude", run: fake}

	out, err := p.Complete(context.Background(), "qual o problema?")
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if out != "resposta da assinatura" {
		t.Fatalf("Complete = %q, want trimmed stdout", out)
	}
	if gotName != "claude" {
		t.Fatalf("runner name = %q, want claude", gotName)
	}
	want := []string{"-p", "--output-format", "text"}
	if !reflect.DeepEqual(gotArgs, want) {
		t.Fatalf("runner args = %v, want %v", gotArgs, want)
	}
	if gotStdin != "qual o problema?" {
		t.Fatalf("runner stdin = %q, want the prompt", gotStdin)
	}
	for _, banned := range []string{"--bare", "--model"} {
		for _, a := range gotArgs {
			if a == banned {
				t.Fatalf("runner args %v must not contain %q (breaks subscription auth / forces a model)", gotArgs, banned)
			}
		}
	}
}

// TestCLIProviderLargePromptGoesToStdin is the regression for the Windows argv
// limit (~32 KB): a real compile/ask prompt exceeds it, so the prompt must never
// be passed as an argument (which failed with "The filename or extension is too
// long"). It must travel on stdin, and argv must stay small and fixed.
func TestCLIProviderLargePromptGoesToStdin(t *testing.T) {
	prompt := strings.Repeat("contexto grande ", 4000) // ~64 KB, well over the argv cap
	var gotArgs []string
	var gotStdin string
	fake := func(_ context.Context, _, _ string, args []string, stdin string) (string, string, error) {
		gotArgs = args
		gotStdin = stdin
		return "ok", "", nil
	}
	p := &cliProvider{bin: "claude", run: fake}

	if _, err := p.Complete(context.Background(), prompt); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if gotStdin != prompt {
		t.Fatalf("prompt did not reach stdin (got %d bytes, want %d)", len(gotStdin), len(prompt))
	}
	for _, a := range gotArgs {
		if strings.Contains(a, "contexto grande") {
			t.Fatalf("prompt leaked into argv (arg of %d bytes) — exceeds the OS command-line limit", len(a))
		}
	}
}

// TestCLIProviderCompleteError wraps a runner failure as ErrClaudeCLI (errors.Is)
// with an actionable pt-BR message that mentions claude -p and the byo-key fallback.
func TestCLIProviderCompleteError(t *testing.T) {
	cause := errors.New("exit status 1: not logged in")
	fake := func(_ context.Context, _, _ string, _ []string, _ string) (string, string, error) {
		return "", "", cause
	}
	p := &cliProvider{bin: "claude", run: fake}

	_, err := p.Complete(context.Background(), "p")
	if !errors.Is(err, ErrClaudeCLI) {
		t.Fatalf("err = %v, want errors.Is ErrClaudeCLI", err)
	}
	if !errors.Is(err, cause) {
		t.Fatalf("err = %v, want it to wrap the runner cause", err)
	}
	for _, want := range []string{"claude -p", "byo-key"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not mention %q", err, want)
		}
	}
}

// TestDescribeExit pins F-0183: the exit description must carry the CLI's own
// explanation whichever stream it came out of. The Claude Code CLI reports
// auth failures on STDOUT with an empty stderr — before this, the error read
// `exited with exit status 1:` with nothing after the colon, hiding the one
// line that named the cause (a 401) behind the generic login hint.
func TestDescribeExit(t *testing.T) {
	tests := []struct {
		name    string
		stdout  string
		stderr  string
		want    []string
		wantNot []string
	}{
		{
			name:   "stderr wins when present",
			stdout: "partial answer",
			stderr: "not logged in",
			want:   []string{"not logged in"},
			// stdout must not pollute the normal case.
			wantNot: []string{"partial answer", "(stdout)"},
		},
		{
			name:   "stdout labeled when stderr is empty",
			stdout: "Failed to authenticate. API Error: 401 OAuth access token has expired.",
			want:   []string{"(stdout)", "401 OAuth access token has expired"},
		},
		{
			name:   "whitespace-only stderr counts as empty",
			stdout: "the real reason",
			stderr: "\n",
			want:   []string{"(stdout)", "the real reason"},
		},
		{
			name: "no output at all is said explicitly",
			want: []string{"(no output)"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := describeExit("claude", "exit status 1", []byte(tt.stdout), []byte(tt.stderr))
			for _, w := range tt.want {
				if !strings.Contains(got, w) {
					t.Errorf("describeExit() = %q, want substring %q", got, w)
				}
			}
			for _, w := range tt.wantNot {
				if strings.Contains(got, w) {
					t.Errorf("describeExit() = %q, must not contain %q", got, w)
				}
			}
			if strings.HasSuffix(strings.TrimSpace(got), ":") {
				t.Errorf("describeExit() = %q ends in a dangling colon", got)
			}
		})
	}
}

// TestDescribeExitTruncatesAnyStream: the size bound applies to the stdout
// fallback too — a chatty CLI cannot flood the error.
func TestDescribeExitTruncatesAnyStream(t *testing.T) {
	big := strings.Repeat("x", 10*1024)
	got := describeExit("claude", "exit status 1", []byte(big), nil)
	if len(got) > maxOutputBytes+200 { // message scaffold on top of the bounded stream
		t.Fatalf("describeExit() length = %d, want bounded near %d", len(got), maxOutputBytes)
	}
}

// TestExecRunCapturesStdoutOnFailure is the wiring proof (F-0183 criterion 8):
// a REAL process that writes only to stdout and exits 1 must surface that
// text in execRun's error. The fake-runner tests never execute execRun — this
// seam is exactly where the original bug lived unseen.
func TestExecRunCapturesStdoutOnFailure(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		fmt.Println("SENTINEL: the actual reason came out on stdout")
		os.Exit(1)
	}
	// execRun offers no env injection (by design), so the guard travels via
	// inherited environment: set here, seen by the re-executed child.
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	_, _, err := execRun(context.Background(), "", os.Args[0],
		[]string{"-test.run=TestExecRunCapturesStdoutOnFailure$"}, "")
	if err == nil {
		t.Fatal("execRun on an exit-1 process returned nil error")
	}
	for _, want := range []string{"SENTINEL: the actual reason came out on stdout", "(stdout)"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("execRun error = %q, want substring %q", err, want)
		}
	}
}

// TestNewProviderClaudeCLINoKey builds the claude-cli provider WITHOUT any API
// key in the environment: it must not error (no ErrAPIKeyMissing) and must wire
// the configured claude_bin. KeyEnv is ignored entirely (subscription path).
func TestNewProviderClaudeCLINoKey(t *testing.T) {
	// Point KeyEnv at an unset var: claude-cli must not read it.
	t.Setenv("TEST_CLAUDE_KEY", "")
	prov, err := NewProvider(project.LLMConfig{Mode: "claude-cli", KeyEnv: "TEST_CLAUDE_KEY", ClaudeBin: "/opt/claude"})
	if err != nil {
		t.Fatalf("NewProvider(claude-cli): %v", err)
	}
	cp, ok := prov.(*cliProvider)
	if !ok {
		t.Fatalf("provider = %T, want *cliProvider", prov)
	}
	if cp.bin != "/opt/claude" {
		t.Fatalf("cliProvider.bin = %q, want /opt/claude", cp.bin)
	}
	if cp.run == nil {
		t.Fatalf("cliProvider.run is nil, want the default execRun")
	}
}

// TestNewProviderClaudeCLIDefaultBin applies WithDefaults so an unset claude_bin
// resolves to "claude".
func TestNewProviderClaudeCLIDefaultBin(t *testing.T) {
	prov, err := NewProvider(project.LLMConfig{Mode: "claude-cli"})
	if err != nil {
		t.Fatalf("NewProvider(claude-cli): %v", err)
	}
	cp, ok := prov.(*cliProvider)
	if !ok {
		t.Fatalf("provider = %T, want *cliProvider", prov)
	}
	if cp.bin != "claude" {
		t.Fatalf("cliProvider.bin = %q, want default \"claude\"", cp.bin)
	}
}

// TestAgentProviderAllowlistFlags pins the F-0224 permission boundary: the
// agentic provider must pass EXACTLY the cerne read-command allowlist via one
// comma-joined --allowedTools argument, run in the project dir, and keep the
// prompt on stdin. The exact-string assertion is the guard against a write
// command ever sneaking into the boundary.
func TestAgentProviderAllowlistFlags(t *testing.T) {
	var gotDir string
	var gotArgs []string
	var gotStdin string
	fake := func(_ context.Context, dir, _ string, args []string, stdin string) (string, string, error) {
		gotDir = dir
		gotArgs = args
		gotStdin = stdin
		return "resposta com F-0132 citada", "", nil
	}
	p := &cliProvider{bin: "claude", run: fake, dir: "/proj", agentTools: AgentReadTools, deniedTools: AgentDeniedPaths}

	out, err := p.Complete(context.Background(), "qual a spec da F-0132?")
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if out != "resposta com F-0132 citada" {
		t.Fatalf("Complete = %q", out)
	}
	if gotDir != "/proj" {
		t.Fatalf("runner dir = %q, want /proj (cerne commands must hit the project backend)", gotDir)
	}
	if gotStdin != "qual a spec da F-0132?" {
		t.Fatalf("stdin = %q, want the prompt", gotStdin)
	}
	wantList := "Read,Grep,Glob," +
		"Bash(cerne search:*),Bash(cerne show:*),Bash(cerne brief:*),Bash(cerne status:*)," +
		"Bash(cerne note list:*),Bash(cerne note show:*),Bash(cerne reference list:*),Bash(cerne decision list:*)"
	wantDenied := "Read(.env),Read(.env.*),Read(**/credentials*),Read(**/*.pem),Read(**/*.key)"
	want := []string{"-p", "--output-format", "text", "--allowedTools", wantList, "--disallowedTools", wantDenied}
	if !reflect.DeepEqual(gotArgs, want) {
		t.Fatalf("runner args = %v, want %v", gotArgs, want)
	}
}

// TestAgentReadToolsIncludesNativeReaders (F-0258 criterion 1): the agentic
// allowlist carries the CLI's native read tools so the chat agent can ground
// a proposal in real project files, not just cerne entities.
func TestAgentReadToolsIncludesNativeReaders(t *testing.T) {
	for _, tool := range []string{"Read", "Grep", "Glob"} {
		found := false
		for _, rule := range AgentReadTools {
			if rule == tool {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("AgentReadTools misses native reader %q: %v", tool, AgentReadTools)
		}
	}
}

// TestAgentReadToolsDeniesWrites (F-0258 criterion 3): no rule may authorize a
// mutating native tool, and every Bash(...) rule must stay inside the closed
// set of the 8 cerne read commands — the allowlist grows only by scoped
// native readers, never by shell surface.
func TestAgentReadToolsDeniesWrites(t *testing.T) {
	cerneBashRules := map[string]bool{
		"Bash(cerne search:*)":         true,
		"Bash(cerne show:*)":           true,
		"Bash(cerne brief:*)":          true,
		"Bash(cerne status:*)":         true,
		"Bash(cerne note list:*)":      true,
		"Bash(cerne note show:*)":      true,
		"Bash(cerne reference list:*)": true,
		"Bash(cerne decision list:*)":  true,
	}
	for _, rule := range AgentReadTools {
		for _, banned := range []string{"Write", "Edit", "NotebookEdit"} {
			if strings.Contains(rule, banned) {
				t.Fatalf("allowlist rule %q authorizes mutating tool %q", rule, banned)
			}
		}
		if strings.HasPrefix(rule, "Bash(") && !cerneBashRules[rule] {
			t.Fatalf("allowlist rule %q is a Bash rule outside the closed cerne read set", rule)
		}
	}
}

// TestCliProviderPassesDisallowedTools (F-0258 criterion 6): the secret-file
// denials must travel as --disallowedTools — a positive allowlist alone lets
// an authorized Read reach any file under the working dir.
func TestCliProviderPassesDisallowedTools(t *testing.T) {
	var gotArgs []string
	fake := func(_ context.Context, _, _ string, args []string, _ string) (string, string, error) {
		gotArgs = args
		return "ok", "", nil
	}
	p := &cliProvider{bin: "claude", run: fake, dir: "/proj", agentTools: AgentReadTools, deniedTools: AgentDeniedPaths}
	if _, err := p.Complete(context.Background(), "p"); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	joined := strings.Join(gotArgs, " ")
	if !strings.Contains(joined, "--disallowedTools") {
		t.Fatalf("args %v carry no --disallowedTools", gotArgs)
	}
	for _, denial := range []string{"Read(.env)", "Read(.env.*)", "Read(**/credentials*)", "Read(**/*.pem)", "Read(**/*.key)"} {
		if !strings.Contains(joined, denial) {
			t.Fatalf("args %v miss secret denial %q", gotArgs, denial)
		}
	}

	// One-shot mode (ask/compile) stays flag-free: no allow, no deny.
	gotArgs = nil
	p = &cliProvider{bin: "claude", run: fake}
	if _, err := p.Complete(context.Background(), "p"); err != nil {
		t.Fatalf("Complete one-shot: %v", err)
	}
	if strings.Contains(strings.Join(gotArgs, " "), "--disallowedTools") {
		t.Fatalf("one-shot args %v carry --disallowedTools", gotArgs)
	}
}

// TestAgentReadToolsNoWriteCommands is the negative boundary check (F-0224
// criterion 4): no rule in the allowlist may cover a mutating cerne command.
func TestAgentReadToolsNoWriteCommands(t *testing.T) {
	banned := []string{"feature", "new", "approve", "start", "complete", "accept",
		"reject", "retire", "push", "import", "retag", "substituir", "work", "hook"}
	for _, rule := range AgentReadTools {
		for _, b := range banned {
			if strings.Contains(rule, b) {
				t.Fatalf("allowlist rule %q covers write-adjacent token %q", rule, b)
			}
		}
	}
}

// TestAgentProviderFallbackOldBinary: a CLI too old to know --allowedTools
// (unknown flag/option error) gets exactly one retry in plain one-shot form.
func TestAgentProviderFallbackOldBinary(t *testing.T) {
	var calls [][]string
	fake := func(_ context.Context, _, _ string, args []string, _ string) (string, string, error) {
		calls = append(calls, args)
		if len(calls) == 1 {
			return "", "", errors.New("error: unknown option '--allowedTools'")
		}
		return "resposta one-shot", "", nil
	}
	p := &cliProvider{bin: "claude", run: fake, dir: "/proj", agentTools: AgentReadTools}

	out, err := p.Complete(context.Background(), "pergunta")
	if err != nil {
		t.Fatalf("Complete after fallback: %v", err)
	}
	if out != "resposta one-shot" {
		t.Fatalf("Complete = %q", out)
	}
	if len(calls) != 2 {
		t.Fatalf("runner called %d times, want 2 (agentic then one-shot)", len(calls))
	}
	for _, a := range calls[1] {
		if a == "--allowedTools" {
			t.Fatalf("fallback args %v still carry --allowedTools", calls[1])
		}
	}
	// A genuine failure (not an unknown flag) must NOT trigger the fallback.
	calls = nil
	failing := func(_ context.Context, _, _ string, args []string, _ string) (string, string, error) {
		calls = append(calls, args)
		return "", "", errors.New("exit status 1: not logged in")
	}
	p = &cliProvider{bin: "claude", run: failing, dir: "/proj", agentTools: AgentReadTools}
	if _, err := p.Complete(context.Background(), "pergunta"); !errors.Is(err, ErrClaudeCLI) {
		t.Fatalf("err = %v, want ErrClaudeCLI", err)
	}
	if len(calls) != 1 {
		t.Fatalf("runner called %d times on a real failure, want 1 (no retry)", len(calls))
	}
}

// TestAgentProviderLogsStderr (F-0224 criterion 4): whatever the CLI printed
// on stderr — where headless permission denials surface — must land in the
// server log even on a successful completion.
func TestAgentProviderLogsStderr(t *testing.T) {
	var buf strings.Builder
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	denial := "Permission denied: Bash(cerne feature new \"x\")"
	fake := func(_ context.Context, _, _ string, _ []string, _ string) (string, string, error) {
		return "resposta", denial, nil
	}
	p := &cliProvider{bin: "claude", run: fake, dir: "/proj", agentTools: AgentReadTools}
	if _, err := p.Complete(context.Background(), "p"); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if !strings.Contains(buf.String(), denial) {
		t.Fatalf("server log %q does not carry the CLI stderr %q", buf.String(), denial)
	}

	// One-shot mode stays quiet: ask/compile callers keep their clean log.
	buf.Reset()
	p = &cliProvider{bin: "claude", run: fake}
	if _, err := p.Complete(context.Background(), "p"); err != nil {
		t.Fatalf("Complete one-shot: %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("one-shot mode logged %q, want silence", buf.String())
	}
}

// TestNewAgentProviderModes (F-0224): claude-cli yields the agentic provider
// (allowlist + dir + default bin); byo-key passes through to the same static
// httpProvider (criterion 3); delegation keeps its sentinel.
func TestNewAgentProviderModes(t *testing.T) {
	prov, err := NewAgentProvider(project.LLMConfig{Mode: "claude-cli"}, "/proj", false)
	if err != nil {
		t.Fatalf("NewAgentProvider(claude-cli): %v", err)
	}
	cp, ok := prov.(*cliProvider)
	if !ok {
		t.Fatalf("provider = %T, want *cliProvider", prov)
	}
	if cp.bin != "claude" || cp.dir != "/proj" || !reflect.DeepEqual(cp.agentTools, AgentReadTools) {
		t.Fatalf("agentic provider misconfigured: bin=%q dir=%q tools=%v", cp.bin, cp.dir, cp.agentTools)
	}
	if !reflect.DeepEqual(cp.deniedTools, AgentDeniedPaths) {
		t.Fatalf("agentic provider deniedTools = %v, want AgentDeniedPaths (F-0258 criterion 6)", cp.deniedTools)
	}

	t.Setenv("TEST_CHAT_KEY", "k")
	prov, err = NewAgentProvider(project.LLMConfig{Mode: "byo-key", KeyEnv: "TEST_CHAT_KEY"}, "/proj", false)
	if err != nil {
		t.Fatalf("NewAgentProvider(byo-key): %v", err)
	}
	if _, ok := prov.(*httpProvider); !ok {
		t.Fatalf("byo-key provider = %T, want *httpProvider (static, no tool loop)", prov)
	}

	if _, err := NewAgentProvider(project.LLMConfig{}, "/proj", false); !errors.Is(err, ErrNoProviderForDelegation) {
		t.Fatalf("delegation err = %v, want ErrNoProviderForDelegation", err)
	}
}

// TestAgentRunStampsActor (F-0260 criterion 2): whatever the agent writes must
// be attributed to an agent, so app.CheckHumanGate keeps refusing approve/accept
// from it. agentRun is the only place that stamp can come from, and the parent
// environment must not be its source — proven with a REAL child process (the
// fake-runner tests never execute it), like TestExecRunCapturesStdoutOnFailure.
func TestAgentRunStampsActor(t *testing.T) {
	if os.Getenv("GO_WANT_ACTOR_CHILD") == "1" {
		fmt.Println("ACTOR:" + os.Getenv("CERNE_ACTOR"))
		os.Exit(0)
	}
	t.Setenv("GO_WANT_ACTOR_CHILD", "1")
	t.Setenv("CERNE_ACTOR", "")
	out, _, err := agentRun(context.Background(), "", os.Args[0],
		[]string{"-test.run=TestAgentRunStampsActor$"}, "")
	if err != nil {
		t.Fatalf("agentRun: %v", err)
	}
	if !strings.Contains(out, "ACTOR:agent") {
		t.Fatalf("child CERNE_ACTOR from %q, want the agent stamp", out)
	}
}

// TestAgentProviderUsesAgentRun pins the wiring: without it the stamp above
// exists but never reaches the CLI, and criterion 2 fails silently. The one-shot
// provider keeps plain execRun — ask/compile/diagram write nothing.
func TestAgentProviderUsesAgentRun(t *testing.T) {
	prov, err := NewAgentProvider(project.LLMConfig{Mode: "claude-cli"}, "/proj", false)
	if err != nil {
		t.Fatalf("NewAgentProvider: %v", err)
	}
	if got, want := runnerName(prov.(*cliProvider).run), runnerName(agentRun); got != want {
		t.Fatalf("agentic runner = %v, want agentRun (F-0260 criterion 2)", got)
	}
	one, err := NewProvider(project.LLMConfig{Mode: "claude-cli"})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	if got, want := runnerName(one.(*cliProvider).run), runnerName(execRun); got != want {
		t.Fatalf("one-shot runner = %v, want execRun (no actor stamp needed)", got)
	}
}

// runnerName identifies a runner by code pointer — funcs are not comparable.
func runnerName(r runner) uintptr { return reflect.ValueOf(r).Pointer() }

// TestAgentWriteToolsGatedByConfirm (F-0260 criterion 3): the write allowlist is
// a per-turn capability, never a standing one. An unconfirmed turn must get the
// read boundary of F-0224 byte for byte — proposta is not permissão.
func TestAgentWriteToolsGatedByConfirm(t *testing.T) {
	for _, tc := range []struct {
		name  string
		write bool
		want  []string
	}{
		{"unconfirmed", false, AgentReadTools},
		{"confirmed", true, append(append([]string{}, AgentReadTools...), AgentWriteTools...)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prov, err := NewAgentProvider(project.LLMConfig{Mode: "claude-cli"}, "/proj", tc.write)
			if err != nil {
				t.Fatalf("NewAgentProvider: %v", err)
			}
			if got := prov.(*cliProvider).agentTools; !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("agentTools = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestAgentWriteToolsScope (F-0269 criteria 1, 6): the boundary inverts. Until
// F-0268 the allowlist enumerated the safe verbs, and every verb it missed cost
// a round of "the agent invents a flag, the user files a feature". Now the
// allowlist is the whole binary and the refusal list carries the small, closed
// set that must stay human. Nothing outside `cerne` is granted — no Write, no
// git, no shell helper.
func TestAgentWriteToolsScope(t *testing.T) {
	want := []string{"Bash(cerne:*)"}
	if !reflect.DeepEqual(AgentWriteTools, want) {
		t.Fatalf("AgentWriteTools = %v, want %v — the boundary is the binary, not a verb list", AgentWriteTools, want)
	}
}

// TestAgentDeniedCommandsCoverLifecycle (F-0269 criteria 2, 7): with a broad
// allowlist, the refusal list IS the gate — so it is checked against the FSM
// itself, not against a hand-kept copy of it. A transition added to
// internal/domain without a refusal here fails this test instead of silently
// handing the agent a human gate. `accept`, `deliver` and `retire` are named
// separately: they succeed the approval without being FSM feature actions.
func TestAgentDeniedCommandsCoverLifecycle(t *testing.T) {
	// The CLI verb differs from the domain action only for supersede.
	verbOf := map[domain.Action]string{domain.ActionSupersede: "substituir"}

	for _, action := range domain.FeatureActions() {
		verb, ok := verbOf[action]
		if !ok {
			verb = string(action)
		}
		if !deniesVerb(AgentDeniedCommands, verb) {
			t.Errorf("AgentDeniedCommands does not refuse %q (domain action %q) — lifecycle stays a human act (F-0131)", verb, action)
		}
	}
	for _, verb := range []string{"accept", "deliver", "retire"} {
		if !deniesVerb(AgentDeniedCommands, verb) {
			t.Errorf("AgentDeniedCommands does not refuse %q — it succeeds the approval", verb)
		}
	}
}

// deniesVerb reports whether some refusal rule targets the given cerne verb.
func deniesVerb(rules []string, verb string) bool {
	for _, r := range rules {
		if strings.Contains(r, " "+verb) {
			return true
		}
	}
	return false
}

// TestAgentProviderDeniedTools (F-0269 criterion 2): the refusals reach the CLI
// on a confirmed turn — the secret-file paths of F-0258 plus the lifecycle
// commands. An unconfirmed turn has no write to refuse, so it carries the path
// refusals alone.
func TestAgentProviderDeniedTools(t *testing.T) {
	for _, tc := range []struct {
		name  string
		write bool
		want  []string
	}{
		{"unconfirmed", false, AgentDeniedPaths},
		{"confirmed", true, append(append([]string{}, AgentDeniedPaths...), AgentDeniedCommands...)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prov, err := NewAgentProvider(project.LLMConfig{Mode: "claude-cli"}, "/proj", tc.write)
			if err != nil {
				t.Fatalf("NewAgentProvider: %v", err)
			}
			if got := prov.(*cliProvider).deniedTools; !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("deniedTools = %v, want %v", got, tc.want)
			}
		})
	}
}
