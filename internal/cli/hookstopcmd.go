package cli

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/project"
)

// End-of-turn closeout plumbing (F-0171). Stop hook: when the session still
// holds a live registration binding, remind the agent to close or update it
// before the turn ends — the board should never lie overnight. Advisory by
// design (additionalContext, never a blocking decision), 100% local (cache
// dir + local backend clone, the same guarantee as `cerne work current`), and
// fail-open like every hook subcommand.

// stopContext is the Stop additionalContext envelope.
type stopContext struct {
	HookSpecificOutput struct {
		HookEventName     string `json:"hookEventName"`
		AdditionalContext string `json:"additionalContext"`
	} `json:"hookSpecificOutput"`
}

func newHookStopCheckCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "stop-check",
		Short:  "Stop plumbing: end-of-turn closeout reminder for the session's live registration (F-0171)",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			runHookStopCheck(cmd)
			return nil
		},
	}
	return cmd
}

func runHookStopCheck(cmd *cobra.Command) {
	in, err := decodeHookInput(cmd.InOrStdin())
	if err != nil || in.SessionID == "" {
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
	repoDir := filepath.Join(cacheDir, "repo")
	id, ok, err := app.CurrentBinding(cacheDir, repoDir, in.SessionID)
	if err != nil || !ok {
		return
	}

	var msg string
	switch {
	case strings.HasPrefix(id, "W-"):
		msg = fmt.Sprintf("cerne: work %s is still active as this turn ends. Close it — `cerne work done %s --note \"<outcome>\"` (or `cerne work abandon %s --reason \"...\"`) — or leave it open deliberately if the work continues next turn.", id, id, id)
	case strings.HasPrefix(id, "F-"):
		msg = fmt.Sprintf("cerne: feature %s is in development as this turn ends. Before stopping, reflect finished tasks in plan.md, accumulate evidence in result.md, and `cerne sync` — the board should show real progress, not the last session's snapshot.", id)
	default:
		return
	}

	var out stopContext
	out.HookSpecificOutput.HookEventName = "Stop"
	out.HookSpecificOutput.AdditionalContext = msg
	enc := json.NewEncoder(cmd.OutOrStdout())
	if err := enc.Encode(out); err != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), err)
	}
}
