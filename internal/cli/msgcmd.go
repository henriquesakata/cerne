package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/project"
)

// `cerne msg` (F-0325): the shortcut to the project's permanent channel. No
// room id, no open/join/close — the channel exists by sending to it, and the
// sender speaks as its live work. Local-only like the hooks: it reads the
// cache and the backend clone, never pulls.

func newMsgCmd() *cobra.Command {
	var dir, session string
	cmd := &cobra.Command{
		Use:   "msg",
		Short: "Talk on the project's permanent channel (no room id, no open/close)",
		Long: "Quick coordination between agents with open work and the human: 'msg send' posts as your " +
			"session's live work, 'msg read' shows the whole live channel with each message's delivery state, " +
			"'msg ack' silences a message addressed to you without replying. Pending messages also reach you at " +
			"session start, in subagents, in 'next', 'status --active' and before every Edit/Write. Messages " +
			"expire after 24h, or when the sender's work closes.",
	}
	cmd.PersistentFlags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	cmd.PersistentFlags().StringVar(&session, "session", "", "agent session id (defaults to $CERNE_SESSION, exported by the SessionStart hook)")
	cmd.AddCommand(newMsgSendCmd(&dir, &session), newMsgReadCmd(&dir, &session), newMsgAckCmd(&dir, &session))
	return cmd
}

// msgProject resolves the project's cache and backend clone dirs from
// .cernerc alone (no pull), like the room commands.
func msgProject(dir string) (cacheDir, repoDir string, err error) {
	cfg, _, err := project.LoadConfig(dir)
	if err != nil {
		return "", "", err
	}
	cacheDir, err = project.CacheDir(cfg.ProjectID)
	if err != nil {
		return "", "", err
	}
	return cacheDir, filepath.Join(cacheDir, "repo"), nil
}

func msgSession(flag string) string {
	if flag != "" {
		return flag
	}
	return os.Getenv("CERNE_SESSION")
}

// msgIdentityErr turns the identity sentinels into the next step to take.
func msgIdentityErr(sess string, err error) error {
	switch {
	case sess == "":
		return errors.New("no session: pass --session or set CERNE_SESSION (the SessionStart hook exports it — re-run `cerne hook install` if it is missing)")
	case errors.Is(err, app.ErrNoChannelIdentity):
		return fmt.Errorf("%w — run `cerne work start \"<what you are doing>\"` first", err)
	}
	return err
}

func newMsgSendCmd(dir, session *string) *cobra.Command {
	return &cobra.Command{
		Use:   "send <text>",
		Short: "Post to the permanent channel as your session's work",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cacheDir, repoDir, err := msgProject(*dir)
			if err != nil {
				return err
			}
			sess := msgSession(*session)
			m, to, err := app.ChannelSend(cacheDir, repoDir, sess, args[0], time.Now().UTC())
			if err != nil {
				return msgIdentityErr(sess, err)
			}
			out := cmd.OutOrStdout()
			switch len(to) {
			case 0:
				fmt.Fprintf(out, "sent #%d — no live session right now; it stays in the channel until it expires (24h, or when %s closes)\n", m.Seq, m.Nick)
			case 1:
				fmt.Fprintf(out, "sent #%d — 1 work with a live session will receive it: %s\n", m.Seq, to[0])
			default:
				fmt.Fprintf(out, "sent #%d — %d works with a live session will receive it: %s\n", m.Seq, len(to), strings.Join(to, ", "))
			}
			return nil
		},
	}
}

func newMsgReadCmd(dir, session *string) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "read",
		Short: "Show the whole live channel, with each message's delivery state",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cacheDir, repoDir, err := msgProject(*dir)
			if err != nil {
				return err
			}
			msgs, roster, err := app.ChannelRead(cacheDir, repoDir, msgSession(*session), time.Now().UTC())
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if asJSON {
				if msgs == nil {
					msgs = []app.ChannelMessage{}
				}
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(map[string]any{"messages": msgs, "roster": roster})
			}
			members := make([]string, 0, len(roster))
			for _, r := range roster {
				if r.Live {
					members = append(members, r.Work+" (live)")
				} else {
					members = append(members, r.Work)
				}
			}
			if len(members) == 0 {
				members = append(members, "no active work")
			}
			fmt.Fprintf(out, "channel — roster: %s\n", strings.Join(members, ", "))
			if len(msgs) == 0 {
				fmt.Fprintln(out, "no live messages")
			}
			for _, m := range msgs {
				who := m.Nick
				if m.Human {
					who += " (human)"
				}
				if m.Directed {
					targets := make([]string, len(m.Targets))
					for i, t := range m.Targets {
						targets[i] = t.Work
						switch {
						case t.Closed:
							targets[i] += " (closed)"
						case t.Acked:
							targets[i] += " (acked)"
						}
					}
					who += " → " + strings.Join(targets, ",")
				}
				fmt.Fprintf(out, "#%d %s [%s] %s\n", m.Seq, who, m.State, strings.Join(strings.Fields(m.Body), " "))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "machine-readable output: messages (state, targets) and roster")
	return cmd
}

func newMsgAckCmd(dir, session *string) *cobra.Command {
	return &cobra.Command{
		Use:   "ack <seq>...",
		Short: "Silence messages addressed to you without replying (a reply on the channel also counts)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			seqs := make([]int, len(args))
			for i, a := range args {
				n, err := strconv.Atoi(strings.TrimPrefix(a, "#"))
				if err != nil || n < 1 {
					return fmt.Errorf("invalid message number %q", a)
				}
				seqs[i] = n
			}
			cacheDir, repoDir, err := msgProject(*dir)
			if err != nil {
				return err
			}
			sess := msgSession(*session)
			acked, notes, err := app.ChannelAck(cacheDir, repoDir, sess, seqs, time.Now().UTC())
			if err != nil {
				return msgIdentityErr(sess, err)
			}
			out := cmd.OutOrStdout()
			for _, n := range notes {
				fmt.Fprintln(out, n)
			}
			if len(acked) > 0 {
				labels := make([]string, len(acked))
				for i, n := range acked {
					labels[i] = "#" + strconv.Itoa(n)
				}
				fmt.Fprintf(out, "acked %s\n", strings.Join(labels, " "))
			}
			return nil
		},
	}
}
