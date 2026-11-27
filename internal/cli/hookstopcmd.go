package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/cost"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/project"
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

// cursorFollowup is Cursor's native stop envelope: the message is submitted
// as the next user turn, the equivalent of Claude Code's Stop context (F-0428).
type cursorFollowup struct {
	FollowupMessage string `json:"followup_message"`
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
	// Cursor auto-submits the reminder as the next user message (F-0428):
	// never after a turn the human aborted, or one that errored.
	if in.cursor() && in.Cursor.Status != "completed" {
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
	// F-0527: dedup between .cursor/hooks.json and .claude/settings.json.
	if !claimCursorHookCall(cacheDir, "stop-check", in) {
		return
	}
	repoDir, err := guardedRepoDir(cacheDir, cfg, projDir)
	if err != nil {
		return // F-0434
	}
	_ = app.TouchBinding(cacheDir, in.SessionID, "") // F-0361: sign of life, one stat + one chtimes, local
	// The mode the session goes idle in (F-0491).
	recordSessionMode(cacheDir, in)

	// F-0309: keep the session's live features' cost snapshot current, so a
	// pruned transcript does not lose it before the stamp. Silent, fail-open.
	// ponytail: a changed transcript is re-read whole each Stop (~95 ms for a 30 MB main file); a byte-offset tail if Stop latency shows up.
	_ = cost.Harvest(cost.Inputs{CacheDir: cacheDir, RepoDir: repoDir, Roots: claudeRoots()}, in.SessionID, in.TranscriptPath, time.Now().UTC())

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

	var out any
	if in.cursor() {
		out = cursorFollowup{FollowupMessage: strings.Join(parts, "\n\n")}
	} else {
		var sc stopContext
		sc.HookSpecificOutput.HookEventName = "Stop"
		sc.HookSpecificOutput.AdditionalContext = strings.Join(parts, "\n\n")
		out = sc
	}
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
// binding, at most once per (session, binding) (F-0294) — per (session,
// binding, compaction count) once the session compacted, when the line asking
// for a handoff joins it (F-0368); mark records it.
func stopCloseout(cacheDir, repoDir, sess string) (string, func()) {
	id, ok, err := app.CurrentBinding(cacheDir, repoDir, sess, "")
	if err != nil || !ok {
		return "", nil
	}

	var msg string
	key := id // the once-per marker: the binding, plus the compaction count when the handoff line goes out (F-0368)
	switch {
	case strings.HasPrefix(id, "W-"):
		msg = fmt.Sprintf("cerne: work %s is still active as this turn ends. Close it — `cerne work done %s --note \"<outcome>\"` (or `cerne work abandon %s --reason \"...\"`) — or leave it open deliberately if the work continues next turn.", id, id, id)
		if n, last := sessionCompactions(cacheDir, sess, id); n > 0 {
			if h, ok, err := latestHandoff(repoDir, id); err == nil && (!ok || !h.CreatedAt.After(last)) {
				key = fmt.Sprintf("%s %d", id, n)
				msg += fmt.Sprintf("\nThis session compacted and %s has no handoff since: `cerne work handoff %s --next \"…\"` lets the next stretch, or another session, resume by the code.", id, id)
			}
		}
	case domain.IsFeatureID(id):
		msg = fmt.Sprintf("cerne: feature %s is in development as this turn ends. Before stopping, reflect finished tasks in plan.md, accumulate evidence in result.md, and `cerne sync` — the board should show real progress, not the last session's snapshot.", id)
	default:
		return "", nil
	}

	// CurrentBinding already rejected an unsafe session id, so it is a safe
	// file name. A marker read/write failure never suppresses the reminder.
	marker := filepath.Join(cacheDir, stopRemindedSubdir, sess)
	// A counted marker of the same binding already covers its plain closeout.
	if prev, err := os.ReadFile(marker); err == nil {
		if p := strings.TrimSpace(string(prev)); p == key || key == id && strings.HasPrefix(p, id+" ") {
			return "", nil
		}
	}
	return msg, func() {
		if err := os.MkdirAll(filepath.Dir(marker), 0o755); err == nil {
			_ = os.WriteFile(marker, []byte(key+"\n"), 0o644)
		}
	}
}
