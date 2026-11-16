package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/henriquesakata/cerne/internal/project"
)

// TestCLIProviderCompleteSuccess drives cliProvider with a fake runner: it
// returns the runner's stdout (trimmed), the runner receives the exact argv the
// Claude Code headless invocation needs — "-p --output-format text" plus the
// one-shot isolation flags (F-0287), with no --bare and no --model — and the
// PROMPT travels on stdin, not argv.
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
	want := append([]string{"-p", "--output-format", "text"}, oneShotIsolation...)
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

// TestExecRunFeedsValidUTF8 (B-0005): every CLI provider prompt reaches the
// child as valid UTF-8, whatever bytes the caller built it from — a source file
// in Latin-1 or a byte-cut rune made codex exec refuse the whole prompt.
func TestExecRunFeedsValidUTF8(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		in, _ := io.ReadAll(os.Stdin)
		if !utf8.Valid(in) {
			fmt.Println("INVALID UTF-8 on stdin")
			os.Exit(1)
		}
		fmt.Print("VALID")
		os.Exit(0)
	}
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	out, _, err := execRun(context.Background(), "", os.Args[0],
		[]string{"-test.run=TestExecRunFeedsValidUTF8$"}, "fonte em latin-1: a\xe7\xe3o, corte no meio: \xc3")
	if err != nil {
		t.Fatalf("execRun: %v", err)
	}
	if out != "VALID" {
		t.Fatalf("child stdout = %q, want VALID", out)
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
		"Bash(cerne note list:*),Bash(cerne note show:*),Bash(cerne reference list:*),Bash(cerne decision list:*)," +
		"Bash(cerne idea list:*)"
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
// set of the 9 cerne read commands — the allowlist grows only by scoped
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
		"Bash(cerne idea list:*)":      true,
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
	withDenied(t, "Bash(cerne feature approve:*)")
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

// withDenied injects the refusal rules cli derives from the command classes
// (F-0323 C19): this package never sees the tree, so its tests set them.
func withDenied(t *testing.T, rules ...string) {
	t.Helper()
	orig := AgentDeniedCommands
	AgentDeniedCommands = rules
	t.Cleanup(func() { AgentDeniedCommands = orig })
}

// TestAgentWriteNeedsDerivedDenials (F-0323 C19): the broad write grant only
// exists paired with refusals derived from the command tree; a process where
// they were never injected gets the read-only agent, never an unfenced write.
func TestAgentWriteNeedsDerivedDenials(t *testing.T) {
	withDenied(t)
	prov, err := NewAgentProvider(project.LLMConfig{Mode: "claude-cli"}, "/proj", true)
	if err != nil {
		t.Fatal(err)
	}
	if got := prov.(*cliProvider).agentTools; !reflect.DeepEqual(got, AgentReadTools) {
		t.Fatalf("agentTools = %v, want the read-only set without derived denials", got)
	}
}

// TestAgentProviderDeniedTools (F-0269 criterion 2): the refusals reach the CLI
// on a confirmed turn — the secret-file paths of F-0258 plus the lifecycle
// commands. An unconfirmed turn has no write to refuse, so it carries the path
// refusals alone.
func TestAgentProviderDeniedTools(t *testing.T) {
	withDenied(t, "Bash(cerne feature approve:*)")
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

// TestCLIProviderOneShotIsolatesUserSession is F-0287 criterion 2: `claude -p`
// boots a full Claude Code session in the cwd, so without isolation the wiki
// synthesis inherits the user's CLAUDE.md, hooks, plugins and MCP servers — and
// the tools that let it write the page to a scratchpad file and answer with a
// report about it. The one-shot invocation must carry --safe-mode (customizations
// off, auth untouched) and --tools "" (no tools at all).
func TestCLIProviderOneShotIsolatesUserSession(t *testing.T) {
	var gotArgs []string
	fake := func(_ context.Context, _, _ string, args []string, _ string) (string, string, error) {
		gotArgs = args
		return "página", "", nil
	}
	p := &cliProvider{bin: "claude", run: fake}

	if _, err := p.Complete(context.Background(), "sintetize"); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	want := []string{"-p", "--output-format", "text", "--safe-mode", "--tools", ""}
	if !reflect.DeepEqual(gotArgs, want) {
		t.Fatalf("runner args = %v, want %v", gotArgs, want)
	}
}

// TestAgentProviderKeepsUserSession is the other half of F-0287 criterion 2: the
// isolation belongs to the one-shot generator alone. The Backlog agent (F-0224)
// runs IN the project on purpose and its whole write-protection boundary is the
// allowlist, so --safe-mode/--tools must never reach it.
func TestAgentProviderKeepsUserSession(t *testing.T) {
	var gotArgs []string
	fake := func(_ context.Context, _, _ string, args []string, _ string) (string, string, error) {
		gotArgs = args
		return "resposta", "", nil
	}
	p := &cliProvider{bin: "claude", run: fake, dir: "/proj", agentTools: AgentReadTools}

	if _, err := p.Complete(context.Background(), "qual a spec da F-0132?"); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	for _, banned := range []string{"--safe-mode", "--tools"} {
		for _, a := range gotArgs {
			if a == banned {
				t.Fatalf("agentic args %v must not contain %q (it would disable the agent's own tools)", gotArgs, banned)
			}
		}
	}
}

// TestCLIProviderOneShotRetriesWithoutIsolationFlags covers a Claude Code too
// old to know --safe-mode/--tools: the one-shot falls back to the bare headless
// form instead of failing the synthesis, mirroring the agentic retry.
func TestCLIProviderOneShotRetriesWithoutIsolationFlags(t *testing.T) {
	var calls [][]string
	fake := func(_ context.Context, _, _ string, args []string, _ string) (string, string, error) {
		calls = append(calls, args)
		if len(calls) == 1 {
			return "", "", errors.New("error: unknown option '--safe-mode'")
		}
		return "página", "", nil
	}
	p := &cliProvider{bin: "claude", run: fake}

	out, err := p.Complete(context.Background(), "sintetize")
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if out != "página" {
		t.Fatalf("Complete = %q, want the retry's output", out)
	}
	if len(calls) != 2 {
		t.Fatalf("runner called %d time(s), want 2 (attempt + bare retry)", len(calls))
	}
	if want := []string{"-p", "--output-format", "text"}; !reflect.DeepEqual(calls[1], want) {
		t.Fatalf("retry args = %v, want %v", calls[1], want)
	}
}

// codexFake plays `codex exec`: it records argv/stdin/cwd and writes reply to
// the file named by -o, like the real CLI does.
func codexFake(reply string, gotArgs *[]string, gotStdin, gotDir *string) runner {
	return func(_ context.Context, dir, _ string, args []string, stdin string) (string, string, error) {
		*gotArgs, *gotStdin, *gotDir = args, stdin, dir
		for i, a := range args {
			if a == "-o" {
				if err := os.WriteFile(args[i+1], []byte(reply), 0o600); err != nil {
					return "", "", err
				}
			}
		}
		return "noise on stdout", "", nil
	}
}

func TestCodexCLIProvider(t *testing.T) {
	var args []string
	var stdin, dir string
	p := &cliProvider{kind: ModeCodexCLI, bin: "codex", model: "gpt-5", run: codexFake("  the answer\n", &args, &stdin, &dir)}
	out, err := p.Complete(context.Background(), "question?")
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if out != "the answer" {
		t.Fatalf("out = %q, want the -o file content", out)
	}
	if stdin != "question?" {
		t.Fatalf("stdin = %q", stdin)
	}
	if dir == "" {
		t.Fatal("codex must run in an empty temp dir, not the project")
	}
	// F-0414 (B4): no tools, no user config, no AGENTS.md, no hosted search.
	want := []string{"exec", "--skip-git-repo-check", "--ephemeral", "--sandbox", "read-only",
		"--ignore-user-config", "--ignore-rules", "--disable", "shell_tool", "--disable", "apps",
		"-c", `web_search="disabled"`, "-c", "project_doc_max_bytes=0",
		"-o", args[16], "-m", "gpt-5", "-"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args = %v, want %v", args, want)
	}
	if _, err := os.Stat(dir); err == nil {
		t.Fatal("temp dir must be removed after the call")
	}
	// No model: no -m.
	p.model = ""
	if _, err := p.Complete(context.Background(), "q"); err != nil {
		t.Fatal(err)
	}
	for _, a := range args {
		if a == "-m" {
			t.Fatalf("-m passed without a model: %v", args)
		}
	}
}

// B-0006: the effort chosen in the chat reaches codex exec as a config
// override; a value that is not a bare word never reaches the argv.
func TestCodexCLIPassesEffort(t *testing.T) {
	var args []string
	var stdin, dir string
	p := &cliProvider{kind: ModeCodexCLI, bin: "codex", model: "gpt-5", effort: "high", run: codexFake("ok", &args, &stdin, &dir)}
	if _, err := p.Complete(context.Background(), "q"); err != nil {
		t.Fatal(err)
	}
	tail := args[len(args)-5:]
	if want := []string{"-m", "gpt-5", "-c", `model_reasoning_effort="high"`, "-"}; !reflect.DeepEqual(tail, want) {
		t.Fatalf("argv tail = %v, want %v", tail, want)
	}
	p.effort = "high\"\nsandbox_mode=\"danger-full-access"
	if _, err := p.Complete(context.Background(), "q"); err != nil {
		t.Fatal(err)
	}
	for _, a := range args {
		if strings.Contains(a, "model_reasoning_effort") {
			t.Fatalf("a non-word effort reached the argv: %v", args)
		}
	}
	// The per-call effort travels in LLMConfig, never read from a file.
	np, _ := NewProvider(project.LLMConfig{Mode: ModeCodexCLI, Effort: "low"})
	if e := np.(*cliProvider).effort; e != "low" {
		t.Fatalf("NewProvider effort = %q, want low", e)
	}
}

func TestCodexCLIErrorsNameTheFix(t *testing.T) {
	for _, c := range []struct{ err, want string }{
		{"could not run \"codex\" (install Codex CLI and ensure it is on PATH): x", "install Codex CLI"},
		{"\"codex\" exited with exit status 1: not logged in", "codex login"},
	} {
		p := &cliProvider{kind: ModeCodexCLI, bin: "codex", run: func(context.Context, string, string, []string, string) (string, string, error) {
			return "", "", errors.New(c.err)
		}}
		_, err := p.Complete(context.Background(), "q")
		if !errors.Is(err, ErrCodexCLI) || errors.Is(err, ErrClaudeCLI) {
			t.Fatalf("err = %v, want ErrCodexCLI only", err)
		}
		if !strings.Contains(err.Error(), c.want) || strings.Contains(err.Error(), "Claude") {
			t.Fatalf("err = %v, want %q and no Claude wording", err, c.want)
		}
	}
}

func TestCodexCLIEmptyAnswerIsAnError(t *testing.T) {
	var a []string
	var s, d string
	p := &cliProvider{kind: ModeCodexCLI, bin: "codex", run: codexFake("  ", &a, &s, &d)}
	if _, err := p.Complete(context.Background(), "q"); !errors.Is(err, ErrCodexCLI) {
		t.Fatalf("err = %v, want ErrCodexCLI", err)
	}
}

func TestGeminiCLIProvider(t *testing.T) {
	var args []string
	var stdin, dir string
	fake := func(_ context.Context, d, _ string, a []string, in string) (string, string, error) {
		args, stdin, dir = a, in, d
		return `{"response":" hello \n","stats":{}}`, "", nil
	}
	p := &cliProvider{kind: ModeGeminiCLI, bin: "gemini", model: "gemini-2.5-pro", run: fake}
	out, err := p.Complete(context.Background(), "hi?")
	if err != nil {
		t.Fatal(err)
	}
	if out != "hello" || stdin != "hi?" || dir == "" {
		t.Fatalf("out=%q stdin=%q dir=%q", out, stdin, dir)
	}
	want := []string{"--output-format", "json", "--approval-mode", "plan", "-e", "none", "-m", "gemini-2.5-pro"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args = %v, want %v", args, want)
	}
	p.model = ""
	p.Complete(context.Background(), "q")
	if !reflect.DeepEqual(args, want[:6]) {
		t.Fatalf("args without model = %v", args)
	}
}

func TestGeminiCLIErrors(t *testing.T) {
	run := func(out string, err error) runner {
		return func(context.Context, string, string, []string, string) (string, string, error) { return out, "", err }
	}
	cases := []struct {
		name string
		run  runner
		want string
	}{
		{"missing", run("", errors.New("could not run \"gemini\" (install Gemini CLI and ensure it is on PATH): x")), "install Gemini CLI"},
		{"invalid input", run("", errors.New("\"gemini\" exited with exit status 42: bad")), "invalid input"},
		{"login", run("", errors.New("\"gemini\" exited with exit status 1: auth")), "run gemini and sign in"},
		{"bad json", run("not json", nil), "unreadable"},
		{"json error", run(`{"error":{"message":"quota"}}`, nil), "quota"},
	}
	for _, c := range cases {
		p := &cliProvider{kind: ModeGeminiCLI, bin: "gemini", run: c.run}
		_, err := p.Complete(context.Background(), "q")
		if !errors.Is(err, ErrGeminiCLI) || !strings.Contains(err.Error(), c.want) || strings.Contains(err.Error(), "Claude") {
			t.Errorf("%s: err = %v, want ErrGeminiCLI containing %q", c.name, err, c.want)
		}
	}
}

func TestExecRunNamesTheMissingCLI(t *testing.T) {
	for bin, want := range map[string]string{"cerne-no-such-claude": "install Claude Code", "cerne-no-such-codex": "install Codex CLI", "cerne-no-such-gemini": "install Gemini CLI"} {
		_, _, err := execRun(context.Background(), "", bin, nil, "")
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v, want %q", bin, err, want)
		}
	}
}

func TestNewProviderSubscriptionCLIs(t *testing.T) {
	for mode, kind := range map[string]string{ModeCodexCLI: ModeCodexCLI, ModeGeminiCLI: ModeGeminiCLI} {
		for _, build := range []func() (Provider, error){
			func() (Provider, error) { return NewProvider(project.LLMConfig{Mode: mode, Model: "m"}) },
			func() (Provider, error) {
				return NewAgentProvider(project.LLMConfig{Mode: mode, Model: "m"}, "/x", true)
			},
		} {
			p, err := build()
			cp, ok := p.(*cliProvider)
			if err != nil || !ok || cp.kind != kind || cp.model != "m" || cp.agentTools != nil {
				t.Fatalf("%s: %v %#v", mode, err, p)
			}
			if _, isTool := p.(ToolCaller); isTool {
				t.Fatalf("%s must not be a ToolCaller (chat has no execution channel)", mode)
			}
		}
	}
	// No model configured: no Anthropic default leaks into -m.
	p, _ := NewProvider(project.LLMConfig{Mode: ModeCodexCLI})
	if m := p.(*cliProvider).model; m != "" {
		t.Fatalf("model = %q, want empty", m)
	}
	if got := ConfiguredProviders(project.LLMConfig{Mode: ModeCodexCLI}); !reflect.DeepEqual(got, []string{"openai"}) {
		t.Fatalf("codex providers = %v", got)
	}
	if got := ConfiguredProviders(project.LLMConfig{Mode: ModeGeminiCLI}); !reflect.DeepEqual(got, []string{"gemini"}) {
		t.Fatalf("gemini providers = %v", got)
	}
	if _, err := ListModels(context.Background(), project.LLMConfig{Mode: ModeGeminiCLI}); !errors.Is(err, ErrNoListing) {
		t.Fatalf("ListModels err = %v", err)
	}
}

// F-0414: a call that ends on the context deadline is a timeout, not a missing
// login; only a real non-zero exit keeps the login hint.
func TestCLITimeoutIsNotALoginProblem(t *testing.T) {
	for _, kind := range []string{ModeClaudeCLI, ModeCodexCLI, ModeGeminiCLI} {
		p := &cliProvider{kind: kind, bin: "x", run: func(context.Context, string, string, []string, string) (string, string, error) {
			return "", "", fmt.Errorf("run: %w", context.DeadlineExceeded)
		}}
		_, err := p.Complete(context.Background(), "q")
		if err == nil || !strings.Contains(err.Error(), "timed out after") {
			t.Fatalf("%s: err = %v, want a timeout message", kind, err)
		}
		if strings.Contains(err.Error(), "login") || strings.Contains(err.Error(), "sign in") {
			t.Fatalf("%s: err = %v, must not blame the login", kind, err)
		}
	}
}

func TestCLICanceledContextIsATimeoutToo(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	p := &cliProvider{kind: ModeClaudeCLI, bin: "claude", run: func(context.Context, string, string, []string, string) (string, string, error) {
		cancel()
		return "", "", errors.New(`"claude" exited with signal: killed`)
	}}
	_, err := p.Complete(ctx, "q")
	if err == nil || !strings.Contains(err.Error(), "timed out after") || strings.Contains(err.Error(), "/login") {
		t.Fatalf("err = %v, want a timeout, no login hint", err)
	}
}

// A Codex-like stderr: banner first, the real error last, colored.
func TestDescribeExitKeepsTheTailAndStripsANSI(t *testing.T) {
	banner := strings.Repeat("OpenAI Codex v0.1 banner line\n", 60)
	stderr := banner + "\x1b[31mERROR:\x1b[0m stream disconnected: model \x1b[1mgpt-9\x1b[0m is not available\n"
	got := describeExit("codex", "exit status 1", nil, []byte(stderr))
	if !strings.Contains(got, "is not available") {
		t.Fatalf("describeExit() = %q, lost the final error line", got)
	}
	if strings.Contains(got, "\x1b[") {
		t.Fatalf("describeExit() = %q, leaks ANSI escapes", got)
	}
	if len(got) > maxOutputBytes+200 {
		t.Fatalf("describeExit() length = %d, want bounded", len(got))
	}
}

// F-0414 (B4): Gemini runs under a system settings file (it overrides user and
// project ones) that leaves it no tool, no MCP server and no context file.
func TestGeminiRunsUnderLockedSettings(t *testing.T) {
	var env []string
	var settings map[string]any
	var path string
	fake := func(ctx context.Context, _, _ string, _ []string, _ string) (string, string, error) {
		env, _ = ctx.Value(envKey{}).([]string)
		for _, kv := range env {
			if v, ok := strings.CutPrefix(kv, "GEMINI_CLI_SYSTEM_SETTINGS_PATH="); ok {
				path = v
			}
		}
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("settings file unreadable during the call: %v", err)
		}
		if err := json.Unmarshal(b, &settings); err != nil {
			t.Fatal(err)
		}
		return `{"response":"ok"}`, "", nil
	}
	p := &cliProvider{kind: ModeGeminiCLI, bin: "gemini", run: fake}
	if _, err := p.Complete(context.Background(), "q"); err != nil {
		t.Fatal(err)
	}
	if path == "" {
		t.Fatalf("GEMINI_CLI_SYSTEM_SETTINGS_PATH not set: %v", env)
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("the settings file must be removed after the call")
	}
	tools := settings["tools"].(map[string]any)
	excluded := tools["exclude"].([]any)
	for _, name := range []string{"read_file", "read_many_files", "glob", "search_file_content", "list_directory", "run_shell_command", "web_fetch", "google_web_search", "save_memory", "write_file", "replace"} {
		if !slices.Contains(excluded, any(name)) {
			t.Errorf("tool %s not excluded: %v", name, excluded)
		}
	}
	if core := tools["core"].([]any); len(core) != 1 || core[0] != "__cerne_no_tool__" {
		t.Errorf("tools.core = %v, want the unusable sentinel", core)
	}
	if servers := settings["mcpServers"].(map[string]any); len(servers) != 0 {
		t.Errorf("mcpServers = %v", servers)
	}
	if ctxf := settings["context"].(map[string]any)["fileName"].([]any); len(ctxf) != 1 || ctxf[0] != "__cerne_none__.md" {
		t.Errorf("context.fileName = %v", ctxf)
	}
}

// Fail closed: a CLI too old to know an isolation flag is an error naming the
// update, never a retry with less isolation.
func TestCLIRejectingAnIsolationFlagFailsClosed(t *testing.T) {
	for _, kind := range []string{ModeCodexCLI, ModeGeminiCLI} {
		calls := 0
		p := &cliProvider{kind: kind, bin: "x", run: func(context.Context, string, string, []string, string) (string, string, error) {
			calls++
			return "", "", errors.New(`"x" exited with exit status 2: error: unexpected argument '--ignore-rules' found`)
		}}
		_, err := p.Complete(context.Background(), "q")
		if err == nil || !strings.Contains(err.Error(), "update") || calls != 1 {
			t.Errorf("%s: err = %v calls = %d, want one call and an update message", kind, err, calls)
		}
	}
}

func TestGeminiEmptyResponseIsAnError(t *testing.T) {
	p := &cliProvider{kind: ModeGeminiCLI, bin: "gemini", run: func(context.Context, string, string, []string, string) (string, string, error) {
		return `{"response":"  "}`, "", nil
	}}
	if _, err := p.Complete(context.Background(), "q"); !errors.Is(err, ErrGeminiCLI) {
		t.Fatalf("err = %v, want ErrGeminiCLI for an empty answer", err)
	}
}

// F-0414 (M6): codex login is the fix only when the error looks like auth.
func TestCodexFixHintOnlyForAuthErrors(t *testing.T) {
	cases := map[string]bool{
		`"codex" exited with exit status 1: 401 Unauthorized`:             true,
		`"codex" exited with exit status 1: please sign in`:               true,
		`"codex" exited with exit status 1: not logged in`:                true,
		`"codex" exited with exit status 1: model gpt-9 is not supported`: false,
	}
	for msg, wantHint := range cases {
		p := &cliProvider{kind: ModeCodexCLI, bin: "codex", run: func(context.Context, string, string, []string, string) (string, string, error) {
			return "", "", errors.New(msg)
		}}
		_, err := p.Complete(context.Background(), "q")
		if err == nil || strings.Contains(err.Error(), "codex login") != wantHint {
			t.Errorf("%q: err = %v, want hint=%v", msg, err, wantHint)
		}
	}
}

func TestCodexChatProjectFiles(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project with spaces")
	prov, err := NewAgentProvider(project.LLMConfig{Mode: ModeCodexCLI}, root, false)
	if err != nil {
		t.Fatal(err)
	}
	var args []string
	var stdin, cwd string
	cp := prov.(*cliProvider)
	cp.run = codexFake("ok", &args, &stdin, &cwd)
	if _, err := cp.Complete(t.Context(), "find the stage"); err != nil {
		t.Fatal(err)
	}
	want, _ := json.Marshal([]string{"mcp", "--project-files", "--dir", root})
	if cwd == root || !slices.Contains(args, "mcp_servers.cerne_project.args="+string(want)) || !slices.Contains(args, "shell_tool") {
		t.Fatalf("unexpected Codex invocation in %q: %v", cwd, args)
	}
}
