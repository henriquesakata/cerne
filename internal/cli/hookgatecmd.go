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
	"github.com/henriquesakata/cerne/internal/project"
)

// Hook plumbing subcommands (F-0151). Both read the Claude Code hook JSON from
// stdin and are wired by `cerne hook install` (never typed by a user, so they
// stay hidden). Both are deliberately silent and always exit 0 on anything
// unexpected: a hook that errors or blocks on infra breaks every tool call in
// the host session — fail-open is the only acceptable failure mode. The gate's
// on/off switch ([gates] work_gate) is evaluated here at run time, not at
// install time, so toggling it needs no reinstall.

// hookInput is the common envelope of a PreToolUse/PostToolUse hook payload.
type hookInput struct {
	SessionID    string          `json:"session_id"`
	CWD          string          `json:"cwd"`
	ToolName     string          `json:"tool_name"`
	ToolInput    json.RawMessage `json:"tool_input"`
	ToolResponse json.RawMessage `json:"tool_response"`
	// AgentID is present only when the hook fires inside a subagent (whose
	// session_id is still the parent's); TranscriptPath is the session's
	// JSONL (F-0309).
	AgentID        string `json:"agent_id"`
	TranscriptPath string `json:"transcript_path"`
	// StopHookActive is set on a Stop payload when the stop is itself the
	// continuation of a Stop hook (F-0294).
	StopHookActive bool `json:"stop_hook_active"`
	// Source is the SessionStart origin; "compact" after a compaction (F-0368).
	Source string `json:"source"`
}

func decodeHookInput(r io.Reader) (hookInput, error) {
	var in hookInput
	err := json.NewDecoder(r).Decode(&in)
	return in, err
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
	cmd.Flags().StringVar(&format, "format", "claude", "input contract: claude|codex|cursor|gemini (F-0174)")
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
	featureIDRe   = regexp.MustCompile(`\bF-\d{4,}\b`)
	// startedIDRe is the id a confirmed `feature start` binds: F-, E- or T-,
	// the same kinds bindingLive accepts (F-0309).
	startedIDRe = regexp.MustCompile(`\b[FET]-\d{4,}\b`)
)

var (
	shellSepRe   = regexp.MustCompile(`&&|\|\||[;|&\n]`)
	quotedRe     = regexp.MustCompile(`"[^"]*"|'[^']*'`)
	cerneStartRe = regexp.MustCompile(`^(?:\w+=\S*\s+)*(?:\S*/)?cerne\s+(?:-\S+\s+)*work\s+start\b`)
	helpFlagRe   = regexp.MustCompile(`(^|\s)(-h|--help)(\s|$)`)
)

// runsWorkStart reports whether a Bash command line actually RUNS
// `cerne work start`: it sits at the start of a command (after ; && || | & or
// a newline, optional VAR=x prefixes), outside quotes, and is not a --help call.
// Quoted text is blanked first, so `echo "cerne work start"` and
// `git commit -m "... work start"` are mentions, not runs (F-0876).
func runsWorkStart(command string) bool {
	blanked := quotedRe.ReplaceAllString(command, `""`)
	for _, seg := range shellSepRe.Split(blanked, -1) {
		seg = strings.TrimSpace(seg)
		if cerneStartRe.MatchString(seg) && !helpFlagRe.MatchString(seg) {
			return true
		}
	}
	return false
}

// warnUnbound tells the agent, through the PostToolUse context channel, that
// the work it just started could not be bound to this session (F-0876).
func warnUnbound(cmd *cobra.Command) {
	var out postToolContext
	out.HookSpecificOutput.HookEventName = "PostToolUse"
	out.HookSpecificOutput.AdditionalContext = "cerne: the work may have been created, but this session is NOT bound to it: the output of `cerne work start` did not reach the hook intact (redirected, filtered, or the command failed). The Edit gate keeps denying until it is bound. Run `cerne work start` alone, without a pipe, redirect or chain, so it binds (check with `cerne work current <session-id>`)."
	_ = json.NewEncoder(cmd.OutOrStdout()).Encode(out)
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
	cfg, _, err := project.LoadConfig(in.CWD)
	if err != nil {
		return
	}
	cacheDir, err := project.CacheDir(cfg.ProjectID)
	if err != nil {
		return
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
			if runsWorkStart(ti.Command) && !strings.Contains(string(in.ToolResponse), "Error") {
				warnUnbound(cmd)
			}
		}
	case handoffTakeRe.MatchString(ti.Command):
		// The take prints "HO-… taken by … — W-… created (active) — …"; a
		// refused take prints no "(active)" and binds nothing (F-0376).
		if m := workCreatedRe.FindStringSubmatch(string(in.ToolResponse)); m != nil {
			id = m[1]
		}
	case featureStartRe.MatchString(ti.Command):
		// The F-/E-/T- id is the argument; bind only if the response confirms the
		// transition (a refused gate prints an error, not the id -> state).
		if strings.Contains(string(in.ToolResponse), "in_development") {
			id = startedIDRe.FindString(ti.Command)
		}
	}
	if id == "" {
		return
	}
	// The cost ledger trusts the delivering contract first: only a Claude Code
	// payload can be a claude-code bind, and only under its CLAUDECODE marker.
	harness := cost.CurrentHarness()
	if format != "claude" {
		harness = "other"
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
	cmd.Flags().StringVar(&format, "format", "claude", "deny envelope: claude|codex|cursor|gemini (F-0174)")
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
	cfg, projDir, err := project.LoadConfig(in.CWD)
	if err != nil {
		return
	}
	cacheDir, err := project.CacheDir(cfg.ProjectID)
	if err != nil {
		return
	}
	repoDir := filepath.Join(cacheDir, "repo")
	_ = app.TouchBinding(cacheDir, in.SessionID, in.AgentID) // F-0361: sign of life
	var ti struct {
		FilePath string `json:"file_path"`
	}
	_ = json.Unmarshal(in.ToolInput, &ti)
	if !cfg.Gates.WorkGateEnabled() || ti.FilePath == "" {
		injectGateChannel(cmd, format, cacheDir, repoDir, projDir, in.SessionID, ti.FilePath)
		return
	}
	allow, reason := app.WorkGateCheck(cacheDir, repoDir, projDir, in.SessionID, in.AgentID, ti.FilePath, cfg.Gates.WorkGateAllow)
	if allow {
		if reason != "" { // allowed by fail-open: say why on stderr (F-0376)
			fmt.Fprintln(cmd.ErrOrStderr(), "warning: "+reason)
		}
		injectGateChannel(cmd, format, cacheDir, repoDir, projDir, in.SessionID, ti.FilePath)
		return
	}
	// F-0170: the deny message is also a teaching moment — append the
	// project's own pointer (e.g. its convention catalog) when configured.
	if hint := cfg.Gates.WorkGateHint; hint != "" {
		reason += " — " + hint
	}
	enc := json.NewEncoder(cmd.OutOrStdout())
	var payload any
	switch format {
	case "cursor":
		payload = cursorGateDeny{Permission: "deny", AgentMessage: reason}
	case "gemini":
		payload = geminiGateDeny{Decision: "deny", Reason: reason}
	default: // claude and codex share the envelope (R-0035)
		var d gateDeny
		d.HookSpecificOutput.HookEventName = "PreToolUse"
		d.HookSpecificOutput.PermissionDecision = "deny"
		d.HookSpecificOutput.PermissionDecisionReason = reason
		payload = d
	}
	if err := enc.Encode(payload); err != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), err)
	}
}

// injectGateChannel delivers, on an allowed edit, the session's pending channel
// messages (F-0325) and the notes citing the edited file (F-0366): the instant
// a collision or a known gotcha matters. PreToolUse additionalContext is
// documented for Claude Code (R-0034) and Codex shares the envelope (R-0035);
// Cursor and Gemini document no context on an allowed preToolUse, so they get
// none. Fail-open: any error is silence.
func injectGateChannel(cmd *cobra.Command, format, cacheDir, repoDir, projDir, sess, filePath string) {
	if format != "claude" && format != "codex" {
		return
	}
	block, err := app.ChannelInject(cacheDir, repoDir, sess, true, time.Now().UTC())
	if err != nil {
		block = ""
	}
	if nb := notesOnEditBlock(cacheDir, repoDir, projDir, sess, filePath); nb != "" {
		if block != "" {
			block += "\n\n"
		}
		block += nb
	}
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

// notesOnEditBlock lists up to 3 not-yet-shown active notes citing filePath, or
// "" (no setting, no file inside the repo, no index, no match, any failure).
func notesOnEditBlock(cacheDir, repoDir, projDir, sess, filePath string) string {
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
	fmt.Fprintf(&b, "cerne notes on %s:", rel)
	for _, n := range shown {
		fmt.Fprintf(&b, "\n- %s — %s", n.ID, flatTitle(n.Title))
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
