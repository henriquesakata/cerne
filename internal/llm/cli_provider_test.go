package llm

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/project"
)

// TestCLIProviderCompleteSuccess drives cliProvider with a fake runner: it
// returns the runner's stdout (trimmed), the runner receives the exact argv the
// Claude Code headless invocation needs — "-p --output-format text" with no
// --bare and no --model — and the PROMPT travels on stdin, not argv.
func TestCLIProviderCompleteSuccess(t *testing.T) {
	var gotName, gotStdin string
	var gotArgs []string
	fake := func(_ context.Context, name string, args []string, stdin string) (string, error) {
		gotName = name
		gotArgs = args
		gotStdin = stdin
		return "  resposta da assinatura\n", nil
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
	fake := func(_ context.Context, _ string, args []string, stdin string) (string, error) {
		gotArgs = args
		gotStdin = stdin
		return "ok", nil
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
	fake := func(_ context.Context, _ string, _ []string, _ string) (string, error) {
		return "", cause
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
	_, err := execRun(context.Background(), os.Args[0],
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
