package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"

	"github.com/spf13/cobra"
)

// roomFlags are the flags every room subcommand shares: --dir (project,
// hidden like resolveProject's) and --port (the running `cerne ui`, same
// default/flag name as doctorcmd/uicmd).
func addRoomFlags(cmd *cobra.Command, dir *string, port *int) {
	cmd.Flags().StringVar(dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.Flags().MarkHidden("dir")
	cmd.Flags().IntVar(port, "port", 7717, "port of the running cerne ui server")
}

func newRoomCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "room",
		Short: "Talk to the local war room (F-0211): open/join/say/read/close/list",
	}
	cmd.AddCommand(newRoomOpenCmd())
	cmd.AddCommand(newRoomJoinCmd())
	cmd.AddCommand(newRoomSayCmd())
	cmd.AddCommand(newRoomReadCmd())
	cmd.AddCommand(newRoomCloseCmd())
	cmd.AddCommand(newRoomListCmd())
	return cmd
}

func newRoomOpenCmd() *cobra.Command {
	var dir string
	var port int
	cmd := &cobra.Command{
		Use:   "open <topic>",
		Short: "Open a new war room",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newRoomClient(dir, port)
			if err != nil {
				return err
			}
			rm, err := c.open(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "room %s opened: %s\n", rm.ID, rm.Topic)
			fmt.Fprintf(out, "entry command: cerne room join %s --nick <nick> [--bio \"...\"]\n", rm.ID)
			fmt.Fprintf(out, "listen: cerne room read %s --since 0 --once\n", rm.ID)
			return nil
		},
	}
	addRoomFlags(cmd, &dir, &port)
	return cmd
}

func newRoomJoinCmd() *cobra.Command {
	var dir, nick, bio string
	var port int
	cmd := &cobra.Command{
		Use:   "join <room>",
		Short: "Join a war room with a unique nickname",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if nick == "" {
				return errors.New("--nick is required")
			}
			c, err := newRoomClient(dir, port)
			if err != nil {
				return err
			}
			rm, err := c.join(cmd.Context(), args[0], nick, bio)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "joined %s as %s (%d participant(s))\n", rm.ID, nick, len(rm.Participants))
			return nil
		},
	}
	cmd.Flags().StringVar(&nick, "nick", "", "your nickname in the room (required)")
	cmd.Flags().StringVar(&bio, "bio", "", "short profile shown to other participants")
	addRoomFlags(cmd, &dir, &port)
	return cmd
}

func newRoomSayCmd() *cobra.Command {
	var dir, nick string
	var port int
	cmd := &cobra.Command{
		Use:   "say <room> <message>",
		Short: "Send a message to a war room",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if nick == "" {
				return errors.New("--nick is required")
			}
			c, err := newRoomClient(dir, port)
			if err != nil {
				return err
			}
			_, err = c.say(cmd.Context(), args[0], nick, args[1])
			return err
		},
	}
	cmd.Flags().StringVar(&nick, "nick", "", "your nickname in the room (required)")
	addRoomFlags(cmd, &dir, &port)
	return cmd
}

func newRoomReadCmd() *cobra.Command {
	var dir string
	var port, since int
	var follow, once bool
	cmd := &cobra.Command{
		Use:   "read <room>",
		Short: "Read a war room's transcript, optionally following new messages",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if once && follow {
				return errors.New("--once and --follow are mutually exclusive")
			}
			c, err := newRoomClient(dir, port)
			if err != nil {
				return err
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
			defer stop()
			return roomReadLoop(ctx, cmd.OutOrStdout(), c, args[0], since, follow, once)
		},
	}
	cmd.Flags().IntVar(&since, "since", 0, "only messages after this sequence number")
	cmd.Flags().BoolVar(&follow, "follow", false, "block for new messages (long-poll) until Ctrl+C")
	cmd.Flags().BoolVar(&once, "once", false, "block (long-poll) until at least one new event, print it, exit 0 — wakeup for turn-based agents")
	addRoomFlags(cmd, &dir, &port)
	return cmd
}

// roomReadLoop prints the transcript from since onward. With follow it keeps
// calling read (each call already blocks server-side up to its long-poll
// timeout) until ctx is done, advancing since past every message printed.
// With once it long-polls (same server mechanism as follow) and returns as
// soon as it has printed a non-empty batch — a room's close event is just
// another typed Message on the stream, so it needs no special handling here.
func roomReadLoop(ctx context.Context, out io.Writer, c roomClient, id string, since int, follow, once bool) error {
	for {
		msgs, err := c.read(ctx, id, since, follow || once)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		for _, m := range msgs {
			if m.Type == "message" {
				fmt.Fprintf(out, "#%d [%s] %s\n", m.Seq, m.Nick, m.Body)
			} else {
				fmt.Fprintf(out, "#%d [%s]\n", m.Seq, m.Type)
			}
			since = m.Seq
		}
		if once {
			if len(msgs) > 0 {
				return nil
			}
		} else if !follow {
			return nil
		}
		if ctx.Err() != nil {
			return nil
		}
	}
}

func newRoomCloseCmd() *cobra.Command {
	var dir string
	var port int
	cmd := &cobra.Command{
		Use:   "close <room>",
		Short: "Close a war room (say/join refused; read still works)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newRoomClient(dir, port)
			if err != nil {
				return err
			}
			rm, err := c.close(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "room %s closed\n", rm.ID)
			return nil
		},
	}
	addRoomFlags(cmd, &dir, &port)
	return cmd
}

func newRoomListCmd() *cobra.Command {
	var dir string
	var port int
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List war rooms",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newRoomClient(dir, port)
			if err != nil {
				return err
			}
			rooms, err := c.list(cmd.Context())
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(rooms) == 0 {
				fmt.Fprintln(out, "no rooms")
				return nil
			}
			for _, rm := range rooms {
				status := "open"
				if rm.ClosedAt != nil {
					status = "closed"
				}
				fmt.Fprintf(out, "%s  %-6s %d participant(s)  %s\n", rm.ID, status, len(rm.Participants), rm.Topic)
			}
			return nil
		},
	}
	addRoomFlags(cmd, &dir, &port)
	return cmd
}
