package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/cost"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
)

// Hook plumbing subcommands (F-0151). Both read the Claude Code hook JSON from
// stdin and are wired by `cerne hook install` (never typed by a user, so they
// stay hidden). Both are deliberately silent and always exit 0 on anything
// unexpected: a hook that errors or blocks on infra breaks every tool call in
// the host session — fail-open is the only acceptable failure mode. The gate's
// on/off switch ([gates] work_gate) is evaluated here at run time, not at
// install time, so toggling it needs no reinstall.

// postToolContext is the PostToolUse additionalContext envelope.
type postToolContext struct {
	HookSpecificOutput struct {
		HookEventName     string `json:"hookEventName"`
		AdditionalContext string `json:"additionalContext"`
	} `json:"hookSpecificOutput"`
}

// hookInput is the common envelope of a PreToolUse/PostToolUse hook payload.
type hookInput struct {
	SessionID    string          `json:"session_id"`
	CWD          string          `json:"cwd"`
	ToolName     string          `json:"tool_name"`
	ToolInput    json.RawMessage `json:"tool_input"`
	ToolResponse json.RawMessage `json:"tool_response"`
	// ToolResult is Copilot's name for the tool response (F-0444).
	ToolResult json.RawMessage `json:"tool_result"`
	// AgentID is present only when the hook fires inside a subagent (whose
	// session_id is still the parent's); TranscriptPath is the session's
	// JSONL (F-0309).
	AgentID        string `json:"agent_id"`
	TranscriptPath string `json:"transcript_path"`
	// StopHookActive is set on a Stop payload when the stop is itself the
	// continuation of a Stop hook (F-0294).
	StopHookActive bool `json:"stop_hook_active"`
	// Prompt is the user's text on a UserPromptSubmit payload (B-0020).
	Prompt string `json:"prompt"`
	// Source is the SessionStart origin; "compact" after a compaction (F-0368).
	Source string `json:"source"`
	// PermissionMode is the session's mode on every turn and tool event
	// (R-0034; not on SessionStart): what decides whether an idle session
	// takes a cross-session message (F-0491).
	PermissionMode string `json:"permission_mode"`

	// Cursor is set when Cursor sent the payload (F-0428).
	Cursor *cursorInput `json:"-"`
}

// cursorInput holds the fields of a Cursor payload — Cursor runs the
// project's .claude hooks through its third-party import, with its own
// payload, marked by cursor_version (F-0428). They are decoded in a second
// pass so that another harness sending a key of the same name with another
// type never turns its hook into a decode error.
type cursorInput struct {
	CursorVersion  string          `json:"cursor_version"`
	ToolOutput     json.RawMessage `json:"tool_output"`
	WorkspaceRoots []string        `json:"workspace_roots"`
	ConversationID string          `json:"conversation_id"`
	LoopCount      int             `json:"loop_count"`
	Status         string          `json:"status"`
	// Model and ModelID name the session's model on every event (F-0513):
	// model_id is the canonical id, model the mode variant on sessionStart.
	Model   string `json:"model"`
	ModelID string `json:"model_id"`
}

// cursor reports a payload delivered by Cursor, which is answered in Cursor's
// native envelope whatever --format says (F-0428).
func (in hookInput) cursor() bool { return in.Cursor != nil }

// recordSessionMode keeps the session's last permission mode (F-0491): the
// channel wakes an idle session only in a mode that takes a cross-session
// message. Fail-open.
func recordSessionMode(cacheDir string, in hookInput) {
	harness := cost.CurrentHarness()
	if in.cursor() {
		harness = "cursor"
	}
	_ = app.RecordSessionMode(cacheDir, in.SessionID, in.AgentID, harness, in.PermissionMode)
}

// recordCursorSession keeps the model a Cursor payload names for its session
// (F-0513), in the cache of the project the payload's workspace root names:
// model_id over model. Called by the decode, so every hook subcommand that
// reads a Cursor payload records it. No project, no record (an empty cwd would
// climb from the hook process's own folder). Fail-open.
func recordCursorSession(in hookInput) {
	if in.CWD == "" {
		return
	}
	cfg, _, err := project.LoadConfig(in.CWD)
	if err != nil {
		return
	}
	cacheDir, err := project.CacheDir(cfg.ProjectID)
	if err != nil {
		return
	}
	model := in.Cursor.ModelID
	if model == "" {
		model = in.Cursor.Model
	}
	_ = app.RecordCursorSession(cacheDir, in.SessionID, model, in.TranscriptPath)
}

func decodeHookInput(r io.Reader) (hookInput, error) {
	var raw json.RawMessage
	if err := json.NewDecoder(r).Decode(&raw); err != nil {
		return hookInput{}, err
	}
	var in hookInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return in, err
	}
	if len(in.ToolResponse) == 0 {
		in.ToolResponse = in.ToolResult
	}
	var c cursorInput
	if json.Unmarshal(raw, &c) == nil && c.CursorVersion != "" {
		in.Cursor = &c
		normalizeCursorInput(&in)
		recordCursorSession(in)
	}
	return in, nil
}

// normalizeCursorInput maps a Cursor payload onto the fields every hook reads
// (F-0428): tool_output is the shell result as a JSON string; the project is
// the workspace root (Cursor sends cwd "" or its own terminals dir); a missing
// session_id falls back to conversation_id; and loop_count > 0 marks the stop
// of a follow-up, Claude Code's stop_hook_active.
func normalizeCursorInput(in *hookInput) {
	c := in.Cursor
	if len(in.ToolResponse) == 0 {
		// The string's own content when it is JSON (the shell result object),
		// else the JSON string itself: ToolResponse stays valid JSON.
		var s string
		if json.Unmarshal(c.ToolOutput, &s) == nil && json.Valid([]byte(s)) {
			in.ToolResponse = json.RawMessage(s)
		} else {
			in.ToolResponse = c.ToolOutput
		}
	}
	if in.SessionID == "" {
		in.SessionID = c.ConversationID
	}
	if c.LoopCount > 0 {
		in.StopHookActive = true
	}
	in.CWD = workspaceDir(in.CWD, c.WorkspaceRoots)
}

// workspaceDir is cwd when it lies inside one of the workspace roots (symlinks
// resolved), else the first root (cwd when there is none).
func workspaceDir(cwd string, roots []string) string {
	for _, root := range roots {
		if cwd == "" {
			break
		}
		if rel, err := filepath.Rel(realPath(root), realPath(cwd)); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return cwd
		}
	}
	if len(roots) > 0 {
		return roots[0]
	}
	return cwd
}

// writeToolContext answers a PostToolUse hook with msg as context, in the
// envelope of the harness that sent in (F-0428); Copilot reads it top-level.
func writeToolContext(cmd *cobra.Command, in hookInput, format, msg string) error {
	enc := json.NewEncoder(cmd.OutOrStdout())
	if in.cursor() {
		return enc.Encode(cursorContext{AdditionalContext: msg})
	}
	if format == "copilot" {
		return enc.Encode(copilotContext{AdditionalContext: msg})
	}
	var out postToolContext
	out.HookSpecificOutput.HookEventName = "PostToolUse"
	out.HookSpecificOutput.AdditionalContext = msg
	return enc.Encode(out)
}

func newHookBindWorkCmd() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:    "bind-work",
		Short:  "PostToolUse plumbing: bind the session to the work/feature it just registered (F-0151)",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			runHookBindWork(cmd, format)
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", "claude", "input contract: claude|codex|cursor|gemini|copilot (F-0174)")
	return cmd
}

var (
	workStartRe    = regexp.MustCompile(`\bwork\s+start\b`)
	featureStartRe = regexp.MustCompile(`\bfeature\s+start\b`)
	handoffTakeRe  = regexp.MustCompile(`\bhandoff\s+take\b`)
	// workCreatedRe is the line `work start` prints for the work it minted
	// ("W-1755 created (active) — ..."). A chain that also lists works prints
	// other W- ids first, so the created one is told apart by its "(active)".
	// No leading \b: in the raw JSON response a newline is the two characters
	// `\n`, so an id on a later line follows a word character.
	workIDRe      = regexp.MustCompile(`\bW-\d{4,}\b`)
	workCreatedRe = regexp.MustCompile(`(W-\d{4,}) [^\s()]+ \(active\)`)
	// startedIDRe is the id a confirmed `feature start` binds: a feature-family id,
	// the same kinds bindingLive accepts (F-0309).
	startedIDRe = regexp.MustCompile(`\b` + domain.FeatureIDPrefix() + `-\d{4,}\b`)
	// cardCreatedRe is the line every card-creating command prints for the id
	// it minted ("F-0486 created (candidate) …", F-0486). No leading \b, as in
	// workCreatedRe: in the raw JSON the line may follow a literal `\n`.
	cardCreatedRe = regexp.MustCompile(`(` + domain.FeatureIDPrefix() + `-\d{4,}) created \(`)
)

// cerneRunPrefix is what may stand before a cerne verb in a segment that runs
// it: VAR=x assignments, a wrapper that runs the rest as its command (rtk
// proxy, rtk, time, nice, command, env), a path to the binary, and cerne's
// global flags (B-0070: `rtk proxy cerne feature start … | tail` bound nothing).
// ponytail: literal wrapper list; one with value flags (`nice -n 5`) is not seen.
const cerneRunPrefix = `^(?:\w+=\S*\s+)*(?:(?:rtk(?:\s+proxy)?|time|nice|command|env)\s+(?:\w+=\S*\s+)*)*(?:\S*/)?cerne\s+(?:-\S+\s+)*`

var (
	shellSepRe          = regexp.MustCompile(`&&|\|\||[;|&\n]`)
	quotedRe            = regexp.MustCompile(`"[^"]*"|'[^']*'`)
	cerneStartRe        = regexp.MustCompile(cerneRunPrefix + `work\s+start\b`)
	cerneTakeRe         = regexp.MustCompile(cerneRunPrefix + `handoff\s+take\b`)
	cerneFeatureStartRe = regexp.MustCompile(cerneRunPrefix + `feature\s+start\b`)
	cerneCardNewRe      = regexp.MustCompile(cerneRunPrefix + `(?:feature|task|bug|epic)\s+new\b`)
	helpFlagRe          = regexp.MustCompile(`(^|\s)(-h|--help)(\s|$)`)
)

// runsCerne reports whether a Bash command line actually RUNS the cerne verb
// re matches (`cerne work start`, `cerne handoff take`): it sits at the start
// of a command (after ; && || | & or a newline, optional VAR=x prefixes),
// outside quotes, and is not a --help call. Quoted text is blanked first, so
// `echo "cerne work start"` and `git commit -m "... work start"` are
// mentions, not runs (F-0876).
func runsCerne(command string, re *regexp.Regexp) bool {
	return cerneRun(command, re) != ""
}

// shellExitRe is the status line Copilot appends to a shell tool's result.
var shellExitRe = regexp.MustCompile(`completed with exit code (\d+)>`)

// toolFailed reports a Bash call that failed: the word "Error" in its response
// (Claude Code and the others), or — Copilot, probed on 1.0.112, where a failed
// command still has result_type "success" — a non-zero exit code in
// text_result_for_llm, or a result_type other than success (F-0444).
func toolFailed(in hookInput) bool {
	if strings.Contains(string(in.ToolResponse), "Error") {
		return true
	}
	var r struct {
		Type string `json:"result_type"`
		Text string `json:"text_result_for_llm"`
	}
	if json.Unmarshal(in.ToolResult, &r) != nil {
		return false
	}
	if r.Type != "" && r.Type != "success" {
		return true
	}
	m := shellExitRe.FindStringSubmatch(r.Text)
	return m != nil && m[1] != "0"
}

// cerneRun is the first command of the line that runs the cerne verb re
// matches, as runsCerne decides it; "" when none.
func cerneRun(command string, re *regexp.Regexp) string {
	blanked := quotedRe.ReplaceAllString(command, `""`)
	for _, seg := range shellSepRe.Split(blanked, -1) {
		seg = strings.TrimSpace(seg)
		if re.MatchString(seg) && !helpFlagRe.MatchString(seg) {
			return seg
		}
	}
	return ""
}

// Unbound warnings: the minted id never reached the hook (F-0876; take F-0376).
const (
	unboundWorkStart = "cerne: the work may have been created, but this session is NOT bound to it: the output of `cerne work start` did not reach the hook intact (redirected, filtered, or the command failed). The Edit gate keeps denying until it is bound. Run `cerne work start` alone, without a pipe, redirect or chain, so it binds (check with `cerne work current <session-id>`)."
	unboundTake      = "cerne: the handoff may have been taken, but this session is NOT bound to the new work: the output of `cerne handoff take` did not reach the hook intact (redirected, filtered, or the command failed). The Edit gate keeps denying until it is bound. The take is done, so do not re-run it: find the new W- in `cerne work list` (it carries the origin's title) and run `cerne work start \"<description>\" --from W-…` alone, without a pipe, redirect or chain, so this session binds."
)

// warnUnbound tells the agent, through the PostToolUse context channel, that
// what it just registered could not be bound to this session (F-0876).
func warnUnbound(cmd *cobra.Command, in hookInput, format, msg string) {
	_ = writeToolContext(cmd, in, format, msg)
}

// runHookBindWork inspects a finished Bash call: when it was a successful
// `cerne work start` / `cerne feature start` / `cerne handoff take`, the
// minted (or started) id is bound to the session — to the (session, agent)
// key inside a subagent (F-0376) — so the work gate recognizes it. Silent
// no-op on anything else.
func runHookBindWork(cmd *cobra.Command, format string) {
	in, err := decodeHookInput(cmd.InOrStdin())
	if err != nil {
		return
	}
	if in.cursor() {
		format = "cursor" // F-0428: a Cursor payload, whatever the wrapper passed
	}
	// Gemini hands session/cwd through env (R-0036), like the gate.
	if format == "gemini" {
		if in.SessionID == "" {
			in.SessionID = os.Getenv("GEMINI_SESSION_ID")
		}
		if in.CWD == "" {
			if in.CWD = os.Getenv("GEMINI_CWD"); in.CWD == "" {
				in.CWD = os.Getenv("GEMINI_PROJECT_DIR")
			}
		}
	}
	if in.SessionID == "" {
		return
	}
	cfg, projDir, err := project.LoadConfig(in.CWD)
	if err != nil {
		return
	}
	cacheDir, err := project.CacheDir(cfg.ProjectID)
	if err != nil {
		return
	}
	if _, err := guardedRepoDir(cacheDir, cfg, projDir); err != nil {
		return // F-0434: fail open, never through a foreign clone
	}
	// F-0361: every Bash call is a sign of life for the session's binding.
	_ = app.TouchBinding(cacheDir, in.SessionID, in.AgentID)
	var ti struct {
		Command string `json:"command"`
	}
	_ = json.Unmarshal(in.ToolInput, &ti)
	if !strings.Contains(ti.Command, "cerne") {
		return
	}
	// The cost ledger trusts the delivering contract first: only a Claude Code
	// payload can be a claude-code bind, and only under its CLAUDECODE marker.
	harness := cost.CurrentHarness()
	if format != "claude" {
		harness = "other"
	}
	// F-0486: a card the session created records it as the card's opener — a
	// mark of its own, never a binding (a candidate is no live binding, and a
	// bind would move the session's cost to the new card). A chain that also
	// starts or takes something still binds below.
	if cerneRun(ti.Command, cerneCardNewRe) != "" {
		if m := cardCreatedRe.FindStringSubmatch(string(in.ToolResponse)); m != nil {
			if err := app.RecordMark(cacheDir, in.SessionID, in.AgentID, harness, m[1], app.RoleOpened); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: card opener not recorded: %v\n", err)
			}
		}
	}
	var id string
	switch {
	case workStartRe.MatchString(ti.Command):
		// The W- id is minted by the command; it appears in the response.
		if m := workCreatedRe.FindStringSubmatch(string(in.ToolResponse)); m != nil {
			id = m[1]
		} else {
			// Redirected or filtered output (or a refused start): there is no
			// minted id to bind, and the Edit gate will keep denying. Say so (F-0876).
			// Only a real, un-helped, un-refused start: a Bash call that merely
			// MENTIONS "work start" (grep, echo, a commit message) is not one.
			if runsCerne(ti.Command, cerneStartRe) && !toolFailed(in) {
				warnUnbound(cmd, in, format, unboundWorkStart)
			}
		}
	case handoffTakeRe.MatchString(ti.Command):
		// The take prints "HO-… taken by … — W-… created (active) — …"; a
		// refused take prints no "(active)" and binds nothing (F-0376).
		if m := workCreatedRe.FindStringSubmatch(string(in.ToolResponse)); m != nil {
			id = m[1]
		} else if runsCerne(ti.Command, cerneTakeRe) && !toolFailed(in) {
			warnUnbound(cmd, in, format, unboundTake)
		}
	case featureStartRe.MatchString(ti.Command):
		// The F-/E-/T- id is the argument; bind only if the response confirms the
		// transition (a refused gate prints an error, not the id -> state).
		if strings.Contains(string(in.ToolResponse), "in_development") {
			id = startedIDRe.FindString(ti.Command)
		} else if run := startedIDRe.FindString(cerneRun(ti.Command, cerneFeatureStartRe)); run != "" {
			// A pipe or redirect (`2>&1 | tail -5`) cut the transition line, which
			// comes first: the clone still tells a real start (B-0002).
			if f, err := store.LoadFeature(filepath.Join(cacheDir, "repo"), run); err == nil && f.State == domain.StateInDevelopment {
				id = run
			}
		}
	}
	if id == "" {
		return
	}
	// Still exit 0 (fail-open), but a bind that cannot be recorded — e.g. a
	// dotted session id (F-0376) — must not vanish without a word.
	if err := app.BindSessionAgent(cacheDir, in.SessionID, in.AgentID, harness, id); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: session binding not recorded: %v\n", err)
	}
}

func newHookWorkGateCmd() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:    "work-gate",
		Short:  "PreToolUse plumbing: deny Edit/Write in a session with no live registration (F-0151)",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			runHookWorkGate(cmd, format)
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", "claude", "deny envelope: claude|codex|cursor|gemini|copilot (F-0174)")
	return cmd
}

// gateDeny is the PreToolUse decision envelope Claude Code expects — and,
// documented identical, Codex (R-0035).
type gateDeny struct {
	HookSpecificOutput struct {
		HookEventName            string `json:"hookEventName"`
		PermissionDecision       string `json:"permissionDecision"`
		PermissionDecisionReason string `json:"permissionDecisionReason"`
	} `json:"hookSpecificOutput"`
}

// cursorGateDeny is Cursor's native preToolUse permission envelope (R-0037).
type cursorGateDeny struct {
	Permission   string `json:"permission"`
	AgentMessage string `json:"agent_message"`
}

// copilotGateDeny is Copilot's PreToolUse decision: top-level fields, not the
// nested hookSpecificOutput of Claude Code (F-0444).
type copilotGateDeny struct {
	PermissionDecision       string `json:"permissionDecision"`
	PermissionDecisionReason string `json:"permissionDecisionReason"`
}

// geminiGateDeny is Gemini's BeforeTool decision envelope (R-0036).
type geminiGateDeny struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
}

// runHookWorkGate evaluates one Edit/Write call. Every early return is an
// allow (no output); only a project file in an unbound session prints the
// deny decision, in the envelope of the requested provider format.
func runHookWorkGate(cmd *cobra.Command, format string) {
	in, err := decodeHookInput(cmd.InOrStdin())
	if err != nil {
		return
	}
	if in.cursor() {
		format = "cursor" // F-0428: a Cursor payload, whatever the wrapper passed
	}
	// Gemini's hook runtime hands session/cwd through env, not the payload
	// (R-0036: GEMINI_SESSION_ID, GEMINI_CWD/GEMINI_PROJECT_DIR).
	if format == "gemini" {
		if in.SessionID == "" {
			in.SessionID = os.Getenv("GEMINI_SESSION_ID")
		}
		if in.CWD == "" {
			if in.CWD = os.Getenv("GEMINI_CWD"); in.CWD == "" {
				in.CWD = os.Getenv("GEMINI_PROJECT_DIR")
			}
		}
	}
	if format == "copilot" && in.CWD == "" { // F-0444: project dir from the host env, then the process cwd
		in.CWD = os.Getenv("COPILOT_PROJECT_DIR")
	}
	cfg, projDir, err := project.LoadConfig(in.CWD)
	if err != nil {
		return
	}
	cacheDir, err := project.CacheDir(cfg.ProjectID)
	if err != nil {
		return
	}
	repoDir, err := guardedRepoDir(cacheDir, cfg, projDir)
	if err != nil {
		return // F-0434
	}
	_ = app.TouchBinding(cacheDir, in.SessionID, in.AgentID) // F-0361: sign of life
	recordSessionMode(cacheDir, in)
	paths := editedPaths(in.ToolInput)
	for i, p := range paths { // Copilot's patch paths are relative to its cwd
		if !filepath.IsAbs(p) {
			base := in.CWD
			if base == "" {
				base, _ = os.Getwd()
			}
			paths[i] = filepath.Join(base, p) // never left relative
		}
	}
	first := ""
	if len(paths) > 0 {
		first = paths[0]
	}
	if !cfg.Gates.WorkGateEnabled() || first == "" {
		injectGateChannel(cmd, format, cacheDir, repoDir, projDir, in.SessionID, in.AgentID, first)
		return
	}
	allow, reason := true, ""
	for _, p := range paths {
		var r string
		if allow, r = app.WorkGateCheck(cacheDir, repoDir, projDir, in.SessionID, in.AgentID, p, cfg.Gates.WorkGateAllow); !allow {
			reason = r
			break
		} else if r != "" {
			reason = r
		}
	}
	if allow {
		if reason != "" { // allowed by fail-open: say why on stderr (F-0376)
			fmt.Fprintln(cmd.ErrOrStderr(), "warning: "+reason)
		}
		injectGateChannel(cmd, format, cacheDir, repoDir, projDir, in.SessionID, in.AgentID, first)
		return
	}
	// F-0170: the deny message is also a teaching moment — append the
	// project's own pointer (e.g. its convention catalog) when configured.
	if hint := cfg.Gates.WorkGateHint; hint != "" {
		reason += " — " + hint
	}
	if err := encodeDeny(cmd.OutOrStdout(), format, reason); err != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), err)
	}
}

// encodeDeny writes the pre-tool deny decision in the envelope of format.
func encodeDeny(w io.Writer, format, reason string) error {
	var payload any
	switch format {
	case "cursor":
		payload = cursorGateDeny{Permission: "deny", AgentMessage: reason}
	case "gemini":
		payload = geminiGateDeny{Decision: "deny", Reason: reason}
	case "copilot":
		payload = copilotGateDeny{PermissionDecision: "deny", PermissionDecisionReason: reason}
	default: // claude and codex share the envelope (R-0035)
		var d gateDeny
		d.HookSpecificOutput.HookEventName = "PreToolUse"
		d.HookSpecificOutput.PermissionDecision = "deny"
		d.HookSpecificOutput.PermissionDecisionReason = reason
		payload = d
	}
	return json.NewEncoder(w).Encode(payload)
}

// patchFileRe matches the file lines of an apply_patch text.
var patchFileRe = regexp.MustCompile(`(?m)^\*\*\* (?:(?:Add|Update|Delete) File|Move to): (.+?)\s*$`)

// editedPaths lists the files an edit tool call targets: file_path, or path
// (Copilot's own arguments), or the files an apply_patch text names — the
// whole tool_input on Copilot (probed, F-0444), tool_input.command on Codex
// (seen live, F-0457).
func editedPaths(toolInput json.RawMessage) []string {
	var obj struct {
		FilePath string `json:"file_path"`
		Path     string `json:"path"`
		Command  string `json:"command"`
	}
	var patch string
	if json.Unmarshal(toolInput, &obj) == nil {
		if obj.FilePath != "" {
			return []string{obj.FilePath}
		}
		if obj.Path != "" {
			return []string{obj.Path}
		}
		if !isPatch(obj.Command) {
			return nil
		}
		patch = obj.Command
	} else if json.Unmarshal(toolInput, &patch) != nil {
		return nil
	}
	var out []string
	for _, m := range patchFileRe.FindAllStringSubmatch(patch, -1) {
		out = append(out, m[1])
	}
	return out
}

// isPatch reports whether text is an apply_patch body, never a shell command.
func isPatch(text string) bool {
	return strings.HasPrefix(strings.TrimSpace(text), "*** Begin Patch")
}

// injectGateChannel delivers, on an allowed edit, the session's pending channel
// messages (F-0325) and the notes citing the edited file (F-0366): the instant
// a collision or a known gotcha matters. PreToolUse additionalContext is
// documented for Claude Code (R-0034) and Codex shares the envelope (R-0035);
// Cursor and Gemini document no context on an allowed preToolUse, so they get
// none. Fail-open: any error is silence.
func injectGateChannel(cmd *cobra.Command, format, cacheDir, repoDir, projDir, sess, agent, filePath string) {
	// The Copilot CLI also runs these .claude hooks (COPILOT_CLI=1) and ignores
	// their output: consuming the channel or marking the notes seen here would
	// silence its own post-tool hook, the channel it documents (F-0457, B-0054).
	if format != "claude" && format != "codex" || os.Getenv("COPILOT_CLI") == "1" {
		return
	}
	block := channelThen(cacheDir, repoDir, sess, agent, notesOnEditBlock(cacheDir, repoDir, projDir, sess, filePath, ""))
	if block == "" {
		return
	}
	var c sessionContext
	c.HookSpecificOutput.HookEventName = "PreToolUse"
	c.HookSpecificOutput.AdditionalContext = block
	_ = json.NewEncoder(cmd.OutOrStdout()).Encode(c)
}

// notesBudget is the hard deadline of the notes lookup: a busy index or a slow
// listing must never hold an edit (F-0366).
const notesBudget = 150 * time.Millisecond

// notesOnEditBlock lists up to 3 not-yet-shown active notes citing or
// declaring filePath (suffix marks an edit already made, F-0457), or "" (no
// setting, no file inside the repo, no index, no match, any failure).
func notesOnEditBlock(cacheDir, repoDir, projDir, sess, filePath, suffix string) string {
	if filePath == "" || !project.NotesOnEdit(projDir) {
		return ""
	}
	abs := filePath
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(projDir, abs)
	}
	rel, err := filepath.Rel(realPath(projDir), realPath(abs))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ""
	}
	rel = filepath.ToSlash(rel)
	if _, err := os.Stat(filepath.Join(cacheDir, "index.db")); err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), notesBudget)
	defer cancel()
	notes, err := app.NotesForPath(ctx, cacheDir, repoDir, rel, app.UniqueNames(ctx, projDir))
	if err != nil || ctx.Err() != nil {
		return ""
	}
	seen := app.SeenNotes(cacheDir, sess)
	var fresh []app.PathNote
	for _, n := range notes {
		if !seen[n.ID] {
			fresh = append(fresh, n)
		}
	}
	if len(fresh) == 0 {
		return ""
	}
	shown := fresh[:min(3, len(fresh))]
	var ids []string
	for _, n := range shown {
		ids = append(ids, n.ID)
	}
	// No usable session id: dedup is impossible, and the same notes on every
	// edit are worse than silence.
	if app.MarkNotesSeen(cacheDir, sess, ids) != nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "cerne notes on %s%s:", rel, suffix)
	for _, n := range shown {
		b.WriteString(noteLine(n.ID, n.Title, n.Gist))
	}
	// Same basis as `note list --path`: every match beyond the ones listed here.
	if rest := len(notes) - len(shown); rest > 0 {
		hint := rel
		if strings.ContainsAny(hint, " \t'\"") {
			hint = "'" + strings.ReplaceAll(hint, "'", `'\''`) + "'"
		}
		fmt.Fprintf(&b, "\n(+%d: cerne note list --path %s)", rest, hint)
	}
	return b.String()
}

// flatTitle flattens a title so it cannot break the block's line structure.
func flatTitle(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// noteLine is one note of a hook block: "\n- <id> — <title>", plus, when the
// note has a gist (F-0459), "\n  <gist>" — flattened and capped again, since
// the entity may have been edited by hand in the clone.
func noteLine(id, title, gist string) string {
	line := "\n- " + id + " — " + flatTitle(title)
	if g := flatTitle(gist); g != "" {
		line += "\n  " + capRunes(g, domain.GistMax)
	}
	return line
}

// capRunes cuts s to n runes, with an ellipsis when it was longer.
func capRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// realPath resolves symlinks (macOS /var vs /private/var) even for a path that
// does not exist yet, by resolving its nearest existing ancestor.
func realPath(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	if d := filepath.Dir(p); d != p {
		return filepath.Join(realPath(d), filepath.Base(p))
	}
	return p
}
