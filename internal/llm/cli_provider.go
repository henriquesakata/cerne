package llm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
)

// ModeClaudeCLI is the provider mode that shells to the Claude Code CLI
// ("claude -p"), using the user's Pro/Max subscription instead of an API key.
const ModeClaudeCLI = "claude-cli"

// ErrClaudeCLI wraps any failure of the claude-cli invocation (executable not
// found, non-zero exit, login missing). Callers detect it with errors.Is.
var ErrClaudeCLI = errors.New("claude-cli invocation failed")

// AgentReadTools is the exact tool allowlist the agentic chat provider grants
// the Claude Code CLI: cerne's READ commands (F-0224 criterion 1) plus the
// CLI's native read tools (F-0258 criterion 1), so the agent can ground a
// proposal in real project files. Native tools are used instead of
// Bash(grep:*)/Bash(cat:*) because a shell rule opens escape surface (pipes,
// ';', command substitution); Read/Grep/Glob never pass through a shell. The
// Bash entries are scoped to cerne's read subcommands so the sibling write
// commands (note new, decision new, reference new) stay outside the boundary.
// In headless mode (-p) every tool not on this list is denied by the CLI
// itself — this list is the whole write-protection boundary, so never add a
// mutating tool or a non-cerne Bash rule here.
var AgentReadTools = []string{
	"Read",
	"Grep",
	"Glob",
	"Bash(cerne search:*)",
	"Bash(cerne show:*)",
	"Bash(cerne brief:*)",
	"Bash(cerne status:*)",
	"Bash(cerne note list:*)",
	"Bash(cerne note show:*)",
	"Bash(cerne reference list:*)",
	"Bash(cerne decision list:*)",
}

// AgentDeniedPaths are the --disallowedTools rules that keep an authorized
// Read away from secret files (F-0258 criterion 6). The positive allowlist
// alone is not enough: Read reaches any file under the working dir. Fixed in
// the binary on purpose — like the role boundary, a project must not be able
// to switch it off via config.
var AgentDeniedPaths = []string{
	"Read(.env)",
	"Read(.env.*)",
	"Read(**/credentials*)",
	"Read(**/*.pem)",
	"Read(**/*.key)",
}

// runner abstracts running an external command in a working directory
// (feeding it stdin and capturing both output streams), so the provider can
// be unit-tested with a fake instead of the real Claude Code CLI.
type runner func(ctx context.Context, dir, name string, args []string, stdin string) (stdout, stderr string, err error)

// maxOutputBytes bounds how much of a failed command's output (either stream)
// is echoed in the error so a verbose CLI does not flood the message.
const maxOutputBytes = 512

// describeExit builds the description of a non-zero exit: it prefers stderr
// (the convention), falls back to stdout — labeled, so the reader knows which
// pipe it came from — when stderr is empty, and says so explicitly when
// neither stream carried anything. The Claude Code CLI reports auth failures
// on stdout with an empty stderr (F-0183); before the fallback, the error
// ended in a dangling colon and hid the one line naming the cause.
func describeExit(name, state string, stdout, stderr []byte) string {
	if len(bytes.TrimSpace(stderr)) > 0 {
		return fmt.Sprintf("%q exited with %s: %s", name, state, truncate(stderr, maxOutputBytes))
	}
	if len(bytes.TrimSpace(stdout)) > 0 {
		return fmt.Sprintf("%q exited with %s (stdout): %s", name, state, truncate(stdout, maxOutputBytes))
	}
	return fmt.Sprintf("%q exited with %s (no output)", name, state)
}

// execRun is the default runner: it runs name+args in dir (empty = inherit)
// via exec.CommandContext, feeding stdin to the process and returning both
// streams on success. A non-zero exit yields an error carrying the process's
// own explanation via describeExit; an exec error (e.g. executable not found)
// is reported with an actionable message naming the binary.
func execRun(ctx context.Context, dir, name string, args []string, stdin string) (string, string, error) {
	return execRunEnv(ctx, dir, name, args, stdin, nil)
}

// agentRun is the runner of the agentic provider (F-0260): same exec, plus the
// CERNE_ACTOR=agent stamp the child inherits. Any cerne command the agent runs
// is then attributed to an agent in the event log, and app.CheckHumanGate keeps
// refusing approve/accept from it — the human gates (F-0131) are untouched by
// the agent gaining write tools. os.Environ() comes first so the stamp wins
// over an inherited value (exec keeps the last of a duplicated key).
func agentRun(ctx context.Context, dir, name string, args []string, stdin string) (string, string, error) {
	return execRunEnv(ctx, dir, name, args, stdin, append(os.Environ(), "CERNE_ACTOR=agent"))
}

// execRunEnv is execRun with an explicit child environment; nil means inherit
// the server's own, which is what every non-agentic path wants (N-0011: an API
// proxy configured in the shell must keep applying transparently).
func execRunEnv(ctx context.Context, dir, name string, args []string, stdin string, env []string) (string, string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		return stdout.String(), stderr.String(), nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return "", "", errors.New(describeExit(name, exitErr.ProcessState.String(), stdout.Bytes(), stderr.Bytes()))
	}
	// Non-exit error: the binary could not be started (not in PATH, etc.).
	return "", "", fmt.Errorf("could not run %q (install Claude Code and ensure it is on PATH): %w", name, err)
}

// cliProvider is a Provider that performs one completion per call by shelling to
// the Claude Code CLI in headless mode. It carries no API key: authentication is
// the user's Claude Code subscription (claude /login or CLAUDE_CODE_OAUTH_TOKEN).
// With agentTools set (F-0224) the CLI runs as a read-only agent: it may chain
// the allowlisted cerne commands (executed in dir, the project root) before
// answering; nil agentTools is the original one-shot text generator, byte-for-
// byte, used by ask/compile/diagram.
type cliProvider struct {
	bin         string
	run         runner
	dir         string   // working directory for the CLI (project root in agentic mode)
	agentTools  []string // Claude Code permission rules passed via --allowedTools; nil = one-shot
	deniedTools []string // permission rules passed via --disallowedTools (secret-file denials, F-0258)
}

// isUnknownFlag reports whether err looks like the CLI rejecting a flag it
// does not know — an old Claude Code without --allowedTools. Covers the
// cobra ("unknown flag") and commander ("unknown option") phrasings.
func isUnknownFlag(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "unknown flag") || strings.Contains(msg, "unknown option")
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
//
// In agentic mode (agentTools set, F-0224) the allowlist travels as one
// comma-joined --allowedTools argument; a CLI too old to know the flag gets
// one retry in plain one-shot form. Whatever the CLI printed on stderr — the
// stream where headless permission denials surface — is echoed to the server
// log, so an attempt outside the allowlist is visible (criterion 4).
func (p *cliProvider) Complete(ctx context.Context, prompt string) (string, error) {
	args := []string{"-p", "--output-format", "text"}
	agentic := len(p.agentTools) > 0
	if agentic {
		args = append(args, "--allowedTools", strings.Join(p.agentTools, ","))
		if len(p.deniedTools) > 0 {
			args = append(args, "--disallowedTools", strings.Join(p.deniedTools, ","))
		}
	}
	out, stderr, err := p.run(ctx, p.dir, p.bin, args, prompt)
	if err != nil && agentic && isUnknownFlag(err) {
		out, stderr, err = p.run(ctx, p.dir, p.bin, []string{"-p", "--output-format", "text"}, prompt)
	}
	if err != nil {
		return "", fmt.Errorf("%w: failed to call Claude Code (claude -p): %w — check 'claude /login' (subscription) or use [llm] mode=\"byo-key\"", ErrClaudeCLI, err)
	}
	if s := strings.TrimSpace(stderr); agentic && s != "" {
		log.Printf("claude-cli stderr: %s", truncate([]byte(s), maxOutputBytes))
	}
	return strings.TrimSpace(out), nil
}
