// Package room implements the war room primitive (F-0211/T-0001): local,
// in-process chat rooms where CLI-driven agents and the UI converse. State
// lives in memory for the life of the server process; every event is also
// appended to <dir>/<id>.jsonl so a restart can replay the transcript.
package room

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
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
	ErrNotFound       = errors.New("room not found")
	ErrClosed         = errors.New("room is closed")
	ErrNickTaken      = errors.New("nickname already in use in this room")
	ErrInvalidSession = errors.New("invalid room session")
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

// Session is the private capability minted on Join (F-0226). SessionID is a
// bearer token: whoever presents it speaks as Nick. It is returned only to
// the joiner and never appears in the transcript or public read/list output.
type Session struct {
	SessionID string    `json:"session_id"`
	RoomID    string    `json:"room_id"`
	Nick      string    `json:"nick"`
	Bio       string    `json:"bio,omitempty"`
	JoinedAt  time.Time `json:"joined_at"`
	LastSeq   int       `json:"last_seq"`
}

// sessionAuth is the server-side half of a Session: only the SHA-256 hash of
// the capability is kept, never the raw token.
type sessionAuth struct {
	hash     [sha256.Size]byte
	nick     string
	joinedAt time.Time
}

// newSessionToken mints a 128-bit random capability, hex-encoded.
func newSessionToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
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
	mu       sync.Mutex
	meta     Room
	log      []Message
	sessions []sessionAuth
	changed  chan struct{} // closed and replaced on every append; long-poll wakeup
}

// findSessionLocked resolves a presented capability to its sessionAuth by
// hashing it and comparing against every stored hash in constant time.
// Caller must hold rs.mu.
func (rs *roomState) findSessionLocked(sessionID string) (sessionAuth, bool) {
	h := sha256.Sum256([]byte(sessionID))
	for _, s := range rs.sessions {
		if subtle.ConstantTimeCompare(h[:], s.hash[:]) == 1 {
			return s, true
		}
	}
	return sessionAuth{}, false
}

// bioOfLocked returns the roster bio for nick. Caller must hold rs.mu.
func (rs *roomState) bioOfLocked(nick string) string {
	for _, p := range rs.meta.Participants {
		if p.Nick == nick {
			return p.Bio
		}
	}
	return ""
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
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") || strings.HasSuffix(e.Name(), sessionsSuffix) {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".jsonl")
		rs, err := replayRoom(filepath.Join(m.dir, e.Name()), id)
		if err != nil {
			return fmt.Errorf("replay %s: %w", id, err)
		}
		if err := loadSessions(filepath.Join(m.dir, id+sessionsSuffix), rs); err != nil {
			return fmt.Errorf("replay %s sessions: %w", id, err)
		}
		m.rooms[id] = rs
		if n, ok := roomNumber(id); ok && n > m.nextID {
			m.nextID = n
		}
	}
	return nil
}

const sessionsSuffix = ".sessions.jsonl"

// sessionRecord is one line of the private <id>.sessions.jsonl sidecar: the
// hex SHA-256 of the capability, never the raw token.
type sessionRecord struct {
	Hash     string    `json:"hash"`
	Nick     string    `json:"nick"`
	JoinedAt time.Time `json:"joined_at"`
}

// loadSessions replays the private sidecar into rs. Only records whose nick
// has a matching public join are activated — a crash between the private
// write and the public event leaves an orphan row, which is ignored; the
// last matching record per nick wins (a retry after such a crash). A missing
// sidecar (legacy transcript) is fine: participants simply have no session.
func loadSessions(path string, rs *roomState) error {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()

	joined := map[string]bool{}
	for _, p := range rs.meta.Participants {
		joined[p.Nick] = true
	}
	latest := map[string]sessionRecord{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec sessionRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			return err
		}
		if joined[rec.Nick] {
			latest[rec.Nick] = rec
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	for _, rec := range latest {
		raw, err := hex.DecodeString(rec.Hash)
		if err != nil || len(raw) != sha256.Size {
			continue // corrupt row: skip rather than brick the room
		}
		sa := sessionAuth{nick: rec.Nick, joinedAt: rec.JoinedAt}
		copy(sa.hash[:], raw)
		rs.sessions = append(rs.sessions, sa)
	}
	return nil
}

// persistSession appends the private record for a freshly minted capability.
// Written before the public join event, so a crash in between leaves only an
// orphan sidecar row (ignored on replay), never a join without authority.
func (m *Manager) persistSession(id string, rec sessionRecord) error {
	line, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(m.dir, id+sessionsSuffix), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(line, '\n'))
	return err
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

// Join registers nick as a participant and mints its session capability.
// Rejected when the room is unknown, closed, or the nick already has an
// active session. A legacy participant (replayed transcript without session
// data) may re-join to obtain a capability.
func (m *Manager) Join(id, nick, bio string) (Room, Session, error) {
	rs, err := m.getRoom(id)
	if err != nil {
		return Room{}, Session{}, err
	}
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if rs.meta.ClosedAt != nil {
		return Room{}, Session{}, ErrClosed
	}
	for _, s := range rs.sessions {
		if s.nick == nick {
			return Room{}, Session{}, ErrNickTaken
		}
	}
	token, err := newSessionToken()
	if err != nil {
		return Room{}, Session{}, err
	}
	now := time.Now().UTC()
	hash := sha256.Sum256([]byte(token))
	if err := m.persistSession(rs.meta.ID, sessionRecord{Hash: hex.EncodeToString(hash[:]), Nick: nick, JoinedAt: now}); err != nil {
		return Room{}, Session{}, err
	}
	if err := m.appendLocked(rs, Message{Type: typeJoin, Nick: nick, Bio: bio, Time: now}); err != nil {
		return Room{}, Session{}, err
	}
	if !rs.hasParticipantLocked(nick) {
		rs.meta.Participants = append(rs.meta.Participants, Participant{Nick: nick, Bio: bio, JoinedAt: now})
	}
	rs.sessions = append(rs.sessions, sessionAuth{hash: hash, nick: nick, joinedAt: now})
	sess := Session{SessionID: token, RoomID: rs.meta.ID, Nick: nick, Bio: bio, JoinedAt: now, LastSeq: len(rs.log)}
	return rs.snapshotLocked(), sess, nil
}

func (rs *roomState) hasParticipantLocked(nick string) bool {
	for _, p := range rs.meta.Participants {
		if p.Nick == nick {
			return true
		}
	}
	return false
}

// Resume validates an existing capability and returns its current session
// without appending any event. Idempotent.
func (m *Manager) Resume(id, sessionID string) (Session, error) {
	rs, err := m.getRoom(id)
	if err != nil {
		return Session{}, err
	}
	rs.mu.Lock()
	defer rs.mu.Unlock()
	sa, ok := rs.findSessionLocked(sessionID)
	if !ok {
		return Session{}, ErrInvalidSession
	}
	return Session{
		SessionID: sessionID,
		RoomID:    rs.meta.ID,
		Nick:      sa.nick,
		Bio:       rs.bioOfLocked(sa.nick),
		JoinedAt:  sa.joinedAt,
		LastSeq:   len(rs.log),
	}, nil
}

// Say appends a message authored by the session's nick — the capability, not
// any client-supplied name, is the authority (F-0226). Rejected when the
// room is unknown or closed, or the capability doesn't belong to this room.
func (m *Manager) Say(id, sessionID, body string) (Message, error) {
	rs, err := m.getRoom(id)
	if err != nil {
		return Message{}, err
	}
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if rs.meta.ClosedAt != nil {
		return Message{}, ErrClosed
	}
	sa, ok := rs.findSessionLocked(sessionID)
	if !ok {
		return Message{}, ErrInvalidSession
	}
	rec := Message{Type: typeMessage, Nick: sa.nick, Body: body, Time: time.Now().UTC()}
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
