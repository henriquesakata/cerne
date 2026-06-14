package llm

import (
	"context"
	"errors"
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
