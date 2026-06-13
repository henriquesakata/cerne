package llm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// ModeClaudeCLI is the provider mode that shells to the Claude Code CLI
// ("claude -p"), using the user's Pro/Max subscription instead of an API key.
const ModeClaudeCLI = "claude-cli"

// ErrClaudeCLI wraps any failure of the claude-cli invocation (executable not
// found, non-zero exit, login missing). Callers detect it with errors.Is.
var ErrClaudeCLI = errors.New("claude-cli invocation failed")

// runner abstracts running an external command and capturing its stdout, so the
// provider can be unit-tested with a fake instead of the real Claude Code CLI.
type runner func(ctx context.Context, name string, args []string) (string, error)

// maxStderrBytes bounds how much of a failed command's stderr is echoed in the
// error so a verbose CLI does not flood the message.
const maxStderrBytes = 512

// execRun is the default runner: it runs name+args via exec.CommandContext,
// returning stdout on success. A non-zero exit yields an error including the
// (truncated) stderr; an exec error (e.g. executable not found) is reported with
// an actionable message naming the binary.
func execRun(ctx context.Context, name string, args []string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		return stdout.String(), nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return "", fmt.Errorf("%q saiu com %s: %s", name, exitErr.ProcessState.String(), truncate(stderr.Bytes(), maxStderrBytes))
	}
	// Non-exit error: the binary could not be started (not in PATH, etc.).
	return "", fmt.Errorf("não foi possível executar %q (instale o Claude Code e garanta que está no PATH): %w", name, err)
}

// cliProvider is a Provider that performs one completion per call by shelling to
// the Claude Code CLI in headless mode. It carries no API key: authentication is
// the user's Claude Code subscription (claude /login or CLAUDE_CODE_OAUTH_TOKEN).
type cliProvider struct {
	bin string
	run runner
}

// Complete runs `claude -p <prompt> --output-format text` and returns the
// trimmed stdout. It deliberately omits --bare (which would ignore the
// subscription token and require an API key) and --model (which would force a
// model possibly unavailable on the plan). Any failure becomes an actionable
// ErrClaudeCLI wrapping the cause.
func (p *cliProvider) Complete(ctx context.Context, prompt string) (string, error) {
	out, err := p.run(ctx, p.bin, []string{"-p", prompt, "--output-format", "text"})
	if err != nil {
		return "", fmt.Errorf("%w: falha ao chamar o Claude Code (claude -p): %w — verifique 'claude /login' (assinatura) ou use [llm] mode=\"byo-key\"", ErrClaudeCLI, err)
	}
	return strings.TrimSpace(out), nil
}
