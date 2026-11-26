package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// ModeClaudeCLI is the provider mode that shells to the Claude Code CLI
// ("claude -p"), using the user's Pro/Max subscription instead of an API key.
const ModeClaudeCLI = "claude-cli"

// ErrClaudeCLI wraps any failure of the claude-cli invocation (executable not
// found, non-zero exit, login missing). Callers detect it with errors.Is.
var ErrClaudeCLI = errors.New("claude-cli invocation failed")

// ErrCodexCLI, ErrGeminiCLI and ErrCursorCLI are ErrClaudeCLI's siblings for
// the other subscription CLIs (F-0414, F-0441).
var (
	ErrCodexCLI   = errors.New("codex-cli invocation failed")
	ErrGeminiCLI  = errors.New("gemini-cli invocation failed")
	ErrCursorCLI  = errors.New("cursor-cli invocation failed")
	ErrCopilotCLI = errors.New("copilot-cli invocation failed")
)

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
	"Bash(cerne idea list:*)",
	"Bash(cerne spec check:*)", // lints a draft body; writes nothing (F-0438)
}

// AgentWriteTools is what a CONFIRMED turn adds on top of AgentReadTools
// (F-0260): the whole cerne binary (F-0269). It is appended per request and
// never by default — an unconfirmed turn sees the read boundary alone, so a
// proposal is not a permission.
//
// The boundary used to enumerate the safe verbs, and it was wrong three times
// running: each verb it missed (the spec write, the module, the tags) cost a
// round of the agent inventing a flag, the user filing a feature, and the list
// growing by one. The dangerous set is the small, closed and stable one — the
// lifecycle — so that is what AgentDeniedCommands names. Everything before the
// approval is revisable, reversible and visible in the event log, which is why
// the Backlog agent gets the same reach a human has at the terminal.
//
// Nothing outside the binary is granted: no Write, no git, no shell helper.
var AgentWriteTools = []string{"Bash(cerne:*)"}

// AgentDeniedCommands are the --disallowedTools rules that keep the broad grant
// above away from the lifecycle (F-0269 criterion 2): transitions, accept,
// deliver, retire, supersede, import. With a broad allowlist this list IS the
// gate, so it is never hand-kept: internal/cli derives it at init from the
// lifecycle marks (verbs.MarkLifecycle), hidden twins included (B-0039: the
// pt-BR `substituir` runs like `supersede`), the same class the API-token tool
// channel refuses, so both modes deny the lifecycle alike (F-0323 crit. 4).
// Nil — a process that never linked the tree — means NewAgentProvider grants
// no write.
//
// It is the FIRST layer, not the only one: these rules match by prefix, and a
// prefix rule is brittle (a flag before the verb, command chaining, spacing).
// agentRun puts two stamps on every command the agent runs: under
// DenyLifecycleEnv the binary refuses every lifecycle verb (B-0040), and under
// CERNE_ACTOR=agent app.CheckHumanGate refuses approve/accept/revert, so the
// lifecycle survives a harness rule that fails to match.
var AgentDeniedCommands []string

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

var ansiRE = regexp.MustCompile("\x1b\\[[0-9;?]*[ -/]*[@-~]")

// tail strips ANSI escapes and keeps the LAST n bytes: a CLI prints its banner
// first and the real error last (Codex), so the head is the useless end.
func tail(data []byte, n int) string {
	s := strings.TrimSpace(ansiRE.ReplaceAllString(string(data), ""))
	if len(s) <= n {
		return s
	}
	s = s[len(s)-n:]
	for len(s) > 0 && !utf8.RuneStart(s[0]) {
		s = s[1:]
	}
	return "…" + s
}

// cliFailure wraps a failed CLI call. A call that ended on the context (the
// deadline passed, or it was canceled) timed out: the login hint would send the
// user to the wrong fix, so only a real failure keeps it.
func cliFailure(ctx context.Context, start time.Time, sentinel error, what string, err error, hint string) error {
	if ctx.Err() != nil || errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%w: %s timed out after %d seconds", sentinel, what, int(time.Since(start).Round(time.Second)/time.Second))
	}
	return fmt.Errorf("%w: failed to call %s: %w — %s", sentinel, what, err, hint)
}

// describeExit builds the description of a non-zero exit: it prefers stderr
// (the convention), falls back to stdout — labeled, so the reader knows which
// pipe it came from — when stderr is empty, and says so explicitly when
// neither stream carried anything. The Claude Code CLI reports auth failures
// on stdout with an empty stderr (F-0183); before the fallback, the error
// ended in a dangling colon and hid the one line naming the cause.
func describeExit(name, state string, stdout, stderr []byte) string {
	msg := describeExitBase(name, state, stdout, stderr)
	if os.Getenv("CODEX_THREAD_ID") != "" {
		msg += " — " + CodexSandboxHint
	}
	return msg
}

// CodexSandboxHint (F-0484) is appended, inside a Codex session, to errors the
// sandbox's cut network can cause; cli.cacheRefreshError shares it.
const CodexSandboxHint = "the Codex sandbox may be cutting the network; to open it for this project, run: cerne hook install --provider codex --network"

func describeExitBase(name, state string, stdout, stderr []byte) string {
	// The Copilot CLI's JSON mode reports a failure as a session.error event on
	// stdout; stderr noise (an update notice) must not hide it.
	if msg := copilotSessionError(stdout); msg != "" {
		return fmt.Sprintf("%q exited with %s: %s", name, state, msg)
	}
	if len(bytes.TrimSpace(stderr)) > 0 {
		return fmt.Sprintf("%q exited with %s: %s", name, state, tail(stderr, maxOutputBytes))
	}
	if len(bytes.TrimSpace(stdout)) > 0 {
		return fmt.Sprintf("%q exited with %s (stdout): %s", name, state, tail(stdout, maxOutputBytes))
	}
	return fmt.Sprintf("%q exited with %s (no output)", name, state)
}

// execRun is the default runner: it runs name+args in dir (empty = inherit)
// via exec.CommandContext, feeding stdin to the process and returning both
// streams on success. A non-zero exit yields an error carrying the process's
// own explanation via describeExit; an exec error (e.g. executable not found)
// is reported with an actionable message naming the binary.
func execRun(ctx context.Context, dir, name string, args []string, stdin string) (string, string, error) {
	var env []string
	if full, ok := ctx.Value(fullEnvKey{}).([]string); ok {
		return execRunEnv(ctx, dir, name, args, stdin, full)
	}
	if extra, _ := ctx.Value(envKey{}).([]string); len(extra) > 0 {
		env = append(os.Environ(), extra...)
	}
	return execRunEnv(ctx, dir, name, args, stdin, env)
}

// agentRun is the runner of the agentic provider (F-0260): same exec, plus two
// stamps the child inherits. CERNE_ACTOR=agent attributes every cerne command
// the agent runs to an agent in the event log, and app.CheckHumanGate keeps
// refusing approve/accept from it — the human gates (F-0131) are untouched by
// the agent gaining write tools. DenyLifecycleEnv makes the binary refuse every
// lifecycle verb (B-0040). os.Environ() comes first, then the context extras
// (claudeNoTitleEnv, F-0505), and the stamps last, so they win over an inherited
// value or an extra (exec keeps the last of a duplicated key).
func agentRun(ctx context.Context, dir, name string, args []string, stdin string) (string, string, error) {
	extra, _ := ctx.Value(envKey{}).([]string)
	env := append(append(os.Environ(), extra...), "CERNE_ACTOR=agent", DenyLifecycleEnv+"=1")
	return execRunEnv(ctx, dir, name, args, stdin, env)
}

// DenyLifecycleEnv is the second stamp of agentRun: while it is present the
// cerne binary refuses every lifecycle verb (B-0040), on the resolved command,
// so a spelling the harness prefix rules miss (`cerne --verbose feature
// supersede`) is refused all the same. CERNE_ACTOR=agent cannot carry this: a
// coding agent in a terminal is an agent too, and it does run
// start/complete/supersede.
const DenyLifecycleEnv = "CERNE_DENY_LIFECYCLE"

// execRunEnv is execRun with an explicit child environment; nil means inherit
// the server's own, which is what every non-agentic path wants (N-0011: an API
// proxy configured in the shell must keep applying transparently).
func execRunEnv(ctx context.Context, dir, name string, args []string, stdin string, env []string) (string, string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = env
	// A prompt is built from project files of any encoding; a strict reader
	// (codex exec) refuses one invalid byte, so coerce here, once, for every CLI (B-0005).
	cmd.Stdin = strings.NewReader(strings.ToValidUTF8(stdin, "�"))
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
	return "", "", fmt.Errorf("could not run %q (install %s and ensure it is on PATH): %w", name, cliProduct(name), err)
}

// cliProduct names the product behind a subscription CLI binary, for the
// "install X" hint; anything unrecognized is Claude Code (the original).
func cliProduct(bin string) string {
	b := strings.ToLower(filepath.Base(bin))
	switch {
	case strings.Contains(b, "codex"):
		return "Codex CLI"
	case strings.Contains(b, "gemini"):
		return "Gemini CLI"
	case strings.Contains(b, "cursor"):
		return "Cursor CLI"
	case strings.Contains(b, "copilot"):
		return "GitHub Copilot CLI"
	}
	return "Claude Code"
}

// cliProvider is a Provider that performs one completion per call by shelling to
// the Claude Code CLI in headless mode. It carries no API key: authentication is
// the user's Claude Code subscription (claude /login or CLAUDE_CODE_OAUTH_TOKEN).
// With agentTools set (F-0224) the CLI runs as a read-only agent: it may chain
// the allowlisted cerne commands (executed in dir, the project root) before
// answering; nil agentTools is the original one-shot text generator, byte-for-
// byte, used by ask/compile/diagram.
type cliProvider struct {
	kind        string // ModeClaudeCLI (also ""), ModeCodexCLI, ModeGeminiCLI, ModeCursorCLI or ModeCopilotCLI
	model       string // -m for codex/gemini; --model on the claude chat turn only (B-0007)
	effort      string // codex model_reasoning_effort / claude --effort for one chat turn (B-0006, B-0007)
	bin         string
	run         runner
	dir         string   // working directory for the CLI (project root in agentic mode)
	agentTools  []string // Claude Code permission rules passed via --allowedTools; nil = one-shot
	deniedTools []string // permission rules passed via --disallowedTools (secret-file denials, F-0258)
	answered    string   // the model claude reported for the last agentic turn (F-0440)
}

// AnsweredModel is the model the CLI itself reported for the last turn, "" when
// it reported none (F-0440): what the chat shows as the model that really ran.
func (p *cliProvider) AnsweredModel() string { return p.answered }

// AnsweredEffort is the effort the CLI is given on its argv, "" when none was
// valid (B-0007): what the chat may truthfully report.
func (p *cliProvider) AnsweredEffort() string { return p.effort }

// claudeJSON is the part of `claude -p --output-format json` cerne reads.
type claudeJSON struct {
	Type       string `json:"type"`
	Result     string `json:"result"`
	ModelUsage map[string]struct {
		InputTokens         int `json:"inputTokens"`
		OutputTokens        int `json:"outputTokens"`
		CacheReadTokens     int `json:"cacheReadInputTokens"`
		CacheCreationTokens int `json:"cacheCreationInputTokens"`
	} `json:"modelUsage"`
}

// parseClaudeJSON reads the reply and the usage of a JSON result — Model is the
// busiest model, the tokens are summed over every model; ok is false for
// anything else (a CLI that ignored the format), kept as plain text.
func parseClaudeJSON(out string) (reply string, u Usage, ok bool) {
	var r claudeJSON
	if json.Unmarshal([]byte(out), &r) != nil || r.Type != "result" {
		return "", Usage{}, false
	}
	best := -1
	for m, mu := range r.ModelUsage {
		u.InputTokens += mu.InputTokens + mu.CacheReadTokens + mu.CacheCreationTokens
		u.OutputTokens += mu.OutputTokens
		if mu.OutputTokens > best || (mu.OutputTokens == best && m < u.Model) {
			best, u.Model = mu.OutputTokens, m
		}
	}
	return r.Result, u, true
}

// oneShotIsolation are the flags that turn `claude -p` back into what the
// non-agentic callers (compile, ask, the draft builders) actually ask for: one
// completion, from the prompt alone. Without them the CLI boots a full Claude
// Code session in the cwd — inheriting the user's CLAUDE.md, hooks, plugins and
// MCP servers, and holding the tools to act on them. F-0287 has the receipt: a
// wiki page synthesis wrote the page to a scratchpad file and returned a report
// about having done so, which cerne then stamped as the page.
//
//   - --safe-mode disables every customization (CLAUDE.md, hooks, plugins,
//     skills, MCP, custom agents) while leaving auth, model and the built-in
//     tools working. That last part is why it is used instead of --bare, which
//     would also drop the OAuth subscription and demand ANTHROPIC_API_KEY.
//   - --tools "" removes the built-in tools, so the synthesis cannot touch the
//     filesystem at all. Every one-shot prompt is self-contained by
//     construction, so there is nothing for a tool to fetch.
//
// Measured on claude 2.1.251 (macOS): a trivial prompt in a project directory
// went from 33.0s to 8.0s. The cwd stays the project's: with CLAUDE.md
// discovery off and no tools there is nothing left to inherit from it, and
// moving it would drop the shell environment the project relies on (N-0011).
//
// This is deliberately NOT applied in agentic mode: the Backlog agent (F-0224)
// runs in the project on purpose, and its tools ARE the feature.
var oneShotIsolation = []string{"--safe-mode", "--tools", ""}

// claudeNoTitleEnv goes in the environment of every `claude -p` child, one-shot
// and agentic (F-0505). Without it the CLI fires a second API request next to
// each call to title the session, resending the whole prompt uncached on the
// same model, and modelUsage counts both: half of the 530,009 input tokens of
// the F-0502 live proof (N-0150). An environment variable, not --name, because
// an old CLI ignores an unknown variable but rejects an unknown flag, and the
// agentic turn never retries with less isolation.
const claudeNoTitleEnv = "CLAUDE_CODE_DISABLE_TERMINAL_TITLE=1"

// isUnknownFlag reports whether err looks like the CLI rejecting a flag it
// does not know — an old Claude Code without --allowedTools. Covers the
// cobra ("unknown flag") and commander ("unknown option") phrasings.
// rejectedFlag reads the argument errors of the Codex and Gemini CLIs (clap and
// yargs word them differently from Claude Code's).
func rejectedFlag(err error) bool {
	msg := err.Error()
	return isUnknownFlag(err) || strings.Contains(msg, "unexpected argument") || strings.Contains(msg, "wasn't expected") || strings.Contains(msg, "unrecognized") || strings.Contains(msg, "Unknown argument")
}

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
// require an API key). --model/--effort go only on the agentic chat turn, from
// the composer (B-0007); the one-shot never forces a model. Any failure becomes
// an actionable ErrClaudeCLI.
//
// In agentic mode (agentTools set, F-0224) the allowlist travels as one
// comma-joined --allowedTools argument, next to the isolation flags; otherwise
// the call is isolated with oneShotIsolation. A CLI too old to know the
// one-shot flags gets one retry in plain headless form; the agentic turn fails
// instead (never less isolation). Whatever the CLI printed on stderr — the
// stream where headless permission denials surface — is echoed to the server
// log, so an attempt outside the allowlist is visible (criterion 4).
func (p *cliProvider) Complete(ctx context.Context, prompt string) (string, error) {
	switch p.kind {
	case ModeCodexCLI:
		return p.completeCodex(ctx, prompt)
	case ModeGeminiCLI:
		return p.completeGemini(ctx, prompt)
	case ModeCursorCLI:
		return p.completeCursor(ctx, prompt)
	case ModeCopilotCLI:
		return p.completeCopilot(ctx, prompt)
	}
	s, _, err := p.completeClaude(ctx, prompt, false)
	return s, err
}

// CompleteUsage is Complete that also returns the tokens the call used
// (F-0502). Usage travels with the call, not in the provider: a provider is
// shared between goroutines. Only the claude CLI reports it, and asks for JSON
// output to do so; the other CLIs answer with a zero Usage.
func (p *cliProvider) CompleteUsage(ctx context.Context, prompt string) (string, Usage, error) {
	if p.kind != "" {
		s, err := p.Complete(ctx, prompt)
		return s, Usage{}, err
	}
	return p.completeClaude(ctx, prompt, true)
}

// completeClaude is the `claude -p` call; wantUsage turns the output to JSON
// so the tokens can be read.
func (p *cliProvider) completeClaude(ctx context.Context, prompt string, wantUsage bool) (string, Usage, error) {
	base := []string{"-p", "--output-format", "text"}
	args := slices.Clone(base)
	agentic := len(p.agentTools) > 0
	if wantUsage {
		args[2] = "json" // F-0502: the result carries the tokens (a copy; base stays text for the old-CLI retry)
	}
	if agentic {
		args[2] = "json" // F-0440: the result carries the model that ran (a copy; base stays text for the old-CLI retry)
		// B-0008: keep the project's CLAUDE.md/skills/hooks, drop the user's
		// (hooks, plugins, ~/.claude/CLAUDE.md). --strict-mcp-config with no
		// --mcp-config drops EVERY MCP server, user and project (.mcp.json)
		// alike, on purpose: the chat reaches cerne through Bash, and its
		// allowlist has no mcp__* entry. Not --safe-mode: the analyst relies on
		// the project's own context.
		args = append(args, "--setting-sources", "project,local", "--strict-mcp-config",
			"--allowedTools", strings.Join(p.agentTools, ","))
		if len(p.deniedTools) > 0 {
			args = append(args, "--disallowedTools", strings.Join(p.deniedTools, ","))
		}
		if p.model != "" {
			args = append(args, "--model", p.model)
		}
		if p.effort != "" {
			args = append(args, "--effort", p.effort)
		}
		// F-0520: the attached images live in the project cache, outside the
		// working dir; Read opens them by the path the prompt names.
		for _, d := range imageDirs(imagesFrom(ctx)) {
			args = append(args, "--add-dir", d)
		}
	} else {
		args = append(args, oneShotIsolation...)
	}
	extra, _ := ctx.Value(envKey{}).([]string)
	ctx = context.WithValue(ctx, envKey{}, append(slices.Clone(extra), claudeNoTitleEnv))
	start := time.Now()
	out, stderr, err := p.run(ctx, p.dir, p.bin, args, prompt)
	if err != nil && isUnknownFlag(err) {
		if agentic {
			// Never retry with less isolation (B-0008, F-0414 rule).
			return "", Usage{}, fmt.Errorf("%w: this Claude Code does not know a flag cerne needs (isolation, model or effort) — update Claude Code (claude update); cerne will not run the chat with less isolation: %w", ErrClaudeCLI, err)
		}
		out, stderr, err = p.run(ctx, p.dir, p.bin, base, prompt)
	}
	if err != nil {
		return "", Usage{}, cliFailure(ctx, start, ErrClaudeCLI, "Claude Code (claude -p)", err, "check 'claude /login' (subscription) or use [llm] mode=\"byo-key\"")
	}
	if s := strings.TrimSpace(stderr); agentic && s != "" {
		log.Printf("claude-cli stderr: %s", truncate([]byte(s), maxOutputBytes))
	}
	if agentic || wantUsage {
		if reply, u, ok := parseClaudeJSON(out); ok {
			if agentic {
				p.answered = u.Model
			}
			return strings.TrimSpace(reply), u, nil
		}
	}
	return strings.TrimSpace(out), Usage{}, nil
}

// scratchDir is the empty directory the Codex and Gemini one-shots run in, so
// the CLI inherits nothing from the project (no AGENTS.md, no GEMINI.md).
func scratchDir() (string, func(), error) {
	dir, err := os.MkdirTemp("", "cerne-cli-")
	if err != nil {
		return "", nil, err
	}
	return dir, func() { os.RemoveAll(dir) }, nil
}

// cerneReadTools are the read-only tools of `cerne mcp` the Codex chat turn
// approves; a test keeps the list equal to mcp.ToolNames.
var cerneReadTools = []string{"status", "brief", "next", "search", "show", "context", "ask", "guide"}

// cerneEnvVars are the locations a fixture or a user points cerne at.
var cerneEnvVars = []string{"CERNE_CACHE_DIR", "CERNE_DATA_DIR", "CERNE_CONFIG_DIR"}

// completeCodex runs `codex exec` (R-0093): prompt on stdin ("-"), the final
// message written by the CLI to the -o file, read-only sandbox, no session
// persisted, in an empty scratch dir.
func (p *cliProvider) completeCodex(ctx context.Context, prompt string) (string, error) {
	dir, cleanup, err := scratchDir()
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrCodexCLI, err)
	}
	defer cleanup()
	outFile := filepath.Join(dir, "answer.txt")
	// Isolation (F-0414, R-0096): the CLI gets no shell, no connectors, no hosted
	// search, no user config or rules, and no AGENTS.md — an indexed document
	// that tries to steer it has nothing to act with.
	args := []string{"exec", "--skip-git-repo-check", "--ephemeral", "--sandbox", "read-only",
		"--ignore-user-config", "--ignore-rules", "--disable", "shell_tool", "--disable", "apps",
		"-c", `web_search="disabled"`, "-c", "project_doc_max_bytes=0", "-o", outFile}
	if p.dir != "" {
		bin, err := os.Executable()
		if err != nil {
			return "", err
		}
		root, err := filepath.Abs(p.dir)
		if err != nil {
			return "", err
		}
		command, _ := json.Marshal(bin)
		argv, _ := json.Marshal([]string{"mcp", "--project-files", "--dir", root})
		args = append(args, "-c", "mcp_servers.cerne_project.command="+string(command), "-c", "mcp_servers.cerne_project.args="+string(argv),
			"-c", `mcp_servers.cerne_project.tools.find_files.approval_mode="approve"`,
			"-c", `mcp_servers.cerne_project.tools.read_file.approval_mode="approve"`,
			"-c", `mcp_servers.cerne_project.tools.search_files.approval_mode="approve"`)
		// F-0439: cerne's own read tools (status, search, show…) beside the
		// files. Every name in mcp.ToolNames is a read command; none writes.
		cerneArgv, _ := json.Marshal([]string{"mcp", "--dir", root})
		args = append(args, "-c", "mcp_servers.cerne.command="+string(command), "-c", "mcp_servers.cerne.args="+string(cerneArgv))
		for _, name := range cerneReadTools {
			args = append(args, "-c", "mcp_servers.cerne.tools."+name+`.approval_mode="approve"`)
		}
		// Codex starts a stdio server with a restricted environment: name the
		// cerne locations that are set, so the children read this project's cache.
		var set []string
		for _, n := range cerneEnvVars {
			if os.Getenv(n) != "" {
				set = append(set, n)
			}
		}
		if len(set) > 0 {
			names, _ := json.Marshal(set)
			args = append(args, "-c", "mcp_servers.cerne_project.env_vars="+string(names), "-c", "mcp_servers.cerne.env_vars="+string(names))
		}
	}
	if p.model != "" {
		args = append(args, "-m", p.model)
	}
	// The effort comes from the browser: only a bare word reaches the TOML
	// override, so it can never close the string and set another key.
	if effortRE.MatchString(p.effort) {
		args = append(args, "-c", `model_reasoning_effort="`+p.effort+`"`)
	}
	// F-0520: one -i per image; -i takes several files, so the stdin marker
	// goes after a "--" or it would be read as one more image.
	imgs := imagesFrom(ctx)
	for _, img := range imgs {
		args = append(args, "-i", img)
	}
	if len(imgs) > 0 {
		args = append(args, "--")
	}
	args = append(args, "-")
	start := time.Now()
	if _, _, err := p.run(ctx, dir, p.bin, args, prompt); err != nil {
		if rejectedFlag(err) {
			return "", fmt.Errorf("%w: this Codex CLI does not know an isolation flag cerne needs — update it (npm i -g @openai/codex); cerne will not run it with less isolation: %w", ErrCodexCLI, err)
		}
		hint := "use another mode"
		switch {
		case strings.Contains(err.Error(), "install Codex CLI"):
			hint = "install Codex CLI and ensure it is on PATH"
		case authErrorRE.MatchString(err.Error()):
			hint = "run 'codex login' (subscription) or use another mode"
		}
		return "", cliFailure(ctx, start, ErrCodexCLI, "Codex CLI (codex exec)", err, hint)
	}
	b, err := os.ReadFile(outFile)
	if err != nil || len(bytes.TrimSpace(b)) == 0 {
		return "", fmt.Errorf("%w: codex exec returned no answer — run 'codex login' or check the Codex CLI version", ErrCodexCLI)
	}
	// The scratch dir is gone after the turn: a path the model wrote under it
	// becomes the project-relative path the files tools return (F-0439).
	prefixes := []string{dir}
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		prefixes = append(prefixes, real)
	}
	reply := stripTurnDir(string(b), prefixes...)
	return strings.TrimSpace(reply), nil
}

// stripTurnDir removes each dir + "/" prefix from reply, longest dir first: the
// resolved /private/var/... form contains the /var/... one, so the short form
// first would leave "/private" glued to the relative path.
func stripTurnDir(reply string, dirs ...string) string {
	dirs = slices.Clone(dirs)
	slices.SortFunc(dirs, func(a, b string) int { return len(b) - len(a) })
	for _, d := range dirs {
		reply = strings.ReplaceAll(reply, d+"/", "")
	}
	return reply
}

// authErrorRE recognizes a CLI error that a login fixes.
var authErrorRE = regexp.MustCompile(`(?i)401|unauthori|log(ged)? ?in|sign(ed)? ?in|not authenticated|auth`)

// claudeModelRE is a claude model id or alias (claude-<family>-<version>, opus,
// sonnet[1m]): a leftover id of another vendor (<other-vendor-model>) or a leading dash never
// reaches `--model` (B-0007).
var claudeModelRE = regexp.MustCompile(`^(claude-[A-Za-z0-9._-]+|opus|sonnet|haiku)(\[[0-9a-z]+\])?$`)

// claudeEfforts are the levels `claude --effort` accepts (claude --help).
var claudeEfforts = []string{"low", "medium", "high", "xhigh", "max"}

// effortRE is the shape of a reasoning effort level (low, medium, xhigh…).
var effortRE = regexp.MustCompile(`^[a-z]+$`)

var exitStatusRE = regexp.MustCompile(`exit status (\d+)`)

// cursorDenyAll is the workspace .cursor/cli.json of a one-shot Cursor call
// (F-0441, R-0098). The Cursor CLI has no locked headless mode (R-0099), so
// every tool is denied by permission: deny wins over allow and holds in print
// mode. The CLI reads this file itself; the Read deny only binds its tools.
const cursorDenyAll = `{"permissions":{"allow":[],"deny":["Shell(*)","Read(**)","Write(**)","WebFetch(*)","Mcp(*:*)"]}}`

// completeCursor runs `cursor-agent -p` (F-0441): prompt on stdin, text output,
// in an empty scratch dir that is also its trusted workspace. Isolation is
// two layers, each verified alone against the real CLI: ask mode (read-only)
// and the deny-all permissions file, with the sandbox on top. The user's own
// Cursor rules, hooks and plugins still load: the CLI cannot switch them off.
func (p *cliProvider) completeCursor(ctx context.Context, prompt string) (string, error) {
	dir, cleanup, err := scratchDir()
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrCursorCLI, err)
	}
	defer cleanup()
	if err := os.MkdirAll(filepath.Join(dir, ".cursor"), 0o700); err != nil {
		return "", fmt.Errorf("%w: %w", ErrCursorCLI, err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".cursor", "cli.json"), []byte(cursorDenyAll), 0o600); err != nil {
		return "", fmt.Errorf("%w: %w", ErrCursorCLI, err)
	}
	args := []string{"-p", "--output-format", "text", "--trust", "--mode", "ask", "--sandbox", "enabled", "--workspace", dir}
	if p.model != "" {
		args = append(args, "--model", p.model)
	}
	start := time.Now()
	out, _, err := p.run(ctx, dir, p.bin, args, prompt)
	if err != nil {
		if rejectedFlag(err) {
			return "", fmt.Errorf("%w: this Cursor CLI does not know an isolation flag cerne needs — update it (cursor-agent update); cerne will not run it with less isolation: %w", ErrCursorCLI, err)
		}
		hint := "use another mode"
		switch {
		case strings.Contains(err.Error(), "install Cursor CLI"):
			hint = "install Cursor CLI (curl https://cursor.com/install -fsS | bash) and ensure cursor-agent is on PATH"
		case authErrorRE.MatchString(err.Error()):
			hint = "run 'cursor-agent login' (subscription) or set CURSOR_API_KEY, or use another mode"
		}
		return "", cliFailure(ctx, start, ErrCursorCLI, "Cursor CLI (cursor-agent -p)", err, hint)
	}
	answer := strings.TrimSpace(out)
	if answer == "" {
		return "", fmt.Errorf("%w: cursor-agent returned no answer — check 'cursor-agent status' (login), the model name and the Cursor CLI version", ErrCursorCLI)
	}
	return answer, nil
}

// copilotEvent is one line of `copilot --output-format json` (JSONL), the part
// cerne reads (F-0445, probed on copilot 1.0.112).
type copilotEvent struct {
	Type string `json:"type"`
	Data struct {
		Model   string `json:"model"`
		Phase   string `json:"phase"`
		Content string `json:"content"`
		Message string `json:"message"`
	} `json:"data"`
}

func copilotEventsOf(out string) []copilotEvent {
	var evs []copilotEvent
	for _, line := range strings.Split(out, "\n") {
		var ev copilotEvent
		if strings.HasPrefix(line, "{") && json.Unmarshal([]byte(line), &ev) == nil {
			evs = append(evs, ev)
		}
	}
	return evs
}

// copilotSessionError is the message of a session.error event on a failed
// call's stdout: in JSON mode the CLI reports a bad model there, ahead of
// events that would push it out of the 512-byte tail describeExit keeps.
func copilotSessionError(stdout []byte) string {
	var msgs []string
	for _, ev := range copilotEventsOf(string(stdout)) {
		if ev.Type == "session.error" && ev.Data.Message != "" {
			msgs = append(msgs, ev.Data.Message)
		}
	}
	return strings.Join(msgs, "; ")
}

// copilotGOOS and copilotLookEnv are the platform and the environment the
// login hint reads; tests replace them.
var (
	copilotGOOS    = runtime.GOOS
	copilotLookEnv = os.LookupEnv
)

// fullEnvKey carries the complete child environment: unlike envKey it replaces
// the inherited one instead of adding to it.
type fullEnvKey struct{}

// copilotEnv is the child environment of a one-shot call: the parent's, minus
// every COPILOT_* variable but the auth token (COPILOT_ALLOW_ALL, extra skill
// or agent dirs, a user COPILOT_HOME…), plus the throwaway COPILOT_HOME.
func copilotEnv(environ []string, home string) []string {
	out := make([]string, 0, len(environ)+1)
	for _, e := range environ {
		if strings.HasPrefix(e, "COPILOT_") && !strings.HasPrefix(e, "COPILOT_GITHUB_TOKEN=") {
			continue
		}
		out = append(out, e)
	}
	return append(out, "COPILOT_HOME="+home)
}

// copilotModelRE is the shape of a model id: it cannot start with a dash, so a
// value from the browser never becomes another flag.
var copilotModelRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]*$`)

// copilotNoTools is an --available-tools name no tool has. An empty list does
// not clear the tool list (probe, F-0445); a list of unknown names does.
const copilotNoTools = "cerne-no-tools"

// completeCopilot runs `copilot` non-interactively (F-0445): prompt on stdin
// (`-p -` sends a literal "-"), JSON events so the answer and the model that
// ran are read from the stream, in an empty scratch dir and with a throwaway
// COPILOT_HOME so no skill, agent, hook or MCP server of the user loads (the
// keychain login still does, probed). Isolation: every tool denied and hidden,
// no built-in MCP, no custom instruction, the folder never trusted. Denying
// alone leaked a canary through a subagent once the main tools were hidden, so
// the tool list itself is emptied.
func (p *cliProvider) completeCopilot(ctx context.Context, prompt string) (string, error) {
	dir, cleanup, err := scratchDir()
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrCopilotCLI, err)
	}
	defer cleanup()
	work, home := filepath.Join(dir, "work"), filepath.Join(dir, "home")
	for _, d := range []string{work, home} {
		if err := os.Mkdir(d, 0o700); err != nil {
			return "", fmt.Errorf("%w: %w", ErrCopilotCLI, err)
		}
	}
	ctx = context.WithValue(ctx, fullEnvKey{}, copilotEnv(os.Environ(), home))
	args := []string{"-s", "--output-format", "json", "--no-ask-user", "--no-auto-update", "--no-custom-instructions", "--disable-builtin-mcps",
		"--deny-tool", "shell", "--deny-tool", "write", "--deny-tool", "url", "--deny-tool", "read", "--deny-tool", "memory",
		"--excluded-tools", "web_fetch", "--available-tools", copilotNoTools}
	if p.model != "" {
		if !copilotModelRE.MatchString(p.model) {
			return "", fmt.Errorf("%w: %q is not a model id — choose a model from your Copilot plan", ErrCopilotCLI, p.model)
		}
		args = append(args, "--model", p.model)
	}
	if effortRE.MatchString(p.effort) {
		args = append(args, "--effort", p.effort)
	}
	for _, img := range imagesFrom(ctx) { // F-0520
		args = append(args, "--attachment", img)
	}
	start := time.Now()
	out, _, err := p.run(ctx, work, p.bin, args, prompt)
	if err != nil {
		if strings.Contains(err.Error(), "invalid value") {
			return "", fmt.Errorf("%w: the Copilot CLI refused a value cerne passed (check the model and effort in the AI settings): %w", ErrCopilotCLI, err)
		}
		if rejectedFlag(err) {
			return "", fmt.Errorf("%w: this GitHub Copilot CLI does not know an isolation flag cerne needs — update it (npm install -g @github/copilot); cerne will not run it with less isolation: %w", ErrCopilotCLI, err)
		}
		return "", cliFailure(ctx, start, ErrCopilotCLI, "GitHub Copilot CLI (copilot)", err, copilotHint(err.Error()))
	}
	// Prefer the last final_answer; without one, the last non-empty message.
	answer, model, final := "", "", false
	sessionErr := ""
	for _, ev := range copilotEventsOf(out) {
		switch {
		case ev.Type == "session.error" && ev.Data.Message != "":
			sessionErr = ev.Data.Message
		case ev.Type == "assistant.message" && strings.TrimSpace(ev.Data.Content) != "":
			isFinal := ev.Data.Phase == "final_answer"
			if isFinal || !final {
				answer, model, final = ev.Data.Content, ev.Data.Model, isFinal
			}
		}
	}
	if strings.TrimSpace(answer) == "" {
		if sessionErr != "" {
			return "", cliFailure(ctx, start, ErrCopilotCLI, "GitHub Copilot CLI (copilot)", errors.New(sessionErr), copilotHint(sessionErr))
		}
		return "", fmt.Errorf("%w: copilot returned no answer — run 'copilot login', check --model (the model must be available to your plan) and the Copilot CLI version", ErrCopilotCLI)
	}
	p.answered = model
	return strings.TrimSpace(answer), nil
}

// copilotHint names the fix for a failed call, by cause. Order matters: a
// policy refusal can mention being unauthorized or unavailable, so it comes
// before the login and the model.
func copilotHint(msg string) string {
	switch {
	case strings.Contains(msg, "install GitHub Copilot CLI"):
		return "install GitHub Copilot CLI (npm install -g @github/copilot) and ensure copilot is on PATH"
	case copilotPolicyRE.MatchString(msg):
		return "your organization's policy blocks it — ask your Copilot administrator, or use another mode"
	case authErrorRE.MatchString(msg):
		if copilotGOOS != "darwin" && !copilotHasToken() {
			return "set COPILOT_GITHUB_TOKEN (cerne runs Copilot with a throwaway COPILOT_HOME, which hides the stored login on this platform), or use another mode"
		}
		return "run 'copilot login' or set COPILOT_GITHUB_TOKEN, or use another mode"
	case copilotModelRefusedRE.MatchString(msg):
		return "choose another model — this one is not available to your Copilot plan or organization"
	}
	return "use another mode"
}

func copilotHasToken() bool {
	for _, k := range []string{"COPILOT_GITHUB_TOKEN", "GH_TOKEN", "GITHUB_TOKEN"} {
		if v, ok := copilotLookEnv(k); ok && v != "" {
			return true
		}
	}
	return false
}

// copilotModelRefusedRE is the CLI's refusal of a model: Model "<id>" is not available.
var copilotModelRefusedRE = regexp.MustCompile(`Model "[^"]*" is not available`)

// copilotPolicyRE recognizes a refusal by the organization's Copilot policy. The
// wording is from GitHub's documentation: no probe could cause it.
var copilotPolicyRE = regexp.MustCompile(`(?i)polic(y|ies)|organi[sz]ation|administrator`)

// geminiReply is the part of `gemini --output-format json` cerne reads.
type geminiReply struct {
	Response string `json:"response"`
	Error    *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// completeGemini runs `gemini` headless (R-0094): prompt on stdin, JSON
// output, plan approval mode (read-only), in an empty scratch dir. Exit 42 is
// invalid input; any other failure is read as an API/login problem.
func (p *cliProvider) completeGemini(ctx context.Context, prompt string) (string, error) {
	dir, cleanup, err := scratchDir()
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrGeminiCLI, err)
	}
	defer cleanup()
	// Isolation (F-0414, R-0094): the system settings file overrides every other
	// one, and leaves the CLI no tool, no MCP server and no context file; -e none
	// turns the extensions off.
	settings := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(settings, []byte(geminiLockedSettings()), 0o600); err != nil {
		return "", fmt.Errorf("%w: %w", ErrGeminiCLI, err)
	}
	ctx = context.WithValue(ctx, envKey{}, []string{"GEMINI_CLI_SYSTEM_SETTINGS_PATH=" + settings})
	args := []string{"--output-format", "json", "--approval-mode", "plan", "-e", "none"}
	if p.model != "" {
		args = append(args, "-m", p.model)
	}
	start := time.Now()
	out, _, err := p.run(ctx, dir, p.bin, args, prompt)
	if err != nil {
		if rejectedFlag(err) {
			return "", fmt.Errorf("%w: this Gemini CLI does not know an isolation flag cerne needs — update it (npm i -g @google/gemini-cli); cerne will not run it with less isolation: %w", ErrGeminiCLI, err)
		}
		hint := "run gemini and sign in (or set GEMINI_API_KEY), then retry"
		if strings.Contains(err.Error(), "install Gemini CLI") {
			hint = "install Gemini CLI and ensure it is on PATH"
		} else if m := exitStatusRE.FindStringSubmatch(err.Error()); m != nil && m[1] == "42" {
			hint = "invalid input: the prompt or the model name was rejected"
		}
		return "", cliFailure(ctx, start, ErrGeminiCLI, "Gemini CLI", err, hint)
	}
	var r geminiReply
	raw := strings.TrimSpace(out)
	if i, j := strings.Index(raw, "{"), strings.LastIndex(raw, "}"); i >= 0 && j > i {
		raw = raw[i : j+1] // tolerate warnings printed around the JSON
	}
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		return "", fmt.Errorf("%w: unreadable gemini output (expected JSON with a response field)", ErrGeminiCLI)
	}
	if r.Error != nil && r.Error.Message != "" {
		return "", fmt.Errorf("%w: %s", ErrGeminiCLI, r.Error.Message)
	}
	answer := strings.TrimSpace(r.Response)
	if answer == "" {
		return "", fmt.Errorf("%w: gemini returned no answer — check the Gemini CLI version and that it is signed in", ErrGeminiCLI)
	}
	return answer, nil
}

// geminiBuiltinTools are the built-in tool names the locked settings exclude.
var geminiBuiltinTools = []string{"read_file", "read_many_files", "glob", "search_file_content", "list_directory",
	"run_shell_command", "web_fetch", "google_web_search", "save_memory", "write_file", "replace"}

// geminiLockedSettings is the system settings.json of a one-shot call. The
// core allowlist holds only a name no tool has; exclude names every built-in
// as well, so a name this list misses in a newer CLI is still not allowed.
func geminiLockedSettings() string {
	b, _ := json.Marshal(map[string]any{
		"mcpServers": map[string]any{},
		"mcp":        map[string]any{"allowed": []string{"__cerne_none__"}},
		"tools":      map[string]any{"core": []string{"__cerne_no_tool__"}, "exclude": geminiBuiltinTools},
		"context":    map[string]any{"fileName": []string{"__cerne_none__.md"}},
	})
	return string(b)
}

// envKey carries extra child environment (KEY=VALUE) from a provider to the
// runner through the context, so the runner signature stays what every fake
// in the tests already implements.
type envKey struct{}
