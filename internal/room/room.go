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
	ErrNotFound             = errors.New("room not found")
	ErrClosed               = errors.New("room is closed")
	ErrNickTaken            = errors.New("nickname already in use in this room")
	ErrInvalidSession       = errors.New("invalid room session")
	ErrNotFacilitator       = errors.New("caller is not the room facilitator")
	ErrIncompleteMinutes    = errors.New("minutes missing a required field")
	ErrOpenItems            = errors.New("room has open facilitation items")
	ErrItemNotFound         = errors.New("facilitation item not found or already resolved")
	ErrRoomOpen             = errors.New("room is still open — close it with minutes first")
	ErrMinutesAlreadyLinked = errors.New("room minutes are already registered as a note")
)

const (
	typeOpen            = "open"
	typeJoin            = "join"
	typeMessage         = "message"
	typeClose           = "close"
	typeProposal        = "proposal"
	typeDecisionRequest = "decision_request"
	typeResolution      = "resolution"
)

// Room is the metadata + roster snapshot returned to callers.
type Room struct {
	ID           string        `json:"id"`
	Topic        string        `json:"topic"`
	OpenedAt     time.Time     `json:"opened_at"`
	ClosedAt     *time.Time    `json:"closed_at,omitempty"`
	Participants []Participant `json:"participants"`
	Minutes      *Minutes      `json:"minutes,omitempty"`
	// MessageCount is the transcript's current length (every event, not just
	// said messages) — the "período seq" of a registered minutes note (F-0238).
	MessageCount int `json:"message_count"`
	// MinutesNote is the id of the note (N-NNNN) this room's closing minutes
	// were registered as, set by SetMinutesNote (F-0238). Empty until then.
	MinutesNote string `json:"minutes_note,omitempty"`
	// Permanent marks the project's permanent channel (F-0325): no
	// open/close/minutes, roster derived from the active works.
	Permanent bool `json:"permanent,omitempty"`
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
// event, a said message, or a facilitation event (F-0228).
type Message struct {
	Seq   int       `json:"seq"`
	Type  string    `json:"type"` // open|join|message|close|proposal|decision_request|resolution
	Nick  string    `json:"nick,omitempty"`
	Bio   string    `json:"bio,omitempty"`   // join only
	Body  string    `json:"body,omitempty"`  // message body, item description, or resolution result
	Topic string    `json:"topic,omitempty"` // open only
	Time  time.Time `json:"time"`

	Title   string   `json:"title,omitempty"`   // proposal|decision_request only
	ItemID  string   `json:"item_id,omitempty"` // resolution only: the item it resolves
	Minutes *Minutes `json:"minutes,omitempty"` // close only

	Human bool `json:"human,omitempty"` // permanent channel (F-0325): posted by the human from the UI
	// Session is the agent session that posted a channel message (F-0325):
	// an answer counts only from the session that was shown the message.
	Session string `json:"session,omitempty"`
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
	mu              sync.Mutex
	meta            Room
	log             []Message
	sessions        []sessionAuth
	facilitatorHash [sha256.Size]byte // F-0228: single rotating capability, never in log/sessions
	items           []FacilitationItem
	changed         chan struct{} // closed and replaced on every append; long-poll wakeup
}

// checkFacilitatorLocked reports whether token is the room's current
// facilitator capability. Caller must hold rs.mu.
func (rs *roomState) checkFacilitatorLocked(token string) bool {
	h := sha256.Sum256([]byte(token))
	return subtle.ConstantTimeCompare(h[:], rs.facilitatorHash[:]) == 1
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
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") ||
			strings.HasSuffix(e.Name(), sessionsSuffix) || strings.HasSuffix(e.Name(), facilitatorSuffix) ||
			strings.HasSuffix(e.Name(), minutesNoteSuffix) {
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
		if err := loadFacilitator(filepath.Join(m.dir, id+facilitatorSuffix), rs); err != nil {
			return fmt.Errorf("replay %s facilitator: %w", id, err)
		}
		if err := loadMinutesNote(filepath.Join(m.dir, id+minutesNoteSuffix), rs); err != nil {
			return fmt.Errorf("replay %s minutes note: %w", id, err)
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

const facilitatorSuffix = ".facilitator.jsonl"

// facilitatorRecord is one line of the private <id>.facilitator.jsonl
// sidecar: the hex SHA-256 of the facilitator capability, never the raw
// token. Append-only; the last line is the active capability (rotation).
type facilitatorRecord struct {
	Hash     string    `json:"hash"`
	IssuedAt time.Time `json:"issued_at"`
}

// persistFacilitator appends a newly minted (or rotated) facilitator
// capability record.
func (m *Manager) persistFacilitator(id string, rec facilitatorRecord) error {
	line, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(m.dir, id+facilitatorSuffix), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(line, '\n'))
	return err
}

// loadFacilitator replays the private sidecar into rs, keeping only the last
// (most recent) hash — every earlier rotation is superseded. A missing
// sidecar (no room should have one after F-0228, but tolerate it) leaves
// rs.facilitatorHash zeroed, so no capability will ever match it.
func loadFacilitator(path string, rs *roomState) error {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()

	var last facilitatorRecord
	found := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec facilitatorRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			return err
		}
		last = rec
		found = true
	}
	if err := sc.Err(); err != nil {
		return err
	}
	if !found {
		return nil
	}
	raw, err := hex.DecodeString(last.Hash)
	if err != nil || len(raw) != sha256.Size {
		return nil // corrupt row: leave unset rather than brick the room
	}
	copy(rs.facilitatorHash[:], raw)
	return nil
}

const minutesNoteSuffix = ".minutes-note.jsonl"

// minutesNoteRecord is one line of the private <id>.minutes-note.jsonl
// sidecar: append-only, same shape as the facilitator sidecar. The last line
// is the active link (a forced re-registration appends rather than
// overwrites, so the history of who registered what stays on disk).
type minutesNoteRecord struct {
	NoteID       string    `json:"note_id"`
	RegisteredAt time.Time `json:"registered_at"`
}

// persistMinutesNote appends a newly registered (or force-overwritten)
// minutes-note link.
func (m *Manager) persistMinutesNote(id string, rec minutesNoteRecord) error {
	line, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(m.dir, id+minutesNoteSuffix), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(line, '\n'))
	return err
}

// loadMinutesNote replays the sidecar into rs, keeping only the last
// (most recent) record — a missing sidecar (no registration yet) leaves
// rs.meta.MinutesNote empty.
func loadMinutesNote(path string, rs *roomState) error {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()

	var last minutesNoteRecord
	found := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec minutesNoteRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			return err
		}
		last = rec
		found = true
	}
	if err := sc.Err(); err != nil {
		return err
	}
	if found {
		rs.meta.MinutesNote = last.NoteID
	}
	return nil
}

// SetMinutesNote records id's closing minutes as registered under noteID
// (F-0238 — `cerne room minutes register`): the room must already be closed
// (ErrRoomOpen otherwise), and an existing link is preserved unless force is
// true (ErrMinutesAlreadyLinked otherwise, with the current link still
// readable on the returned Room, so the caller can report which note it is).
func (m *Manager) SetMinutesNote(id, noteID string, force bool) (Room, error) {
	rs, err := m.getRoom(id)
	if err != nil {
		return Room{}, err
	}
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if rs.meta.ClosedAt == nil {
		return Room{}, ErrRoomOpen
	}
	if rs.meta.MinutesNote != "" && !force {
		return rs.snapshotLocked(), ErrMinutesAlreadyLinked
	}
	if err := m.persistMinutesNote(id, minutesNoteRecord{NoteID: noteID, RegisteredAt: time.Now().UTC()}); err != nil {
		return Room{}, err
	}
	rs.meta.MinutesNote = noteID
	return rs.snapshotLocked(), nil
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
			rs.meta.Minutes = rec.Minutes
		case typeProposal, typeDecisionRequest:
			rs.items = append(rs.items, FacilitationItem{
				ID:        fmt.Sprintf("I%d", rec.Seq),
				Kind:      rec.Type,
				Author:    rec.Nick,
				Title:     rec.Title,
				Body:      rec.Body,
				SourceSeq: rec.Seq,
				Status:    itemStatusOpen,
			})
		case typeResolution:
			for i := range rs.items {
				if rs.items[i].ID == rec.ItemID {
					rs.items[i].Status = itemStatusResolved
					rs.items[i].Result = rec.Body
					rs.items[i].ResolutionSeq = rec.Seq
					break
				}
			}
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
	cp.MessageCount = len(rs.log)
	return cp
}

// Open creates a room with a sequential id ("R1", "R2", ...), persists the
// opening event and mints the room's facilitator capability (F-0228): the
// bearer can resolve facilitation items and close the room, and can transfer
// the role via HandoffFacilitator. It is returned only here (and by
// HandoffFacilitator) and never appears in the roster, transcript or List.
func (m *Manager) Open(topic string) (Room, string, error) {
	m.mu.Lock()
	m.nextID++
	id := fmt.Sprintf("R%d", m.nextID)
	now := time.Now().UTC()
	rs := &roomState{meta: Room{ID: id, Topic: topic, OpenedAt: now}, changed: make(chan struct{})}
	m.rooms[id] = rs
	m.mu.Unlock()

	facilitator, err := newSessionToken()
	if err != nil {
		return Room{}, "", err
	}
	hash := sha256.Sum256([]byte(facilitator))
	if err := m.persistFacilitator(id, facilitatorRecord{Hash: hex.EncodeToString(hash[:]), IssuedAt: now}); err != nil {
		return Room{}, "", err
	}

	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.facilitatorHash = hash
	if err := m.appendLocked(rs, Message{Type: typeOpen, Topic: topic, Time: now}); err != nil {
		return Room{}, "", err
	}
	return rs.snapshotLocked(), facilitator, nil
}

// HandoffFacilitator validates current, then atomically mints and stores a
// replacement facilitator capability, invalidating current in the same
// step — there is never a window with two valid capabilities. Does not
// append a transcript event (the role is possession of the capability, not
// an identity change worth recording).
func (m *Manager) HandoffFacilitator(id, current string) (string, error) {
	rs, err := m.getRoom(id)
	if err != nil {
		return "", err
	}
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if !rs.checkFacilitatorLocked(current) {
		return "", ErrNotFacilitator
	}
	next, err := newSessionToken()
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256([]byte(next))
	if err := m.persistFacilitator(id, facilitatorRecord{Hash: hex.EncodeToString(hash[:]), IssuedAt: time.Now().UTC()}); err != nil {
		return "", err
	}
	rs.facilitatorHash = hash
	return next, nil
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

// Propose appends a proposal raised by sessionID's participant (F-0228
// criterion 2). See addItem.
func (m *Manager) Propose(id, sessionID, title, body string) (FacilitationItem, error) {
	return m.addItem(id, sessionID, typeProposal, title, body)
}

// RequestDecision appends a decision request raised by sessionID's
// participant (F-0228 criterion 2). See addItem.
func (m *Manager) RequestDecision(id, sessionID, title, body string) (FacilitationItem, error) {
	return m.addItem(id, sessionID, typeDecisionRequest, title, body)
}

// addItem is the shared implementation of Propose/RequestDecision: any
// authenticated participant (not just the facilitator) may raise an item,
// preserving the transcript's global seq order.
func (m *Manager) addItem(id, sessionID, kind, title, body string) (FacilitationItem, error) {
	rs, err := m.getRoom(id)
	if err != nil {
		return FacilitationItem{}, err
	}
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if rs.meta.ClosedAt != nil {
		return FacilitationItem{}, ErrClosed
	}
	sa, ok := rs.findSessionLocked(sessionID)
	if !ok {
		return FacilitationItem{}, ErrInvalidSession
	}
	rec := Message{Type: kind, Nick: sa.nick, Title: title, Body: body, Time: time.Now().UTC()}
	if err := m.appendLocked(rs, rec); err != nil {
		return FacilitationItem{}, err
	}
	seq := rs.log[len(rs.log)-1].Seq
	item := FacilitationItem{
		ID:        fmt.Sprintf("I%d", seq),
		Kind:      kind,
		Author:    sa.nick,
		Title:     title,
		Body:      body,
		SourceSeq: seq,
		Status:    itemStatusOpen,
	}
	rs.items = append(rs.items, item)
	return item, nil
}

// CheckFacilitator reports ErrNotFacilitator when token is not id's current
// facilitator capability, without side effects — for callers (e.g. the read
// side of the HTTP API) that need to gate on the role without resolving or
// closing anything.
func (m *Manager) CheckFacilitator(id, token string) error {
	rs, err := m.getRoom(id)
	if err != nil {
		return err
	}
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if !rs.checkFacilitatorLocked(token) {
		return ErrNotFacilitator
	}
	return nil
}

// Pending returns every open facilitation item, in the order they were
// raised — no LLM synthesis, just the typed events as-is (F-0228 criterion
// 3).
func (m *Manager) Pending(id string) ([]FacilitationItem, error) {
	rs, err := m.getRoom(id)
	if err != nil {
		return nil, err
	}
	rs.mu.Lock()
	defer rs.mu.Unlock()
	var out []FacilitationItem
	for _, it := range rs.items {
		if it.Status == itemStatusOpen {
			out = append(out, it)
		}
	}
	return out, nil
}

// Resolve marks an open facilitation item resolved with result, appending a
// resolution event. Only the facilitator may resolve (F-0228 criterion 4).
func (m *Manager) Resolve(id, facilitatorToken, itemID, result string) (FacilitationItem, error) {
	rs, err := m.getRoom(id)
	if err != nil {
		return FacilitationItem{}, err
	}
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if !rs.checkFacilitatorLocked(facilitatorToken) {
		return FacilitationItem{}, ErrNotFacilitator
	}
	idx := -1
	for i, it := range rs.items {
		if it.ID == itemID && it.Status == itemStatusOpen {
			idx = i
			break
		}
	}
	if idx == -1 {
		return FacilitationItem{}, ErrItemNotFound
	}
	rec := Message{Type: typeResolution, ItemID: itemID, Body: result, Time: time.Now().UTC()}
	if err := m.appendLocked(rs, rec); err != nil {
		return FacilitationItem{}, err
	}
	seq := rs.log[len(rs.log)-1].Seq
	rs.items[idx].Status = itemStatusResolved
	rs.items[idx].Result = result
	rs.items[idx].ResolutionSeq = seq
	return rs.items[idx], nil
}

// Close closes the room: further Join/Say are rejected, but Read keeps
// serving the transcript. Only the facilitator may close (criterion 4), the
// minutes must carry its required fields (criterion 6), and by default close
// refuses while facilitation items are still open — allowOpenItems forces it
// through and records the exception in minutes.OpenItems (criterion 5).
// Idempotent: closing an already-closed room is a no-op returning its
// existing minutes, still gated on a valid facilitator capability.
func (m *Manager) Close(id, facilitatorToken string, minutes Minutes, allowOpenItems bool) (Room, error) {
	rs, err := m.getRoom(id)
	if err != nil {
		return Room{}, err
	}
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if !rs.checkFacilitatorLocked(facilitatorToken) {
		return Room{}, ErrNotFacilitator
	}
	if rs.meta.ClosedAt != nil {
		return rs.snapshotLocked(), nil
	}
	if err := minutes.validate(); err != nil {
		return Room{}, err
	}
	var openIDs []string
	for _, it := range rs.items {
		if it.Status == itemStatusOpen {
			openIDs = append(openIDs, it.ID)
		}
	}
	if len(openIDs) > 0 {
		if !allowOpenItems {
			return Room{}, ErrOpenItems
		}
		minutes.OpenItems = openIDs
	}
	now := time.Now().UTC()
	if err := m.appendLocked(rs, Message{Type: typeClose, Time: now, Minutes: &minutes}); err != nil {
		return Room{}, err
	}
	rs.meta.ClosedAt = &now
	rs.meta.Minutes = &minutes
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
