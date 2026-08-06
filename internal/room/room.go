// Package room implements the war room primitive (F-0211/T-0001): local,
// in-process chat rooms where CLI-driven agents and the UI converse. State
// lives in memory for the life of the server process; every event is also
// appended to <dir>/<id>.jsonl so a restart can replay the transcript.
package room

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	ErrNotFound  = errors.New("room not found")
	ErrClosed    = errors.New("room is closed")
	ErrNickTaken = errors.New("nickname already in use in this room")
	ErrNotJoined = errors.New("nickname is not a participant of this room")
)

const (
	typeOpen    = "open"
	typeJoin    = "join"
	typeMessage = "message"
	typeClose   = "close"
)

// Room is the metadata + roster snapshot returned to callers.
type Room struct {
	ID           string        `json:"id"`
	Topic        string        `json:"topic"`
	OpenedAt     time.Time     `json:"opened_at"`
	ClosedAt     *time.Time    `json:"closed_at,omitempty"`
	Participants []Participant `json:"participants"`
}

// Participant is one joined nick.
type Participant struct {
	Nick     string    `json:"nick"`
	Bio      string    `json:"bio,omitempty"`
	JoinedAt time.Time `json:"joined_at"`
}

// Message is one line of the room transcript: an open/join/close system
// event or a said message.
type Message struct {
	Seq   int       `json:"seq"`
	Type  string    `json:"type"` // open|join|message|close
	Nick  string    `json:"nick,omitempty"`
	Bio   string    `json:"bio,omitempty"`   // join only
	Body  string    `json:"body,omitempty"`  // message only
	Topic string    `json:"topic,omitempty"` // open only
	Time  time.Time `json:"time"`
}

// Manager owns every room for one project: in-memory state for concurrency
// (seq, join uniqueness, long-poll wake-up) backed by an append-only jsonl
// file per room under dir. Safe for concurrent use — construct one per
// project cache dir and reuse it for the server process lifetime.
type Manager struct {
	dir string

	mu     sync.Mutex // guards rooms + nextID; never held during file I/O
	rooms  map[string]*roomState
	nextID int
}

// roomState is one room's live state. rs.mu is the single serialization
// point for that room: seq assignment, log append, file persist and
// long-poll broadcast all happen while it is held, so concurrent Say calls
// never race for a seq slot.
type roomState struct {
	mu      sync.Mutex
	meta    Room
	log     []Message
	changed chan struct{} // closed and replaced on every append; long-poll wakeup
}

// NewManager creates dir if needed and replays every existing <id>.jsonl into
// memory, so a server restart keeps open rooms and their transcripts.
func NewManager(dir string) (*Manager, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	m := &Manager{dir: dir, rooms: map[string]*roomState{}}
	if err := m.load(); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *Manager) load() error {
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".jsonl")
		rs, err := replayRoom(filepath.Join(m.dir, e.Name()), id)
		if err != nil {
			return fmt.Errorf("replay %s: %w", id, err)
		}
		m.rooms[id] = rs
		if n, ok := roomNumber(id); ok && n > m.nextID {
			m.nextID = n
		}
	}
	return nil
}

func roomNumber(id string) (int, bool) {
	n, err := strconv.Atoi(strings.TrimPrefix(id, "R"))
	if err != nil {
		return 0, false
	}
	return n, true
}

// replayRoom rebuilds a room's meta + log from its persisted jsonl file.
func replayRoom(path, id string) (*roomState, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	rs := &roomState{meta: Room{ID: id}, changed: make(chan struct{})}
	seen := map[string]bool{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec Message
		if err := json.Unmarshal(line, &rec); err != nil {
			return nil, err
		}
		switch rec.Type {
		case typeOpen:
			rs.meta.Topic = rec.Topic
			rs.meta.OpenedAt = rec.Time
		case typeJoin:
			if !seen[rec.Nick] {
				seen[rec.Nick] = true
				rs.meta.Participants = append(rs.meta.Participants, Participant{Nick: rec.Nick, Bio: rec.Bio, JoinedAt: rec.Time})
			}
		case typeClose:
			t := rec.Time
			rs.meta.ClosedAt = &t
		}
		rs.log = append(rs.log, rec)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return rs, nil
}

func (m *Manager) getRoom(id string) (*roomState, error) {
	m.mu.Lock()
	rs, ok := m.rooms[id]
	m.mu.Unlock()
	if !ok {
		return nil, ErrNotFound
	}
	return rs, nil
}

// appendLocked assigns the next sequential seq, persists the event and wakes
// any long-poll reader. Caller must already hold rs.mu.
func (m *Manager) appendLocked(rs *roomState, rec Message) error {
	rec.Seq = len(rs.log) + 1
	if err := m.persist(rs.meta.ID, rec); err != nil {
		return err
	}
	rs.log = append(rs.log, rec)
	close(rs.changed)
	rs.changed = make(chan struct{})
	return nil
}

func (m *Manager) persist(id string, rec Message) error {
	line, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(m.dir, id+".jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(line, '\n'))
	return err
}

func (rs *roomState) snapshotLocked() Room {
	cp := rs.meta
	cp.Participants = append([]Participant(nil), rs.meta.Participants...)
	return cp
}

// Open creates a room with a sequential id ("R1", "R2", ...), persists the
// opening event and returns the fresh room (empty roster).
func (m *Manager) Open(topic string) (Room, error) {
	m.mu.Lock()
	m.nextID++
	id := fmt.Sprintf("R%d", m.nextID)
	now := time.Now().UTC()
	rs := &roomState{meta: Room{ID: id, Topic: topic, OpenedAt: now}, changed: make(chan struct{})}
	m.rooms[id] = rs
	m.mu.Unlock()

	rs.mu.Lock()
	defer rs.mu.Unlock()
	if err := m.appendLocked(rs, Message{Type: typeOpen, Topic: topic, Time: now}); err != nil {
		return Room{}, err
	}
	return rs.snapshotLocked(), nil
}

// Join registers nick as a participant. Rejected when the room is unknown,
// closed, or the nick is already taken in this room.
func (m *Manager) Join(id, nick, bio string) (Room, error) {
	rs, err := m.getRoom(id)
	if err != nil {
		return Room{}, err
	}
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if rs.meta.ClosedAt != nil {
		return Room{}, ErrClosed
	}
	for _, p := range rs.meta.Participants {
		if p.Nick == nick {
			return Room{}, ErrNickTaken
		}
	}
	now := time.Now().UTC()
	if err := m.appendLocked(rs, Message{Type: typeJoin, Nick: nick, Bio: bio, Time: now}); err != nil {
		return Room{}, err
	}
	rs.meta.Participants = append(rs.meta.Participants, Participant{Nick: nick, Bio: bio, JoinedAt: now})
	return rs.snapshotLocked(), nil
}

// Say appends a message from nick. Rejected when the room is unknown,
// closed, or nick has not joined.
func (m *Manager) Say(id, nick, body string) (Message, error) {
	rs, err := m.getRoom(id)
	if err != nil {
		return Message{}, err
	}
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if rs.meta.ClosedAt != nil {
		return Message{}, ErrClosed
	}
	joined := false
	for _, p := range rs.meta.Participants {
		if p.Nick == nick {
			joined = true
			break
		}
	}
	if !joined {
		return Message{}, ErrNotJoined
	}
	rec := Message{Type: typeMessage, Nick: nick, Body: body, Time: time.Now().UTC()}
	if err := m.appendLocked(rs, rec); err != nil {
		return Message{}, err
	}
	return rs.log[len(rs.log)-1], nil
}

// Close closes the room: further Join/Say are rejected, but Read keeps
// serving the transcript. Idempotent.
func (m *Manager) Close(id string) (Room, error) {
	rs, err := m.getRoom(id)
	if err != nil {
		return Room{}, err
	}
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if rs.meta.ClosedAt != nil {
		return rs.snapshotLocked(), nil
	}
	now := time.Now().UTC()
	if err := m.appendLocked(rs, Message{Type: typeClose, Time: now}); err != nil {
		return Room{}, err
	}
	rs.meta.ClosedAt = &now
	return rs.snapshotLocked(), nil
}

// Read returns every event with Seq > since. When follow is true and none is
// yet available, it blocks (long-poll) until a new event arrives, ctx is
// canceled, or timeout elapses — an empty result on timeout is not an error.
func (m *Manager) Read(ctx context.Context, id string, since int, follow bool, timeout time.Duration) ([]Message, error) {
	rs, err := m.getRoom(id)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(timeout)
	for {
		rs.mu.Lock()
		var out []Message
		for _, msg := range rs.log {
			if msg.Seq > since {
				out = append(out, msg)
			}
		}
		ch := rs.changed
		rs.mu.Unlock()

		if len(out) > 0 || !follow {
			return out, nil
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, nil
		}
		timer := time.NewTimer(remaining)
		select {
		case <-ch:
			timer.Stop()
		case <-timer.C:
			return nil, nil
		case <-ctx.Done():
			timer.Stop()
			return nil, nil
		}
	}
}

// List returns every room (open and closed), sorted by numeric id.
func (m *Manager) List() []Room {
	m.mu.Lock()
	states := make([]*roomState, 0, len(m.rooms))
	for _, rs := range m.rooms {
		states = append(states, rs)
	}
	m.mu.Unlock()

	out := make([]Room, len(states))
	for i, rs := range states {
		rs.mu.Lock()
		out[i] = rs.snapshotLocked()
		rs.mu.Unlock()
	}
	sort.Slice(out, func(i, j int) bool {
		ni, _ := roomNumber(out[i].ID)
		nj, _ := roomNumber(out[j].ID)
		return ni < nj
	})
	return out
}
