package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/project"
)

// Tool notes (F-0457): the notes whose declared trigger matches a tool call,
// plus the session's pending project-channel messages (B-0054), shown to the
// agent through the context channel its harness documents —
// before the tool on Claude Code and Codex (PreToolUse additionalContext),
// right after it on Cursor, Gemini and Copilot, whose pre-tool hooks can only
// deny. Local only and fail-open like every hook subcommand: any error is
// silence, and the hook decides on the tool only under [gates] accept =
// "merge", where it denies an agent's PR merge (F-0488).

func newHookToolNotesCmd() *cobra.Command {
	var format string
	var after bool
	cmd := &cobra.Command{
		Use:    "tool-notes",
		Short:  "Pre/PostToolUse plumbing: show the notes whose declared trigger matches the tool call (F-0457)",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			runHookToolNotes(cmd, format, after)
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", "claude", "context envelope: claude|codex|cursor|gemini|copilot")
	cmd.Flags().BoolVar(&after, "after", false, "post-tool mode, for the harnesses that only take context after a tool (Cursor, Gemini, Copilot)")
	return cmd
}

// toolNotesEmits says whether a call answers: before the tool only Claude
// Code and Codex take context; after it only Cursor (recognized by its
// payload, whatever the wrapper passed), Gemini and Copilot. copilotCLI marks
// the Copilot CLI running the .claude hooks, whose output it ignores: marking
// notes as seen there would silence Copilot's own post-tool hook.
func toolNotesEmits(after bool, format string, cursor, copilotCLI bool) bool {
	switch {
	case cursor:
		return after
	case format == "gemini", format == "copilot":
		return after
	case format == "claude", format == "codex":
		return !after && !copilotCLI
	}
	return false
}

func runHookToolNotes(cmd *cobra.Command, format string, after bool) {
	in, err := decodeHookInput(cmd.InOrStdin())
	if err != nil {
		return
	}
	if !after && denyAgentMerge(cmd, in, format) {
		return
	}
	if !toolNotesEmits(after, format, in.cursor(), os.Getenv("COPILOT_CLI") == "1") {
		return
	}
	if in.cursor() {
		format = "cursor" // F-0428: a Cursor payload, whatever the wrapper passed
	}
	switch format { // the host env fills what the payload may lack, as in the gate
	case "gemini":
		if in.SessionID == "" {
			in.SessionID = os.Getenv("GEMINI_SESSION_ID")
		}
		if in.CWD == "" {
			if in.CWD = os.Getenv("GEMINI_CWD"); in.CWD == "" {
				in.CWD = os.Getenv("GEMINI_PROJECT_DIR")
			}
		}
	case "copilot":
		if in.CWD == "" {
			in.CWD = os.Getenv("COPILOT_PROJECT_DIR")
		}
	}
	// A path makes the call an edit, whatever else its input carries (Codex's
	// apply_patch has its patch under "command", a text editor a verb).
	var ti struct {
		Command string `json:"command"`
	}
	var edited string
	if paths := editedPaths(in.ToolInput); len(paths) > 0 {
		if !after {
			return // before an edit, the work gate already speaks (F-0366)
		}
		edited = paths[0]
		if !filepath.IsAbs(edited) { // patch paths are relative to the cwd
			edited = filepath.Join(in.CWD, edited)
		}
	} else if json.Unmarshal(in.ToolInput, &ti); ti.Command == "" {
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
	repoDir, err := guardedRepoDir(cacheDir, cfg, projDir)
	if err != nil {
		return // F-0434
	}
	recordSessionMode(cacheDir, in)
	var block string
	switch {
	case ti.Command != "" && after:
		block = commandNotesBlock(cacheDir, repoDir, projDir, in.SessionID, ti.Command, " (the command already ran)")
	case ti.Command != "":
		block = commandNotesBlock(cacheDir, repoDir, projDir, in.SessionID, ti.Command, "")
	default:
		block = notesOnEditBlock(cacheDir, repoDir, projDir, in.SessionID, edited, " (after this edit)")
	}
	// A shell command is a channel contact point too (B-0054): a session that
	// works only through Bash would otherwise hear the channel only at start.
	block = channelThen(cacheDir, repoDir, in.SessionID, in.AgentID, block)
	if block == "" {
		return
	}
	enc := json.NewEncoder(cmd.OutOrStdout())
	switch {
	case !after:
		var c sessionContext
		c.HookSpecificOutput.HookEventName = "PreToolUse"
		c.HookSpecificOutput.AdditionalContext = block
		_ = enc.Encode(c)
	case format == "cursor":
		_ = enc.Encode(cursorContext{AdditionalContext: block})
	case format == "copilot":
		_ = enc.Encode(copilotContext{AdditionalContext: block})
	default: // gemini: appended to the tool result (R-0036)
		var c sessionContext
		c.HookSpecificOutput.HookEventName = "AfterTool"
		c.HookSpecificOutput.AdditionalContext = block
		_ = enc.Encode(c)
	}
}

// commandNotesBlock lists up to 3 not-yet-shown active notes whose bash:
// trigger matches command, under a header naming the matched words (suffix
// marks a command that already ran), or "" (setting off, no index, no match,
// no usable session, any failure).
func commandNotesBlock(cacheDir, repoDir, projDir, sess, command, suffix string) string {
	if !project.NotesOnBash(projDir) {
		return ""
	}
	if _, err := os.Stat(filepath.Join(cacheDir, "index.db")); err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), notesBudget)
	defer cancel()
	notes, err := app.NotesForCommand(ctx, cacheDir, repoDir, command)
	if err != nil || ctx.Err() != nil {
		return ""
	}
	seen := app.SeenNotes(cacheDir, sess)
	var shown []app.CommandNote
	for _, n := range notes {
		if !seen[n.ID] && len(shown) < 3 {
			shown = append(shown, n)
		}
	}
	if len(shown) == 0 {
		return ""
	}
	var ids, words []string
	for _, n := range shown {
		ids = append(ids, n.ID)
		if w := "`" + n.Words + "`"; !slices.Contains(words, w) {
			words = append(words, w)
		}
	}
	// No usable session id: dedup is impossible, and the same notes on every
	// command are worse than silence.
	if app.MarkNotesSeen(cacheDir, sess, ids) != nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "cerne notes on %s%s:", strings.Join(words, ", "), suffix)
	for _, n := range shown {
		b.WriteString(noteLine(n.ID, n.Title, n.Gist))
	}
	return b.String()
}

// mergeCmdRe matches a PR/MR merge at a command position: line start, after
// ; & | ( $( or a quoted sh -c, past env assignments and the wrappers and
// keywords that run a command (sudo, env, command, exec, if, then, { …).
// The forms: gh pr merge, glab mr merge, tea pulls/pr merge, a gh/glab api
// PUT to a merge endpoint (the GET of the same path only reads), and the
// GraphQL merge mutations. A mention inside an argument (a commit message)
// is not at a command position. Known ceilings: a heredoc line that starts
// with a merge command is denied, and a merge through git push, curl or a
// non-shell tool is not seen (F-0488).
var mergeCmdRe = regexp.MustCompile(`(?m)(?:^|[;&|(]|\$\(|\b(?:ba|z)?sh\s+-c\s+["'])\s*` +
	`(?:(?:[A-Za-z_][A-Za-z0-9_]*=\S*|sudo|env|command|exec|time|nohup|xargs|if|elif|then|else|do|while|until|!|\{)\s+)*` +
	`(?:` +
	`(?:gh|glab|tea)\s[^;&|\n]*?\b(?:pr|mr|pulls?)\s+merge\b` +
	`|(?:gh|glab)\s[^;&|\n]*?\bapi\b[^;&|\n]*?(?:(?:-X\s*|--method[\s=]+)(?i:put)\b[^;&|\n]*?/(?:pulls|merge_requests)/[0-9]+/merge\b|/(?:pulls|merge_requests)/[0-9]+/merge\b[^;&|\n]*?(?:-X\s*|--method[\s=]+)(?i:put)\b)` +
	`|gh\s[^;&|\n]*?\bapi\s+graphql\b[^\n]*?\b(?:mergePullRequest|enablePullRequestAutoMerge)\b` +
	`)`)

// mergeDenyReason is what the agent reads when its merge is refused.
const mergeDenyReason = "this project accepts a card on the merge of its PR ([gates] accept = \"merge\" or \"risk\", F-0488, F-0516): the merge is the human's acceptance, so an agent never merges — ask a human to merge the PR, then record it with 'cerne feature accept --merged'"

// denyAgentMerge refuses, before the tool, an agent's PR merge in a project
// under the merge gate (F-0488 crit. 10), in the envelope of the harness that
// runs the hook. Fail-open like every hook: no project, no gate, no match is
// a pass. It reports whether it denied.
func denyAgentMerge(cmd *cobra.Command, in hookInput, format string) bool {
	var ti struct {
		Command string `json:"command"`
	}
	if json.Unmarshal(in.ToolInput, &ti) != nil || !mergeCmdRe.MatchString(ti.Command) {
		return false
	}
	if in.cursor() {
		format = "cursor"
	}
	cwd := in.CWD
	if cwd == "" && format == "copilot" {
		cwd = os.Getenv("COPILOT_PROJECT_DIR")
	}
	cfg, _, err := project.LoadConfig(cwd)
	if err != nil || !cfg.Gates.AcceptsOnMerge() {
		return false
	}
	return encodeDeny(cmd.OutOrStdout(), format, mergeDenyReason) == nil
}
