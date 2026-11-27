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
	ErrNickReserved         = errors.New(`the nickname "facilitator" is reserved: it names the facilitator as a minutes draft author`)
	ErrInvalidSession       = errors.New("invalid room session")
	ErrNotFacilitator       = errors.New("caller is not the room facilitator")
	ErrIncompleteMinutes    = errors.New("minutes missing a required field")
	ErrOpenItems            = errors.New("room has open facilitation items")
	ErrItemNotFound         = errors.New("facilitation item not found or already resolved")
	ErrRoomOpen             = errors.New("room is still open — close it with minutes first")
	ErrMinutesAlreadyLinked = errors.New("room minutes are already registered as a note")
	ErrNoMinutesDraft       = errors.New("room has no minutes draft")
	ErrDraftChanged         = errors.New("the minutes draft changed after it was reviewed")
)

const (
	typeOpen            = "open"
	typeJoin            = "join"
	typeLeave           = "leave"
	typeMessage         = "message"
	typeClose           = "close"
	typeProposal        = "proposal"
	typeDecisionRequest = "decision_request"
	typeResolution      = "resolution"
	typeMinutesDraft    = "minutes_draft"
)

// Room is the metadata + roster snapshot returned to callers.
type Room struct {
	ID           string        `json:"id"`
	Topic        string        `json:"topic"`
	OpenedAt     time.Time     `json:"opened_at"`
	ClosedAt     *time.Time    `json:"closed_at,omitempty"`
	Participants []Participant `json:"participants"`
	// ActiveParticipants is the current membership (F-0511): one entry per
	// valid session, in join order. Participants stays the historical roster
	// the minutes read; a nick that left drops out of this list only.
	ActiveParticipants []Participant `json:"active_participants"`
	Minutes            *Minutes      `json:"minutes,omitempty"`
	// MessageCount is the transcript's current length (every event, not just
	// said messages) — the "período seq" of a registered minutes note (F-0238).
	MessageCount int `json:"message_count"`
	// MinutesNote is the id of the note (N-NNNN) this room's closing minutes
	// were registered as, set by SetMinutesNote (F-0238). Empty until then.
	MinutesNote string `json:"minutes_note,omitempty"`
	// Permanent marks the project's permanent channel (F-0325): no
	// open/close/minutes, roster derived from the active works.
	Permanent bool `json:"permanent,omitempty"`
	// MinutesDraft is the open room's current shared draft (T-0022), saved by
	// any participant or the facilitator; gone once the room closes.
	MinutesDraft *MinutesDraft `json:"minutes_draft,omitempty"`
}

// MinutesDraft is a validated minutes proposal and who saved it last (T-0022).
// Author is the participant's nick, or "facilitator".
type MinutesDraft struct {
	Minutes   Minutes   `json:"minutes"`
	Author    string    `json:"author"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Participant is one joined nick. Listening and LastSeenAt are live presence
// (T-0021), in memory only and set only on ActiveParticipants entries.
type Participant struct {
	Nick     string    `json:"nick"`
	Bio      string    `json:"bio,omitempty"`
	JoinedAt time.Time `json:"joined_at"`
	// Listening: the nick's session holds a follow read in flight in an
	// open room.
	Listening bool `json:"listening,omitempty"`
	// LastSeenAt: the session's last authenticated interaction.
	LastSeenAt *time.Time `json:"last_seen_at,omitempty"`
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
	Type  string    `json:"type"` // open|join|leave|message|close|proposal|decision_request|resolution|minutes_draft
	Nick  string    `json:"nick,omitempty"`
	Bio   string    `json:"bio,omitempty"`   // join only
	Body  string    `json:"body,omitempty"`  // message body, item description, or resolution result
	Topic string    `json:"topic,omitempty"` // open only
	Time  time.Time `json:"time"`

	Title   string   `json:"title,omitempty"`   // proposal|decision_request only
	ItemID  string   `json:"item_id,omitempty"` // resolution only: the item it resolves
	Minutes *Minutes `json:"minutes,omitempty"` // close and minutes_draft only

	Human bool `json:"human,omitempty"` // permanent channel (F-0325): posted by the human from the UI
	// Session is the agent session that posted a channel message (F-0325):
	// an answer counts only from the session that was shown the message.
	Session string `json:"session,omitempty"`
	// Ref is the card a channel message is about (F-0486): the human's message
	// from the card's page, and the replies to it — the card's thread.
	Ref string `json:"ref,omitempty"`
	// To is the sessions a human's message from a card's page is for, and only
	// them (F-0486). Like Session, never served.
	To []string `json:"to,omitempty"`
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
	// Live presence by nick (T-0021), never persisted: follow reads in flight
	// and the last authenticated interaction.
	watchers map[string]int
	lastSeen map[string]time.Time
}

// touchLocked stamps nick's last authenticated interaction. Caller must hold
// rs.mu.
func (rs *roomState) touchLocked(nick string) {
	if rs.lastSeen == nil {
		rs.lastSeen = map[string]time.Time{}
	}
	rs.lastSeen[nick] = time.Now().UTC()
}

// hasSessionLocked reports whether the session with hash is still valid.
// Caller must hold rs.mu.
func (rs *roomState) hasSessionLocked(hash [sha256.Size]byte) bool {
	for _, s := range rs.sessions {
		if s.hash == hash {
			return true
		}
	}
	return false
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
		if err := loadMinutesDraft(filepath.Join(m.dir, id+minutesDraftSuffix), rs); err != nil {
			return fmt.Errorf("replay %s minutes draft: %w", id, err)
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
// hex SHA-256 of the capability, never the raw token. Revoked marks an
// explicit leave (F-0511): the nick's capability stops being valid.
type sessionRecord struct {
	Hash     string    `json:"hash"`
	Nick     string    `json:"nick"`
	JoinedAt time.Time `json:"joined_at"`
	Revoked  bool      `json:"revoked,omitempty"`
}

// loadSessions replays the private sidecar into rs. A record is activated only
// when it belongs to its nick's last public join — the nick has not left since,
// and Join wrote the same instant to the record and to the event — so a crash
// between the private write and the public event leaves an orphan row, which
// is ignored; the last matching record per nick wins (a revocation of that
// join), and a revoked last record activates nothing (F-0511). A missing
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

	lastJoin := map[string]time.Time{} // nick → its last public join, while it has not left since
	for _, msg := range rs.log {
		switch msg.Type {
		case typeJoin:
			lastJoin[msg.Nick] = msg.Time
		case typeLeave:
			delete(lastJoin, msg.Nick)
		}
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
		if t, ok := lastJoin[rec.Nick]; ok && rec.JoinedAt.Equal(t) {
			latest[rec.Nick] = rec
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	for _, rec := range latest {
		if rec.Revoked {
			continue
		}
		raw, err := hex.DecodeString(rec.Hash)
		if err != nil || len(raw) != sha256.Size {
			continue // corrupt row: skip rather than brick the room
		}
		sa := sessionAuth{nick: rec.Nick, joinedAt: rec.JoinedAt}
		copy(sa.hash[:], raw)
		rs.sessions = append(rs.sessions, sa)
	}
	sort.Slice(rs.sessions, func(i, j int) bool { return rs.sessions[i].joinedAt.Before(rs.sessions[j].joinedAt) })
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
// capability record to room id's sidecar under dir.
func persistFacilitator(dir, id string, rec facilitatorRecord) error {
	line, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, id+facilitatorSuffix), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
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

// minutesDraftSuffix names the file holding a room's current minutes draft
// (T-0022): one JSON object, not a .jsonl log, so a cerne that predates it
// never lists the file as a room.
const minutesDraftSuffix = ".minutes-draft.json"

// persistMinutesDraft replaces the room's draft file atomically (temp file
// plus rename), so a crash never leaves it torn. No capability is ever
// written: the author is a nick or "facilitator".
func (m *Manager) persistMinutesDraft(id string, d MinutesDraft) error {
	data, err := json.Marshal(d)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(m.dir, id+minutesDraftSuffix+".*.tmp")
	if err != nil {
		return err
	}
	_, err = tmp.Write(data)
	if err == nil {
		err = tmp.Chmod(0o644)
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), filepath.Join(m.dir, id+minutesDraftSuffix))
	}
	if err != nil {
		os.Remove(tmp.Name())
	}
	return err
}

// loadMinutesDraft reads an open room's draft file; a closed room keeps
// none, and a missing file or corrupt JSON means no draft. Any other read
// error (a permission error) fails the load, as for the sibling sidecars.
func loadMinutesDraft(path string, rs *roomState) error {
	if rs.meta.ClosedAt != nil {
		return nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var d MinutesDraft
	if json.Unmarshal(data, &d) != nil {
		return nil // corrupt file: no draft rather than a bricked project
	}
	d.Minutes = d.Minutes.withLists()
	rs.meta.MinutesDraft = &d
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
		if rec.Minutes != nil { // a transcript written before T-0022 may carry null lists
			mn := rec.Minutes.withLists()
			rec.Minutes = &mn
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
	cp.ActiveParticipants = make([]Participant, 0, len(rs.sessions))
	for _, s := range rs.sessions {
		p := Participant{Nick: s.nick, Bio: rs.bioOfLocked(s.nick), JoinedAt: s.joinedAt}
		p.Listening = rs.meta.ClosedAt == nil && rs.watchers[s.nick] > 0
		if t, ok := rs.lastSeen[s.nick]; ok {
			p.LastSeenAt = &t
		}
		cp.ActiveParticipants = append(cp.ActiveParticipants, p)
	}
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
	if err := persistFacilitator(m.dir, id, facilitatorRecord{Hash: hex.EncodeToString(hash[:]), IssuedAt: now}); err != nil {
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
	if !m.isFacilitatorLocked(id, rs, current) {
		return "", ErrNotFacilitator
	}
	return m.rotateFacilitatorLocked(id, rs)
}

// ResetFacilitator mints a replacement facilitator capability for room id
// under dir without the current one (B-0083): the recovery when the
// capability was lost, e.g. a UI handoff whose new capability nobody copied.
// Every earlier capability stops working. It writes the room's sidecar
// directly, so it runs in the CLI past the human gate and no HTTP route hands
// a capability out (B-0093); a running Manager re-reads the sidecar on every
// check (isFacilitatorLocked).
func ResetFacilitator(dir, id string) (string, error) {
	if _, ok := roomNumber(id); !ok || !strings.HasPrefix(id, "R") {
		return "", ErrNotFound // also keeps a path out of the file names below
	}
	if _, err := os.Stat(filepath.Join(dir, id+".jsonl")); errors.Is(err, os.ErrNotExist) {
		return "", ErrNotFound
	} else if err != nil {
		return "", err
	}
	next, err := newSessionToken()
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256([]byte(next))
	if err := persistFacilitator(dir, id, facilitatorRecord{Hash: hex.EncodeToString(hash[:]), IssuedAt: time.Now().UTC()}); err != nil {
		return "", err
	}
	return next, nil
}

// isFacilitatorLocked re-reads room id's facilitator sidecar, then checks
// token against it (B-0093): a reset the CLI wrote counts with no restart. An
// unreadable sidecar keeps the capability already in memory. Caller must hold
// rs.mu.
func (m *Manager) isFacilitatorLocked(id string, rs *roomState, token string) bool {
	_ = loadFacilitator(filepath.Join(m.dir, id+facilitatorSuffix), rs)
	return rs.checkFacilitatorLocked(token)
}

// rotateFacilitatorLocked mints, persists and installs a new facilitator
// capability. Caller must hold rs.mu.
func (m *Manager) rotateFacilitatorLocked(id string, rs *roomState) (string, error) {
	next, err := newSessionToken()
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256([]byte(next))
	if err := persistFacilitator(m.dir, id, facilitatorRecord{Hash: hex.EncodeToString(hash[:]), IssuedAt: time.Now().UTC()}); err != nil {
		return "", err
	}
	rs.facilitatorHash = hash
	return next, nil
}

// facilitatorAuthor is the author of a minutes draft the facilitator saved
// (T-0022); no participant may join under it, in any case or padding.
const facilitatorAuthor = "facilitator"

// Join registers nick as a participant and mints its session capability.
// Rejected when the room is unknown, closed, the nick is the reserved
// facilitatorAuthor, or the nick already has an active session. A legacy
// participant (replayed transcript without session data) may re-join to
// obtain a capability.
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
	if strings.EqualFold(strings.TrimSpace(nick), facilitatorAuthor) {
		return Room{}, Session{}, ErrNickReserved
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
	rs.touchLocked(nick)
	sess := Session{SessionID: token, RoomID: rs.meta.ID, Nick: nick, Bio: bio, JoinedAt: now, LastSeq: len(rs.log)}
	return rs.snapshotLocked(), sess, nil
}

// Leave revokes sessionID's capability and records a public leave event
// (F-0511). The revocation is persisted to the private sidecar before the
// public event, so a crash in between leaves the capability revoked, never a
// leave without effect. The nick stays in the historical Participants and
// may join again for a new capability. Rejected when the room is unknown or
// closed, or the capability is not valid in this room.
func (m *Manager) Leave(id, sessionID string) (Room, error) {
	rs, err := m.getRoom(id)
	if err != nil {
		return Room{}, err
	}
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if rs.meta.ClosedAt != nil {
		return Room{}, ErrClosed
	}
	sa, ok := rs.findSessionLocked(sessionID)
	if !ok {
		return Room{}, ErrInvalidSession
	}
	if err := m.persistSession(rs.meta.ID, sessionRecord{Hash: hex.EncodeToString(sa.hash[:]), Nick: sa.nick, JoinedAt: sa.joinedAt, Revoked: true}); err != nil {
		return Room{}, err
	}
	for i, s := range rs.sessions {
		if s.hash == sa.hash {
			rs.sessions = append(rs.sessions[:i], rs.sessions[i+1:]...)
			break
		}
	}
	delete(rs.watchers, sa.nick)
	delete(rs.lastSeen, sa.nick)
	if err := m.appendLocked(rs, Message{Type: typeLeave, Nick: sa.nick, Time: time.Now().UTC()}); err != nil {
		return Room{}, err
	}
	return rs.snapshotLocked(), nil
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
	rs.touchLocked(sa.nick)
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
	rs.touchLocked(sa.nick)
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
	rs.touchLocked(sa.nick)
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
	if !m.isFacilitatorLocked(id, rs, token) {
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
	if !m.isFacilitatorLocked(id, rs, facilitatorToken) {
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
	return m.close(id, facilitatorToken, &minutes, time.Time{}, allowOpenItems)
}

// close implements Close and CloseWithDraft: a nil given closes with the
// saved draft, refused with ErrDraftChanged when reviewed is set and is not
// that draft's updated_at. Closing drops the draft.
func (m *Manager) close(id, facilitatorToken string, given *Minutes, reviewed time.Time, allowOpenItems bool) (Room, error) {
	rs, err := m.getRoom(id)
	if err != nil {
		return Room{}, err
	}
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if !m.isFacilitatorLocked(id, rs, facilitatorToken) {
		return Room{}, ErrNotFacilitator
	}
	if rs.meta.ClosedAt != nil {
		return rs.snapshotLocked(), nil
	}
	if given == nil {
		if rs.meta.MinutesDraft == nil {
			return Room{}, ErrNoMinutesDraft
		}
		if !reviewed.IsZero() && !reviewed.Equal(rs.meta.MinutesDraft.UpdatedAt) {
			return Room{}, ErrDraftChanged
		}
		given = &rs.meta.MinutesDraft.Minutes
	}
	minutes := given.withLists()
	if err := minutes.validate(); err != nil {
		return Room{}, err
	}
	var openIDs []string
	for _, it := range rs.items {
		if it.Status == itemStatusOpen {
			openIDs = append(openIDs, it.ID)
		}
	}
	if len(openIDs) > 0 && !allowOpenItems {
		return Room{}, ErrOpenItems
	}
	// The forced-close trail is the room's own record (F-0228), never what
	// the minutes claim (T-0022).
	minutes.OpenItems = openIDs
	now := time.Now().UTC()
	if err := m.appendLocked(rs, Message{Type: typeClose, Time: now, Minutes: &minutes}); err != nil {
		return Room{}, err
	}
	rs.meta.ClosedAt = &now
	rs.meta.Minutes = &minutes
	rs.meta.MinutesDraft = nil
	// The close event is already persisted and a closed room never loads its
	// draft, so a file left behind by a failed remove is inert.
	_ = os.Remove(filepath.Join(m.dir, id+minutesDraftSuffix))
	return rs.snapshotLocked(), nil
}

// SaveMinutesDraft replaces the open room's shared minutes draft (T-0022) and
// appends a public minutes_draft event carrying it, without closing the room.
// Any participant (sessionID, authoring as its nick) or the facilitator
// (facilitatorToken, authoring as "facilitator") may save; the draft must
// carry the fields Close requires. The private sidecar is written before the
// event, so a crash in between keeps the draft for the next restart.
func (m *Manager) SaveMinutesDraft(id, sessionID, facilitatorToken string, minutes Minutes) (MinutesDraft, error) {
	rs, err := m.getRoom(id)
	if err != nil {
		return MinutesDraft{}, err
	}
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if rs.meta.ClosedAt != nil {
		return MinutesDraft{}, ErrClosed
	}
	author := facilitatorAuthor
	switch {
	case sessionID != "":
		sa, ok := rs.findSessionLocked(sessionID)
		if !ok {
			return MinutesDraft{}, ErrInvalidSession
		}
		author = sa.nick
	case facilitatorToken != "":
		if !m.isFacilitatorLocked(id, rs, facilitatorToken) {
			return MinutesDraft{}, ErrNotFacilitator
		}
	default:
		return MinutesDraft{}, ErrInvalidSession
	}
	if err := minutes.validate(); err != nil {
		return MinutesDraft{}, err
	}
	minutes.OpenItems = nil // only a forced close records open items (F-0228)
	d := MinutesDraft{Minutes: minutes.withLists(), Author: author, UpdatedAt: time.Now().UTC()}
	if err := m.persistMinutesDraft(id, d); err != nil {
		return MinutesDraft{}, err
	}
	if err := m.appendLocked(rs, Message{Type: typeMinutesDraft, Nick: author, Minutes: &d.Minutes, Time: d.UpdatedAt}); err != nil {
		return MinutesDraft{}, err
	}
	rs.meta.MinutesDraft = &d
	if sessionID != "" {
		rs.touchLocked(author)
	}
	return d, nil
}

// CloseWithDraft is Close with the room's saved minutes draft (T-0022):
// ErrNoMinutesDraft when none was saved. A non-zero reviewed is the
// updated_at of the draft the facilitator read: a draft saved since then
// refuses with ErrDraftChanged, so the room never closes with minutes its
// facilitator did not see. Same facilitator gate, open-items rule and
// idempotency as Close.
func (m *Manager) CloseWithDraft(id, facilitatorToken string, reviewed time.Time, allowOpenItems bool) (Room, error) {
	return m.close(id, facilitatorToken, nil, reviewed, allowOpenItems)
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

// ReadForSession is Read on behalf of sessionID's participant (T-0021),
// returning the reader's nick. It stamps the nick's last_seen_at and, while
// a follow read is in flight, counts it as listening. The count is undone by
// defer, which stamps last_seen_at again as the read ends, so a canceled
// request never leaks one; a session revoked meanwhile (Leave already
// cleared its presence) undoes nothing. Rejected when the capability is not
// valid in this room.
func (m *Manager) ReadForSession(ctx context.Context, id, sessionID string, since int, follow bool, timeout time.Duration) ([]Message, string, error) {
	rs, err := m.getRoom(id)
	if err != nil {
		return nil, "", err
	}
	rs.mu.Lock()
	sa, ok := rs.findSessionLocked(sessionID)
	if !ok {
		rs.mu.Unlock()
		return nil, "", ErrInvalidSession
	}
	rs.touchLocked(sa.nick)
	if follow {
		if rs.watchers == nil {
			rs.watchers = map[string]int{}
		}
		rs.watchers[sa.nick]++
		defer func() {
			rs.mu.Lock()
			defer rs.mu.Unlock()
			if !rs.hasSessionLocked(sa.hash) {
				return
			}
			rs.touchLocked(sa.nick)
			if rs.watchers[sa.nick] > 0 {
				rs.watchers[sa.nick]--
			}
		}()
	}
	rs.mu.Unlock()
	msgs, err := m.Read(ctx, id, since, follow, timeout)
	return msgs, sa.nick, err
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
