package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/hooks"
	"gitlab.com/rockt/cerne/internal/project"
	"gitlab.com/rockt/cerne/internal/store"
)

// Session context plumbing (F-0174). Emits the canonical registration rules
// plus the LIVE work list in the envelope of the requested provider — the
// SessionStart injection every provider gets, so an agent on Cursor, Codex or
// Gemini starts a session as informed as one on Claude Code. Local-only (the
// work list comes from the backend clone) and fail-open: outside a cerne
// project, or on any error loading works, it degrades gracefully.

// cursorContext is Cursor's native sessionStart output envelope.
type cursorContext struct {
	AdditionalContext string `json:"additional_context"`
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
	cmd.Flags().StringVar(&format, "format", "claude", "output envelope: claude|codex|cursor|gemini")
	return cmd
}

func runHookSessionContext(cmd *cobra.Command, format string) {
	in, _ := decodeHookInput(cmd.InOrStdin()) // best effort: some providers send no payload
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
	cfg, _, err := project.LoadConfig(dir)
	if err != nil {
		return // not a cerne project: never inject
	}

	text := hooks.RegisterRules + "\n\nACTIVE WORK RIGHT NOW (cerne work list):\n" + activeWorkLines(cfg.ProjectID)
	if block := channelBlock(cfg.ProjectID, in.SessionID, true); block != "" {
		text += "\n\n" + block
	}

	enc := json.NewEncoder(cmd.OutOrStdout())
	var payload any
	if format == "cursor" {
		payload = cursorContext{AdditionalContext: text}
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
