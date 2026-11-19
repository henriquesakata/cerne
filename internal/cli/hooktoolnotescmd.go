package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/project"
)

// Tool notes (F-0457): the notes whose declared trigger matches a tool call,
// shown to the agent through the context channel its harness documents —
// before the tool on Claude Code and Codex (PreToolUse additionalContext),
// right after it on Cursor, Gemini and Copilot, whose pre-tool hooks can only
// deny. Local only and fail-open like every hook subcommand: any error is
// silence, and the hook never decides on the tool.

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
	if err != nil || !toolNotesEmits(after, format, in.cursor(), os.Getenv("COPILOT_CLI") == "1") {
		return
	}
	var ti struct {
		Command string `json:"command"`
	}
	_ = json.Unmarshal(in.ToolInput, &ti)
	if ti.Command == "" {
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
	block := commandNotesBlock(cacheDir, repoDir, projDir, in.SessionID, ti.Command, "")
	if block == "" {
		return
	}
	var c sessionContext
	c.HookSpecificOutput.HookEventName = "PreToolUse"
	c.HookSpecificOutput.AdditionalContext = block
	_ = json.NewEncoder(cmd.OutOrStdout()).Encode(c)
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
		fmt.Fprintf(&b, "\n- %s — %s", n.ID, flatTitle(n.Title))
	}
	return b.String()
}
