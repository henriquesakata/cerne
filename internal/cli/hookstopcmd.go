package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/project"
)

// End-of-turn closeout plumbing (F-0171). Stop hook: when the session still
// holds a live registration binding, remind the agent to close or update it
// before the turn ends — the board should never lie overnight. Advisory by
// design (additionalContext, never a blocking decision), 100% local (cache
// dir + local backend clone, the same guarantee as `cerne work current`), and
// fail-open like every hook subcommand.
//
// A Stop additionalContext continues the turn, so the reminder is rationed
// (F-0294): silent when stop_hook_active (the stop is our own continuation —
// otherwise it loops), and at most once per (session, binding).

// stopRemindedSubdir holds one marker per session naming the binding it was
// last reminded about. It lives outside sessions/, where every file is read as
// a binding.
const stopRemindedSubdir = "stop-reminded"

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
	if err != nil || in.SessionID == "" || in.StopHookActive {
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

	// Each part decides and rations itself; whatever is due goes out in ONE
	// envelope — a single continuation of the turn, never two.
	var parts []string
	var marks []func()
	if msg, mark := stopCloseout(cacheDir, repoDir, in.SessionID); msg != "" {
		parts, marks = append(parts, msg), append(marks, mark)
	}
	if msg, mark := app.ChannelStopReminder(cacheDir, repoDir, in.SessionID, time.Now().UTC()); msg != "" {
		parts, marks = append(parts, msg), append(marks, mark) // F-0325
	}
	if len(parts) == 0 {
		return
	}

	var out stopContext
	out.HookSpecificOutput.HookEventName = "Stop"
	out.HookSpecificOutput.AdditionalContext = strings.Join(parts, "\n\n")
	enc := json.NewEncoder(cmd.OutOrStdout())
	if err := enc.Encode(out); err != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), err)
		return
	}
	// Marked only once delivered: an undelivered reminder must not count.
	for _, mark := range marks {
		mark()
	}
}

// stopCloseout is the F-0171 closeout reminder for the session's live
// binding, at most once per (session, binding) (F-0294); mark records it.
func stopCloseout(cacheDir, repoDir, sess string) (string, func()) {
	id, ok, err := app.CurrentBinding(cacheDir, repoDir, sess)
	if err != nil || !ok {
		return "", nil
	}

	var msg string
	switch {
	case strings.HasPrefix(id, "W-"):
		msg = fmt.Sprintf("cerne: work %s is still active as this turn ends. Close it — `cerne work done %s --note \"<outcome>\"` (or `cerne work abandon %s --reason \"...\"`) — or leave it open deliberately if the work continues next turn.", id, id, id)
	case domain.IsFeatureID(id):
		msg = fmt.Sprintf("cerne: feature %s is in development as this turn ends. Before stopping, reflect finished tasks in plan.md, accumulate evidence in result.md, and `cerne sync` — the board should show real progress, not the last session's snapshot.", id)
	default:
		return "", nil
	}

	// CurrentBinding already rejected an unsafe session id, so it is a safe
	// file name. A marker read/write failure never suppresses the reminder.
	marker := filepath.Join(cacheDir, stopRemindedSubdir, sess)
	if prev, err := os.ReadFile(marker); err == nil && strings.TrimSpace(string(prev)) == id {
		return "", nil
	}
	return msg, func() {
		if err := os.MkdirAll(filepath.Dir(marker), 0o755); err == nil {
			_ = os.WriteFile(marker, []byte(id+"\n"), 0o644)
		}
	}
}
