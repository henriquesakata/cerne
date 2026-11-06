package room

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// The permanent channel (F-0325): one reserved room per project for quick
// agent↔agent↔human coordination, with no open/close/minutes. Unlike the war
// rooms it is authoritative on disk, not in a server's memory: CLI commands,
// hooks (which run without the server) and `cerne ui` all read and append the
// same two jsonl files under a short lockfile. Everything else — watermarks,
// delivery states, expiry, addressing — is derived from them on read.

// ChannelID is the reserved id of the project's permanent channel. It never
// collides with a war room (those are R<n>).
const ChannelID = "channel"

var (
	ErrPermanentRoom  = errors.New("the project's permanent channel has no open/close/minutes")
	ErrChannelBusy    = errors.New("channel is busy")
	ErrMessageTooLong = errors.New("channel message too long")
)

// maxChannelBody keeps every append one small write.
const maxChannelBody = 2000

// Lock tuning: a hook never waits longer than channelLockWait (it fails open
// and the message goes out at the next contact point); a holder that died is
// stolen after channelLockStale.
var (
	channelLockWait  = 300 * time.Millisecond
	channelLockStale = 10 * time.Second
)

// Delivery is one line of the channel's delivery log: session sess (working
// as Work) was shown Seqs at At and has seen everything up to Through. A Peek
// (a subagent's throwaway context) is recorded but delivers nothing: it never
// moves a watermark and never makes a later post an answer. Ack carries
// explicit acknowledgements instead (`cerne msg ack`).
type Delivery struct {
	Session string    `json:"session"`
	Work    string    `json:"work,omitempty"`
	Seqs    []int     `json:"seqs,omitempty"`
	Through int       `json:"through,omitempty"`
	Peek    bool      `json:"peek,omitempty"`
	Ack     []int     `json:"ack,omitempty"`
	At      time.Time `json:"at"`
}

// Channel is the permanent channel's store at <roomsDir>/channel/.
type Channel struct{ dir string }

// OpenChannel addresses the channel under roomsDir without touching disk; the
// dir is created by the first write.
func OpenChannel(roomsDir string) Channel {
	return Channel{dir: filepath.Join(roomsDir, ChannelID)}
}

func (c Channel) messagesPath() string   { return filepath.Join(c.dir, "messages.jsonl") }
func (c Channel) deliveriesPath() string { return filepath.Join(c.dir, "deliveries.jsonl") }
func (c Channel) seqPath() string        { return filepath.Join(c.dir, "seq") }

// Append assigns m the next seq and persists it, under the lock.
func (c Channel) Append(m Message) (Message, error) {
	if utf8.RuneCountInString(m.Body) > maxChannelBody {
		return Message{}, fmt.Errorf("%w (%d runes max)", ErrMessageTooLong, maxChannelBody)
	}
	err := c.withLock(func() error {
		raw, err := readIfExists(c.messagesPath())
		if err != nil {
			return err
		}
		// The high-water mark lives apart from the log (critic C16): a
		// trimmed or compacted log must never hand out a seq again, or every
		// session whose watermark is past it misses the new messages.
		hw, _ := os.ReadFile(c.seqPath())
		last, _ := strconv.Atoi(strings.TrimSpace(string(hw)))
		for _, prev := range parseLines[Message](raw) {
			last = max(last, prev.Seq)
		}
		m.Seq = last + 1
		if err := appendLine(c.messagesPath(), raw, m); err != nil {
			return err
		}
		return os.WriteFile(c.seqPath(), []byte(strconv.Itoa(m.Seq)+"\n"), 0o644)
	})
	if err != nil {
		return Message{}, err
	}
	return m, nil
}

// Messages returns every parseable message; a missing channel is empty.
func (c Channel) Messages() ([]Message, error) {
	raw, err := readIfExists(c.messagesPath())
	return parseLines[Message](raw), err
}

// Deliveries returns every parseable delivery line.
func (c Channel) Deliveries() ([]Delivery, error) {
	raw, err := readIfExists(c.deliveriesPath())
	return parseLines[Delivery](raw), err
}

// Update reads both logs and lets fn decide, under one lock, the delivery to
// append (nil for none) — so two parallel injections into one session never
// deliver the same message twice.
func (c Channel) Update(fn func(msgs []Message, dels []Delivery) (*Delivery, error)) error {
	return c.withLock(func() error {
		msgs, err := c.Messages()
		if err != nil {
			return err
		}
		rawDels, err := readIfExists(c.deliveriesPath())
		if err != nil {
			return err
		}
		d, err := fn(msgs, parseLines[Delivery](rawDels))
		if err != nil || d == nil {
			return err
		}
		return appendLine(c.deliveriesPath(), rawDels, *d)
	})
}

// Version changes whenever either log grows: the long-poll cursor.
func (c Channel) Version() int64 {
	var v int64
	for _, p := range []string{c.messagesPath(), c.deliveriesPath()} {
		if info, err := os.Stat(p); err == nil {
			v += info.Size()
		}
	}
	return v
}

// Wait blocks until Version differs from version, ctx ends or timeout
// elapses, and returns the version it saw last.
func (c Channel) Wait(ctx context.Context, version int64, timeout time.Duration) int64 {
	deadline := time.Now().Add(timeout)
	for {
		if v := c.Version(); v != version || !time.Now().Before(deadline) {
			return v
		}
		select {
		case <-ctx.Done():
			return c.Version()
		case <-time.After(min(500*time.Millisecond, time.Until(deadline))):
		}
	}
}

// withLock runs fn holding <dir>/lock.
// ponytail: third copy of the O_EXCL lockfile pattern (gitback/lock.go,
// project/settings.go); extract internal/fslock when a fourth appears. The
// gitback lock waits up to 150 s, which would hang a hook.
func (c Channel) withLock(fn func() error) error {
	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		return err
	}
	lock := filepath.Join(c.dir, "lock")
	deadline := time.Now().Add(channelLockWait)
	for {
		f, err := os.OpenFile(lock, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			_, _ = fmt.Fprintf(f, "%d\n", os.Getpid())
			_ = f.Close()
			defer os.Remove(lock)
			return fn()
		}
		if !errors.Is(err, fs.ErrExist) {
			return err
		}
		if info, err := os.Stat(lock); err == nil && time.Since(info.ModTime()) > channelLockStale {
			_ = os.Remove(lock) // dead holder
			continue
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%w: %s", ErrChannelBusy, lock)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func readIfExists(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return raw, err
}

// parseLines decodes one JSON value per line, skipping torn or garbage lines.
func parseLines[T any](raw []byte) []T {
	var out []T
	for _, line := range bytes.Split(raw, []byte("\n")) {
		var v T
		if len(bytes.TrimSpace(line)) > 0 && json.Unmarshal(line, &v) == nil {
			out = append(out, v)
		}
	}
	return out
}

// appendLine writes v as one O_APPEND line. prev is the file's current
// content: a torn last line (no newline) is closed first, so the new record
// never fuses with it.
func appendLine(path string, prev []byte, v any) error {
	line, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(prev) > 0 && prev[len(prev)-1] != '\n' {
		line = append([]byte("\n"), line...)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
