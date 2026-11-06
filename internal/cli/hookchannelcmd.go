package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/project"
)

// Channel contact-point plumbing (F-0325), run by the SessionStart and
// SubagentStart scripts with the hook payload on stdin. 100% local (cache dir
// + backend clone, no network) and fail-open like every hook subcommand.

// envSessionRe admits only session ids that are inert when CLAUDE_ENV_FILE is
// sourced by bash.
var envSessionRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

func newHookChannelCmd() *cobra.Command {
	var point string
	cmd := &cobra.Command{
		Use:    "channel",
		Short:  "SessionStart/SubagentStart plumbing: export CERNE_SESSION and deliver pending channel messages (F-0325)",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			runHookChannel(cmd, point)
			return nil
		},
	}
	cmd.Flags().StringVar(&point, "point", "session-start", "contact point: session-start|subagent")
	return cmd
}

func runHookChannel(cmd *cobra.Command, point string) {
	in, err := decodeHookInput(cmd.InOrStdin())
	if err != nil {
		return
	}
	// A subagent shares its parent's session id and a context that will be
	// discarded: it peeks, so consuming there never steals the parent's
	// background.
	consume := point == "session-start"
	if consume {
		exportSession(in.SessionID)
	} else if point != "subagent" {
		return
	}
	cfg, _, err := project.LoadConfig(in.CWD)
	if err != nil {
		return
	}
	fmt.Fprint(cmd.OutOrStdout(), channelBlock(cfg.ProjectID, in.SessionID, consume))
}

// channelBlock is session sess's pending channel block for the project, ""
// when there is nothing to deliver or on any error (fail-open, N-0021).
func channelBlock(projectID, sess string, consume bool) string {
	cacheDir, err := project.CacheDir(projectID)
	if err != nil {
		return ""
	}
	block, err := app.ChannelInject(cacheDir, filepath.Join(cacheDir, "repo"), sess, consume, time.Now().UTC())
	if err != nil {
		return ""
	}
	return block
}

// exportSession appends `export CERNE_SESSION=<id>` to CLAUDE_ENV_FILE, which
// Claude Code sources before every Bash call of the session (R-0034): the
// agent's own `cerne msg|next|status` then know which session they run in.
func exportSession(sess string) {
	path := os.Getenv("CLAUDE_ENV_FILE")
	if path == "" || !envSessionRe.MatchString(sess) {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "export CERNE_SESSION=%s\n", sess)
}
