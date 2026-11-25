package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/henriquesakata/cerne/internal/project"
	"github.com/spf13/cobra"
)

// roomCheckpoint is the durable cursor for one `room watch` consumer
// (F-0227): one file per room+session, so a watcher restart resumes without
// replaying the whole transcript. The session is stored hashed, never raw.
type roomCheckpoint struct {
	RoomID      string    `json:"room_id"`
	SessionHash string    `json:"session_hash"`
	LastSeq     int       `json:"last_seq"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// checkpointPath is <cache-dir>/room-watch/<room>/<sha256(session)>.json —
// a sibling of the "rooms" dir the server itself uses under project.CacheDir.
func checkpointPath(projectID, roomID, sessionID string) (string, error) {
	if err := checkRoomIDLocal(roomID); err != nil { // B-0084: never a path outside room-watch/
		return "", err
	}
	cacheDir, err := project.CacheDir(projectID)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(sessionID))
	return filepath.Join(cacheDir, "room-watch", roomID, hex.EncodeToString(sum[:])+".json"), nil
}

func hashSessionID(sessionID string) string {
	sum := sha256.Sum256([]byte(sessionID))
	return hex.EncodeToString(sum[:])
}

func loadCheckpoint(path string) (roomCheckpoint, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return roomCheckpoint{}, false, nil
	}
	if err != nil {
		return roomCheckpoint{}, false, err
	}
	var cp roomCheckpoint
	if err := json.Unmarshal(data, &cp); err != nil {
		return roomCheckpoint{}, false, err
	}
	return cp, true, nil
}

// roomCheckpointRename is os.Rename, swappable in tests to simulate a crash
// between the checkpoint write and its atomic commit (spec criterion 8: a
// checkpoint that never lands must leave the previous value in place, so a
// restart repeats the last batch instead of skipping ahead).
var roomCheckpointRename = os.Rename

// saveCheckpoint writes cp atomically (temp file + rename) with 0600
// permissions — the file identifies a room+session pairing, not a secret
// itself (the raw token isn't in it), but there's no reason to make it
// group/world readable.
func saveCheckpoint(path string, cp roomCheckpoint) error {
	data, err := json.Marshal(cp)
	if err != nil {
		return err
	}
	return writePrivateFile(path, data)
}

// writePrivateFile writes data to path atomically (temp file + rename), the
// file 0600 under a 0700 dir: the watch cursor and the stored room
// credential (B-0084).
func writePrivateFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := roomCheckpointRename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}

// errCheckpointLocked is returned when another live `room watch` already
// holds the checkpoint for this room+session (spec criterion 7).
var errCheckpointLocked = errors.New("another 'room watch' is already running for this room and session")

// lockCheckpoint takes a non-blocking exclusive lock on path+".lock": unlike
// gitback.WithLock (which blocks and steals a stale lock for the shared
// backend clone), a second concurrent watcher must fail immediately with a
// clear error rather than wait or steal — two live consumers can't safely
// share one cursor.
//
// The lock is an OS lock on the file (tryLockFile), not the file's existence:
// the kernel drops it when the holder dies, however it dies, so a watch killed
// with SIGKILL or a crash never blocks the session (F-0472). release closes
// the file and leaves it on disk: removing it would let a process that opened
// the old file and one that creates a new one both hold "the" lock.
func lockCheckpoint(path string) (func(), error) {
	lockPath := path + ".lock"
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := tryLockFile(f); err != nil {
		f.Close()
		return nil, err
	}
	return func() { f.Close() }, nil
}

func newRoomWatchCmd() *cobra.Command {
	var dir, session, nick, cursorFile string
	var port, since int
	var once bool
	var settle time.Duration
	cmd := &cobra.Command{
		Use:   "watch <room>",
		Short: "Watch a war room with a durable per-session cursor, filtering your own echo (F-0227)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			roomID := args[0]
			c, err := newRoomClient(dir, port)
			if err != nil {
				return err
			}
			if session, err = resolveRoomSession(session, nick, c.pid, roomID); err != nil {
				return err
			}

			cpPath := cursorFile
			if cpPath == "" {
				cpPath, err = checkpointPath(c.pid, roomID, session)
				if err != nil {
					return err
				}
			}
			// Trap the signals before taking the lock: a watch killed while it
			// holds the lock leaves it on disk, and nothing ever steals it, so
			// the session could not watch again (F-0467). SIGTERM is how an
			// agent harness or a supervisor stops a watcher.
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			release, err := lockCheckpoint(cpPath)
			if err != nil {
				return err
			}
			defer release()

			start, err := resolveWatchStart(ctx, c, roomID, session, cpPath, since, cmd.Flags().Changed("since"))
			if err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return err
			}
			return roomWatchLoop(ctx, cmd.OutOrStdout(), c, roomID, session, cpPath, start, once, settle)
		},
	}
	cmd.Flags().StringVar(&session, "session", "", roomSessionFlagHelp)
	cmd.Flags().StringVar(&nick, "nick", "", roomNickFlagHelp)
	cmd.Flags().IntVar(&since, "since", 0, "override the checkpoint/last_seq and start from this sequence number")
	cmd.Flags().BoolVar(&once, "once", false, "exit 0 after the first relevant (non-echo) event, once --settle passes with nothing new")
	cmd.Flags().DurationVar(&settle, "settle", 1500*time.Millisecond, "with --once, keep printing events until this long passes with no new one, at most 5s after the first; 0 exits on the first batch")
	cmd.Flags().StringVar(&cursorFile, "cursor-file", "", "override the default per-room-per-session checkpoint file path")
	addRoomFlags(cmd, &dir, &port)
	return cmd
}

// resolveWatchStart picks the watch's starting cursor (spec criterion 1):
// an explicit --since wins; else a saved checkpoint; else the room's
// current tail via resume (the same value a fresh `room join` would have
// returned as last_seq).
func resolveWatchStart(ctx context.Context, c roomClient, roomID, session, cpPath string, since int, sinceSet bool) (int, error) {
	if sinceSet {
		return since, nil
	}
	if cp, ok, err := loadCheckpoint(cpPath); err != nil {
		return 0, err
	} else if ok {
		return cp.LastSeq, nil
	}
	sess, err := c.resume(ctx, roomID, session)
	if err != nil {
		return 0, err
	}
	return sess.LastSeq, nil
}

// roomEventReader is the read side of roomClient that roomWatchLoop needs —
// narrow enough to fake in tests without a real HTTP server (backoff timing
// especially wants no real network/sleep involved).
type roomEventReader interface {
	readAsSession(ctx context.Context, id string, since int, sessionID string) ([]roomWatchEvent, error)
}

// Backoff tuning for a transient (connection-level) read failure: doubles
// from roomWatchBackoffBase up to roomWatchBackoffMax, resetting to the base
// after any successful read (empty or not). "Timeout vazio" (a long-poll
// that simply found nothing new) is not a failure and never backs off.
const (
	roomWatchBackoffBase = 250 * time.Millisecond
	roomWatchBackoffMax  = 10 * time.Second
)

func nextRoomWatchBackoff(attempt int) time.Duration {
	if attempt > 6 { // 250ms<<6 = 16s, already past the cap
		attempt = 6
	}
	d := roomWatchBackoffBase << attempt
	if d > roomWatchBackoffMax {
		return roomWatchBackoffMax
	}
	return d
}

// roomWatchSleep waits d or returns early on ctx cancellation (Ctrl+C must
// interrupt a backoff wait, not just an in-flight long-poll). Package var so
// tests can replace real waiting with instant, duration-recording stubs.
var roomWatchSleep = func(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

// roomWatchSettleCap bounds the --settle window from the first relevant
// event, so a room that never goes quiet cannot hold the turn (E-0006).
const roomWatchSettleCap = 5 * time.Second

// roomWatchNow is time.Now, swappable so the settle tests run on a fake clock.
var roomWatchNow = time.Now

// roomWatchLoop long-polls id as session from since, printing every event
// but its own echo (own:true never printed, but still advances the
// checkpoint — criterion 3) and persisting the checkpoint only after each
// event's write succeeds (criterion 2/8). once exits after the first batch
// with at least one printed event — or, with settle > 0, once settle passes
// with no new relevant event, at most roomWatchSettleCap after the first
// (E-0006); a close event always ends the loop, printed and checkpointed,
// regardless of once (criterion 6). A transient read error retries with
// bounded backoff, cursor untouched; a definitive one (errRoomTransient not
// set) returns immediately (criterion 5/6).
func roomWatchLoop(ctx context.Context, out io.Writer, c roomEventReader, id, session, cpPath string, since int, once bool, settle time.Duration) error {
	attempt := 0
	// Both zero until --once prints its first relevant event; from then on
	// each read is bounded by the settle window, which every relevant event
	// pushes back, never past first+roomWatchSettleCap.
	var first, deadline time.Time
	for {
		readCtx, cancel := ctx, context.CancelFunc(func() {})
		if !deadline.IsZero() {
			readCtx, cancel = context.WithDeadline(ctx, deadline)
		}
		events, err := c.readAsSession(readCtx, id, since, session)
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			// Inside the settle window, the deadline running out, or a
			// dropped connection a backoff would carry past the cap, ends the
			// run normally: what it printed is checkpointed and the next
			// --once resumes there (E-0006).
			if !deadline.IsZero() && (errors.Is(err, context.DeadlineExceeded) || errors.Is(err, errRoomTransient)) {
				return nil
			}
			if !errors.Is(err, errRoomTransient) {
				return err
			}
			roomWatchSleep(ctx, nextRoomWatchBackoff(attempt))
			if ctx.Err() != nil {
				return nil
			}
			attempt++
			continue
		}
		attempt = 0
		printedRelevant := false
		for _, ev := range events {
			own := ev.Own != nil && *ev.Own
			if !own {
				if _, err := fmt.Fprintln(out, formatRoomEvent(ev.Message)); err != nil {
					return err
				}
				printedRelevant = true
			}
			since = ev.Seq
			cp := roomCheckpoint{RoomID: id, SessionHash: hashSessionID(session), LastSeq: since, UpdatedAt: time.Now().UTC()}
			if err := saveCheckpoint(cpPath, cp); err != nil {
				return err
			}
			if ev.Type == "close" {
				return nil
			}
		}
		if once && printedRelevant {
			if settle <= 0 {
				return nil
			}
			now := roomWatchNow()
			if first.IsZero() {
				first = now
			}
			deadline = now.Add(settle)
			if limit := first.Add(roomWatchSettleCap); deadline.After(limit) {
				deadline = limit
			}
			if !deadline.After(now) {
				return nil
			}
		}
		if ctx.Err() != nil {
			return nil
		}
	}
}
