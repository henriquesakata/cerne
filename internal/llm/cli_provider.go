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

// runner abstracts running an external command (feeding it stdin and capturing
// its stdout), so the provider can be unit-tested with a fake instead of the
// real Claude Code CLI.
type runner func(ctx context.Context, name string, args []string, stdin string) (string, error)

// maxStderrBytes bounds how much of a failed command's stderr is echoed in the
// error so a verbose CLI does not flood the message.
const maxStderrBytes = 512

// execRun is the default runner: it runs name+args via exec.CommandContext,
// feeding stdin to the process and returning stdout on success. A non-zero exit
// yields an error including the (truncated) stderr; an exec error (e.g.
// executable not found) is reported with an actionable message naming the binary.
func execRun(ctx context.Context, name string, args []string, stdin string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		return stdout.String(), nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return "", fmt.Errorf("%q exited with %s: %s", name, exitErr.ProcessState.String(), truncate(stderr.Bytes(), maxStderrBytes))
	}
	// Non-exit error: the binary could not be started (not in PATH, etc.).
	return "", fmt.Errorf("could not run %q (install Claude Code and ensure it is on PATH): %w", name, err)
}

// cliProvider is a Provider that performs one completion per call by shelling to
// the Claude Code CLI in headless mode. It carries no API key: authentication is
// the user's Claude Code subscription (claude /login or CLAUDE_CODE_OAUTH_TOKEN).
type cliProvider struct {
	bin string
	run runner
}

// Complete runs `claude -p --output-format text` with the prompt on STDIN (not
// argv) and returns the trimmed stdout. The prompt must go through stdin because
// Windows caps a process command line near 32 KB, and real prompts (compiled
// wiki synthesis, ask with retrieved context) routinely exceed that — passing
// them as an argument fails with "The filename or extension is too long".
// `claude -p` reads the prompt from stdin when no positional prompt is given.
// It deliberately omits --bare (which would ignore the subscription token and
// require an API key) and --model (which would force a model possibly
// unavailable on the plan). Any failure becomes an actionable ErrClaudeCLI.
func (p *cliProvider) Complete(ctx context.Context, prompt string) (string, error) {
	out, err := p.run(ctx, p.bin, []string{"-p", "--output-format", "text"}, prompt)
	if err != nil {
		return "", fmt.Errorf("%w: failed to call Claude Code (claude -p): %w — check 'claude /login' (subscription) or use [llm] mode=\"byo-key\"", ErrClaudeCLI, err)
	}
	return strings.TrimSpace(out), nil
}
