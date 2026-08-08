package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"

	"github.com/spf13/cobra"
	"gitlab.com/rockt/cerne/internal/room"
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
	cmd.AddCommand(newRoomFacilitatorCmd())
	cmd.AddCommand(newRoomProposeCmd())
	cmd.AddCommand(newRoomRequestDecisionCmd())
	cmd.AddCommand(newRoomPendingCmd())
	cmd.AddCommand(newRoomResolveCmd())
	cmd.AddCommand(newRoomMinutesCmd())
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
			res, err := c.open(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			rm := res.Room
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "room %s opened: %s\n", rm.ID, rm.Topic)
			fmt.Fprintf(out, "entry command: cerne room join %s --nick <nick> [--bio \"...\"]\n", rm.ID)
			fmt.Fprintf(out, "listen: cerne room read %s --since 0 --once\n", rm.ID)
			fmt.Fprintf(out, "facilitator capability: %s\n", res.FacilitatorID)
			fmt.Fprintf(out, "keep this — it is required to resolve items and close the room; hand it off with 'cerne room facilitator handoff'\n")
			fmt.Fprintf(out, "env: export CERNE_ROOM_FACILITATOR=%s\n", res.FacilitatorID)
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
			res, err := c.join(cmd.Context(), args[0], nick, bio)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "joined %s as %s (%d participant(s))\n", res.Room.ID, nick, len(res.Room.Participants))
			fmt.Fprintf(out, "session: %s\n", res.Session.SessionID)
			fmt.Fprintf(out, "keep this session token — losing it locks the nick until the room closes\n")
			fmt.Fprintf(out, "say: cerne room say %s \"<message>\" --session %s\n", res.Room.ID, res.Session.SessionID)
			fmt.Fprintf(out, "env: export CERNE_ROOM_SESSION=%s\n", res.Session.SessionID)
			return nil
		},
	}
	cmd.Flags().StringVar(&nick, "nick", "", "your nickname in the room (required)")
	cmd.Flags().StringVar(&bio, "bio", "", "short profile shown to other participants")
	addRoomFlags(cmd, &dir, &port)
	return cmd
}

func newRoomSayCmd() *cobra.Command {
	var dir, session string
	var port int
	cmd := &cobra.Command{
		Use:   "say <room> <message>",
		Short: "Send a message to a war room (authored by your session capability)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			// --session wins over the env var; the variable is a single-room
			// convenience, not multi-room storage (F-0226 criterion 6).
			if session == "" {
				session = os.Getenv("CERNE_ROOM_SESSION")
			}
			if session == "" {
				return errors.New("a session is required: pass --session or set CERNE_ROOM_SESSION (run 'cerne room join <room>' to obtain one)")
			}
			c, err := newRoomClient(dir, port)
			if err != nil {
				return err
			}
			_, err = c.say(cmd.Context(), args[0], session, args[1])
			return err
		},
	}
	cmd.Flags().StringVar(&session, "session", "", "session capability printed by 'cerne room join' (falls back to CERNE_ROOM_SESSION)")
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
			switch {
			case m.Type == "message":
				fmt.Fprintf(out, "#%d [%s] %s\n", m.Seq, m.Nick, m.Body)
			case m.Type == "join" && m.Bio != "":
				fmt.Fprintf(out, "#%d [join: %s — %s]\n", m.Seq, m.Nick, m.Bio)
			case m.Type == "join":
				fmt.Fprintf(out, "#%d [join: %s]\n", m.Seq, m.Nick)
			default:
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
	var dir, facilitator, minutesPath string
	var port int
	var allowOpenItems bool
	cmd := &cobra.Command{
		Use:   "close <room>",
		Short: "Close a war room with structured minutes (say/join refused; read still works)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if facilitator == "" {
				facilitator = os.Getenv("CERNE_ROOM_FACILITATOR")
			}
			if facilitator == "" {
				return errors.New("a facilitator capability is required: pass --facilitator or set CERNE_ROOM_FACILITATOR (printed by 'cerne room open')")
			}
			if minutesPath == "" {
				return errors.New("--minutes <path|-|{...}> is required (JSON; run 'cerne room minutes template' for a blank one)")
			}
			minutes, err := readMinutesJSON(minutesPath)
			if err != nil {
				return fmt.Errorf("--minutes: %w", err)
			}
			c, err := newRoomClient(dir, port)
			if err != nil {
				return err
			}
			rm, err := c.close(cmd.Context(), args[0], facilitator, minutes, allowOpenItems)
			if err != nil {
				pending, pendErr := c.pending(cmd.Context(), args[0], facilitator)
				if pendErr == nil && len(pending) > 0 {
					out := cmd.OutOrStdout()
					fmt.Fprintf(out, "close refused — %d open facilitation item(s):\n", len(pending))
					for _, it := range pending {
						fmt.Fprintf(out, "  %s [%s] %s (by %s, #%d)\n", it.ID, it.Kind, it.Title, it.Author, it.SourceSeq)
					}
					fmt.Fprintf(out, "resolve them, or pass --allow-open-items to close anyway (recorded as an exception in the minutes)\n")
				}
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "room %s closed\n", rm.ID)
			return nil
		},
	}
	cmd.Flags().StringVar(&facilitator, "facilitator", "", "facilitator capability printed by 'cerne room open' (falls back to CERNE_ROOM_FACILITATOR)")
	cmd.Flags().StringVar(&minutesPath, "minutes", "", "path to a JSON file with the meeting minutes, or - for stdin (required)")
	cmd.Flags().BoolVar(&allowOpenItems, "allow-open-items", false, "close even with open facilitation items (recorded as an exception in the minutes)")
	addRoomFlags(cmd, &dir, &port)
	return cmd
}

// readMinutesJSON decodes a room.Minutes from src: a literal JSON object
// (starts with "{"), "-" for stdin, or a file path otherwise.
func readMinutesJSON(src string) (room.Minutes, error) {
	var r io.Reader
	switch {
	case strings.HasPrefix(strings.TrimSpace(src), "{"):
		r = strings.NewReader(src)
	case src == "-":
		r = os.Stdin
	default:
		f, err := os.Open(src)
		if err != nil {
			return room.Minutes{}, err
		}
		defer f.Close()
		r = f
	}
	var mn room.Minutes
	if err := json.NewDecoder(r).Decode(&mn); err != nil {
		return room.Minutes{}, err
	}
	return mn, nil
}

func newRoomFacilitatorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "facilitator",
		Short: "Manage the room's facilitator capability",
	}
	cmd.AddCommand(newRoomFacilitatorHandoffCmd())
	return cmd
}

func newRoomFacilitatorHandoffCmd() *cobra.Command {
	var dir, facilitator string
	var port int
	cmd := &cobra.Command{
		Use:   "handoff <room>",
		Short: "Rotate the facilitator capability, transferring the role to a new bearer",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if facilitator == "" {
				facilitator = os.Getenv("CERNE_ROOM_FACILITATOR")
			}
			if facilitator == "" {
				return errors.New("a facilitator capability is required: pass --facilitator or set CERNE_ROOM_FACILITATOR")
			}
			c, err := newRoomClient(dir, port)
			if err != nil {
				return err
			}
			next, err := c.handoffFacilitator(cmd.Context(), args[0], facilitator)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "facilitator capability rotated — the previous one is now invalid\n")
			fmt.Fprintf(out, "new facilitator capability: %s\n", next)
			fmt.Fprintf(out, "transfer it to the new facilitator out of band (it is never stored in the room transcript)\n")
			fmt.Fprintf(out, "env: export CERNE_ROOM_FACILITATOR=%s\n", next)
			return nil
		},
	}
	cmd.Flags().StringVar(&facilitator, "facilitator", "", "current facilitator capability (falls back to CERNE_ROOM_FACILITATOR)")
	addRoomFlags(cmd, &dir, &port)
	return cmd
}

func newRoomItemCmd(use, short, kind string) *cobra.Command {
	var dir, session, title, body string
	var port int
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if session == "" {
				session = os.Getenv("CERNE_ROOM_SESSION")
			}
			if session == "" {
				return errors.New("a session is required: pass --session or set CERNE_ROOM_SESSION (run 'cerne room join <room>' to obtain one)")
			}
			if title == "" {
				return errors.New("--title is required")
			}
			c, err := newRoomClient(dir, port)
			if err != nil {
				return err
			}
			item, err := c.createItem(cmd.Context(), args[0], session, kind, title, body)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s created (from #%d)\n", item.ID, item.SourceSeq)
			return nil
		},
	}
	cmd.Flags().StringVar(&session, "session", "", "session capability printed by 'cerne room join' (falls back to CERNE_ROOM_SESSION)")
	cmd.Flags().StringVar(&title, "title", "", "short title (required)")
	cmd.Flags().StringVar(&body, "body", "", "description")
	addRoomFlags(cmd, &dir, &port)
	return cmd
}

func newRoomProposeCmd() *cobra.Command {
	return newRoomItemCmd("propose <room>", "Raise a proposal for the facilitator to triage", "proposal")
}

func newRoomRequestDecisionCmd() *cobra.Command {
	return newRoomItemCmd("request-decision <room>", "Raise a decision request for the facilitator to triage", "decision_request")
}

func newRoomPendingCmd() *cobra.Command {
	var dir, facilitator string
	var port int
	cmd := &cobra.Command{
		Use:   "pending <room>",
		Short: "List the room's open facilitation items (the facilitator's triage queue)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if facilitator == "" {
				facilitator = os.Getenv("CERNE_ROOM_FACILITATOR")
			}
			if facilitator == "" {
				return errors.New("a facilitator capability is required: pass --facilitator or set CERNE_ROOM_FACILITATOR")
			}
			c, err := newRoomClient(dir, port)
			if err != nil {
				return err
			}
			items, err := c.pending(cmd.Context(), args[0], facilitator)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(items) == 0 {
				fmt.Fprintln(out, "no pending items")
				return nil
			}
			for _, it := range items {
				fmt.Fprintf(out, "%s [%s] %s — %s (by %s, from #%d)\n", it.ID, it.Kind, it.Title, it.Body, it.Author, it.SourceSeq)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&facilitator, "facilitator", "", "facilitator capability (falls back to CERNE_ROOM_FACILITATOR)")
	addRoomFlags(cmd, &dir, &port)
	return cmd
}

func newRoomResolveCmd() *cobra.Command {
	var dir, facilitator, result string
	var port int
	cmd := &cobra.Command{
		Use:   "resolve <room> <item-id>",
		Short: "Resolve an open facilitation item",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if facilitator == "" {
				facilitator = os.Getenv("CERNE_ROOM_FACILITATOR")
			}
			if facilitator == "" {
				return errors.New("a facilitator capability is required: pass --facilitator or set CERNE_ROOM_FACILITATOR")
			}
			c, err := newRoomClient(dir, port)
			if err != nil {
				return err
			}
			item, err := c.resolve(cmd.Context(), args[0], facilitator, args[1], result)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s resolved: %s\n", item.ID, item.Result)
			return nil
		},
	}
	cmd.Flags().StringVar(&facilitator, "facilitator", "", "facilitator capability (falls back to CERNE_ROOM_FACILITATOR)")
	cmd.Flags().StringVar(&result, "result", "", "the resolution")
	addRoomFlags(cmd, &dir, &port)
	return cmd
}

func newRoomMinutesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "minutes",
		Short: "Work with a room's structured closing minutes",
	}
	cmd.AddCommand(newRoomMinutesTemplateCmd())
	cmd.AddCommand(newRoomMinutesShowCmd())
	return cmd
}

func newRoomMinutesTemplateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "template",
		Short: "Print a blank minutes JSON scaffold, ready for 'room close --minutes'",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(room.Minutes{
				DiscardedOptions: []string{},
				Owners:           []string{},
				NextSteps:        []string{},
			})
		},
	}
}

func newRoomMinutesShowCmd() *cobra.Command {
	var dir, format string
	var port int
	cmd := &cobra.Command{
		Use:   "show <room>",
		Short: "Print a closed room's minutes",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if format != "markdown" {
				return errors.New(`--format must be "markdown" (the only supported projection today)`)
			}
			c, err := newRoomClient(dir, port)
			if err != nil {
				return err
			}
			rooms, err := c.list(cmd.Context())
			if err != nil {
				return err
			}
			for _, rm := range rooms {
				if rm.ID != args[0] {
					continue
				}
				if rm.ClosedAt == nil {
					return fmt.Errorf("room %s is still open — it has no minutes yet", rm.ID)
				}
				fmt.Fprint(cmd.OutOrStdout(), room.RenderMinutesMarkdown(rm))
				return nil
			}
			return fmt.Errorf("room %s not found", args[0])
		},
	}
	cmd.Flags().StringVar(&format, "format", "markdown", "output format")
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
