package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/hooks"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/skills"
	"github.com/henriquesakata/cerne/internal/store"
)

// Session context plumbing (F-0174). Emits the canonical registration rules
// plus the LIVE work list in the envelope of the requested provider — the
// SessionStart injection every provider gets, so an agent on Cursor, Codex or
// Gemini starts a session as informed as one on Claude Code. Local-only except for the
// bounded pull of a missing projection (B-0017; the work list comes from the
// backend clone) and fail-open: outside a cerne
// project, or on any error loading works, it degrades gracefully.

// cursorContext is Cursor's native sessionStart output envelope.
type cursorContext struct {
	AdditionalContext string `json:"additional_context"`
}

// copilotContext is Copilot's SessionStart/PostToolUse context envelope:
// top-level, not nested under hookSpecificOutput (F-0444).
type copilotContext struct {
	AdditionalContext string `json:"additionalContext"`
}

// sessionContext is the Claude Code-style SessionStart envelope, also
// accepted by Codex (documented identical) and Gemini (Claude-compatible
// hooks contract, `hooks migrate --from-claude`).
type sessionContext struct {
	HookSpecificOutput struct {
		HookEventName     string `json:"hookEventName"`
		AdditionalContext string `json:"additionalContext"`
	} `json:"hookSpecificOutput"`
}

// newHookGauntletRuleCmd prints the GAUNTLET session rule for Claude Code's
// SessionStart script (F-0364): nothing when the project runs in light mode,
// the rule otherwise — including outside a project or on an unreadable
// .cernerc (fail-safe toward the Gauntlet). It never reads stdin, so the
// script's later call still gets the hook payload.
func newHookGauntletRuleCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "gauntlet-rule",
		Short:  "SessionStart plumbing: the GAUNTLET rule, or nothing in light mode (F-0364)",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := os.Getenv("CLAUDE_PROJECT_DIR")
			if dir == "" {
				dir, _ = os.Getwd()
			}
			if cfg, _, err := project.LoadConfig(dir); err == nil && !cfg.Gates.GauntletEnabled() {
				return nil
			}
			fmt.Fprintln(cmd.OutOrStdout(), hooks.GauntletRule)
			return nil
		},
	}
}

func newHookSessionContextCmd() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:    "session-context",
		Short:  "SessionStart plumbing: registration rules + live work list, per provider envelope (F-0174)",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			runHookSessionContext(cmd, format)
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", "claude", "output envelope: claude|codex|cursor|gemini|copilot")
	return cmd
}

// sessionPullBudget bounds the SessionStart materialization (B-0017). The
// installed hooks carry no timeout of their own, so the host's default applies;
// this stays far below it so a slow or offline backend never stalls a session.
var sessionPullBudget = 10 * time.Second

// sessionPull materializes the projection of projDir, all output discarded so
// it cannot reach the hook's envelope. It is the normal `cerne pull`, so it
// takes the same backend lock as an interactive one.
var sessionPull func(projDir string) error

func init() { // assigned here: Root refers back to this file (initialization cycle)
	sessionPull = func(projDir string) error {
		root := Root()
		root.SetOut(io.Discard)
		root.SetErr(io.Discard)
		root.SetArgs([]string{"pull", "--dir", projDir})
		return root.Execute()
	}
}

// materializeIfMissing runs the pull when projDir has no .docs/CONTEXT.md (a
// fresh git worktree: .docs/ is gitignored) and returns the one-line notice to
// inject when it fails or runs out of budget; "" otherwise (B-0017).
func materializeIfMissing(projDir string) string {
	if _, err := os.Stat(filepath.Join(projDir, ".docs", "CONTEXT.md")); err == nil {
		return ""
	}
	done := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- fmt.Errorf("pull panicked: %v", r)
			}
		}()
		done <- sessionPull(projDir)
	}()
	select {
	case err := <-done:
		if err == nil {
			return ""
		}
	case <-time.After(sessionPullBudget):
	}
	return "cerne: .docs/ is not materialized in this checkout and the automatic pull did not finish — run `cerne pull` before reading .docs/CONTEXT.md."
}

func runHookSessionContext(cmd *cobra.Command, format string) {
	in, _ := decodeHookInput(cmd.InOrStdin()) // best effort: some providers send no payload
	if in.cursor() {
		format = "cursor" // F-0428: a Cursor payload, whatever the wrapper passed
	}
	dir := in.CWD
	if dir == "" {
		for _, env := range []string{"GEMINI_PROJECT_DIR", "GEMINI_CWD", "CURSOR_PROJECT_DIR", "CLAUDE_PROJECT_DIR"} {
			if v := os.Getenv(env); v != "" {
				dir = v
				break
			}
		}
	}
	if dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return
		}
		dir = wd
	}
	cfg, projDir, err := project.LoadConfig(dir)
	if err != nil {
		return // not a cerne project: never inject
	}

	if in.SessionID != "" {
		if cacheDir, err := project.CacheDir(cfg.ProjectID); err == nil {
			_ = app.TouchBinding(cacheDir, in.SessionID, "") // F-0361: sign of life
		}
	}

	missingDocs := materializeIfMissing(projDir) // B-0017

	stale := ""
	if format == "copilot" {
		stale = copilotStaleNotice(projDir)
	}
	ours := cloneIsOurs(cfg, projDir) // F-0434: never list another backend's work
	work := "- none"
	if ours {
		work = activeWorkLines(cfg.ProjectID)
	}
	text := hooks.SessionRules(cfg.Gates.GauntletEnabled()) + "\n\nACTIVE WORK RIGHT NOW (cerne work list):\n" + work
	if stale != "" {
		text += "\n\n" + stale
	}
	if missingDocs != "" {
		text += "\n\n" + missingDocs
	}
	if in.Source == "compact" && ours {
		if block := compactBlock(dir, in.SessionID); block != "" {
			text += "\n\n" + block
		}
	}
	if ours {
		if block := channelBlock(cfg.ProjectID, in.SessionID, true); block != "" {
			text += "\n\n" + block
		}
	}

	enc := json.NewEncoder(cmd.OutOrStdout())
	var payload any
	if format == "cursor" {
		payload = cursorContext{AdditionalContext: text}
	} else if format == "copilot" {
		payload = copilotContext{AdditionalContext: text}
	} else {
		var sc sessionContext
		sc.HookSpecificOutput.HookEventName = "SessionStart"
		sc.HookSpecificOutput.AdditionalContext = text
		payload = sc
	}
	if err := enc.Encode(payload); err != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), err)
	}
}

// activeWorkLines renders the live activity roster from the local backend
// clone — "- none" when empty or unreadable (a session must start even when
// the clone is missing).
func activeWorkLines(projectID string) string {
	cacheDir, err := project.CacheDir(projectID)
	if err != nil {
		return "- none"
	}
	works, err := store.LoadAllWorks(filepath.Join(cacheDir, "repo"))
	if err != nil {
		return "- none"
	}
	var lines []string
	for _, w := range works {
		if w.State != domain.WorkActive {
			continue
		}
		lines = append(lines, "- "+w.ID+" — "+w.Title+" ("+w.Actor+")")
	}
	if len(lines) == 0 {
		return "- none"
	}
	return strings.Join(lines, "\n")
}

// handoffNextRunes caps the next step in the compact block, like a channel
// message body.
const handoffNextRunes = 280

// compactBlock is the post-compaction handoff pointer (F-0368 crit. 12) for a
// session bound to an active W-: its latest HO and how to read it, or the one
// line telling how to record one. "" when unbound.
func compactBlock(dir, sess string) string {
	_, repoDir, workID, ok := boundActiveWork(dir, sess)
	if !ok {
		return ""
	}
	h, ok, err := latestHandoff(repoDir, workID)
	if err != nil { // unreadable store: the W- only, no guess about its HO
		return fmt.Sprintf("AFTER COMPACTION — this session is bound to %s.", workID)
	}
	if !ok {
		return fmt.Sprintf("AFTER COMPACTION — this session is bound to %s, with no handoff yet: `cerne work handoff %s --next \"…\"` lets the next stretch, or another session, resume by the code.", workID, workID)
	}
	return fmt.Sprintf("AFTER COMPACTION — this session is bound to %s.\nLatest handoff: %s %s %s — next: %s\nRead it first: cerne show %s",
		workID, h.ID, h.State, store.ShortAge(time.Since(h.CreatedAt)), app.TruncateBody(h.Next, handoffNextRunes, 4*handoffNextRunes), h.ID)
}

// copilotStaleNotice is the F-0405 session-start check over the Copilot files:
// only the Copilot entries of the shared stale lists, so a Copilot-only
// project is not told about the Claude Code skills it never installed.
func copilotStaleNotice(projDir string) string {
	var names []string
	for _, n := range append(hooks.Stale(projDir), skills.Stale(projDir)...) {
		if strings.Contains(n, "copilot") {
			names = append(names, n)
		}
	}
	if len(names) == 0 {
		return ""
	}
	return "cerne: managed Copilot file(s) out of date with this cerne build: " + strings.Join(names, ", ") + " — run `cerne hook install --provider copilot` and `cerne skill install`."
}
