package room

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func newManager(t *testing.T) *Manager {
	t.Helper()
	m, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return m
}

// minimalMinutes satisfies Close's required-field validation with no
// substantive content — for tests that only care about close mechanics, not
// minutes content.
var minimalMinutes = Minutes{Conclusion: "ok", Rationale: "ok"}

func TestOpenAssignsSequentialIDs(t *testing.T) {
	m := newManager(t)
	r1, _, err := m.Open("primeiro topico")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if r1.ID != "R1" {
		t.Fatalf("ID = %q, want R1", r1.ID)
	}
	if r1.Topic != "primeiro topico" {
		t.Fatalf("Topic = %q", r1.Topic)
	}
	if len(r1.Participants) != 0 {
		t.Fatalf("Participants = %v, want empty", r1.Participants)
	}
	r2, _, err := m.Open("segundo")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if r2.ID != "R2" {
		t.Fatalf("ID = %q, want R2", r2.ID)
	}
}

// mustJoin joins nick and returns the minted session.
func mustJoin(t *testing.T, m *Manager, id, nick, bio string) Session {
	t.Helper()
	_, sess, err := m.Join(id, nick, bio)
	if err != nil {
		t.Fatalf("Join %s: %v", nick, err)
	}
	return sess
}

func TestJoinRegistersParticipant(t *testing.T) {
	m := newManager(t)
	r, _, _ := m.Open("topico")
	got, _, err := m.Join(r.ID, "alice", "otimista")
	if err != nil {
		t.Fatalf("Join: %v", err)
	}
	if len(got.Participants) != 1 || got.Participants[0].Nick != "alice" || got.Participants[0].Bio != "otimista" {
		t.Fatalf("Participants = %+v", got.Participants)
	}
}

func TestJoinMintsSessionCapability(t *testing.T) {
	m := newManager(t)
	r, _, _ := m.Open("topico")
	_, sess, err := m.Join(r.ID, "alice", "otimista")
	if err != nil {
		t.Fatalf("Join: %v", err)
	}
	if len(sess.SessionID) < 32 {
		t.Fatalf("SessionID = %q, want >= 32 hex chars (128 bits)", sess.SessionID)
	}
	if _, err := hex.DecodeString(sess.SessionID); err != nil {
		t.Fatalf("SessionID %q not hex: %v", sess.SessionID, err)
	}
	if sess.RoomID != r.ID || sess.Nick != "alice" || sess.Bio != "otimista" {
		t.Fatalf("session = %+v", sess)
	}
	if sess.JoinedAt.IsZero() {
		t.Fatalf("JoinedAt zero")
	}
	if sess.LastSeq != 2 { // open=1, join=2
		t.Fatalf("LastSeq = %d, want 2", sess.LastSeq)
	}
	sess2 := mustJoin(t, m, r.ID, "bob", "")
	if sess2.SessionID == sess.SessionID {
		t.Fatalf("two sessions share the same capability")
	}
}

func TestJoinDuplicateNickRejected(t *testing.T) {
	m := newManager(t)
	r, _, _ := m.Open("topico")
	if _, _, err := m.Join(r.ID, "alice", ""); err != nil {
		t.Fatalf("first Join: %v", err)
	}
	if _, _, err := m.Join(r.ID, "alice", ""); !errors.Is(err, ErrNickTaken) {
		t.Fatalf("second Join err = %v, want ErrNickTaken", err)
	}
}

func TestJoinUnknownRoomNotFound(t *testing.T) {
	m := newManager(t)
	if _, _, err := m.Join("R99", "alice", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestJoinClosedRoomRejected(t *testing.T) {
	m := newManager(t)
	r, fac, _ := m.Open("topico")
	if _, err := m.Close(r.ID, fac, minimalMinutes, false); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, _, err := m.Join(r.ID, "alice", ""); !errors.Is(err, ErrClosed) {
		t.Fatalf("err = %v, want ErrClosed", err)
	}
}

func TestSayAppendsMessageWithSeq(t *testing.T) {
	m := newManager(t)
	r, _, _ := m.Open("topico")
	sess := mustJoin(t, m, r.ID, "alice", "")
	msg, err := m.Say(r.ID, sess.SessionID, "oi pessoal")
	if err != nil {
		t.Fatalf("Say: %v", err)
	}
	if msg.Body != "oi pessoal" || msg.Nick != "alice" || msg.Type != typeMessage {
		t.Fatalf("msg = %+v", msg)
	}
	if msg.Seq <= 0 {
		t.Fatalf("Seq = %d, want > 0", msg.Seq)
	}
}

func TestSayInvalidSessionRejected(t *testing.T) {
	m := newManager(t)
	r, _, _ := m.Open("topico")
	mustJoin(t, m, r.ID, "alice", "")
	other, _, _ := m.Open("outra sala")
	otherSess := mustJoin(t, m, other.ID, "bob", "")

	cases := []struct {
		name, sessionID string
	}{
		{"empty", ""},
		{"random", "deadbeefdeadbeefdeadbeefdeadbeef"},
		{"other room's capability", otherSess.SessionID},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before, _ := m.Read(context.Background(), r.ID, 0, false, time.Second)
			if _, err := m.Say(r.ID, tc.sessionID, "oi"); !errors.Is(err, ErrInvalidSession) {
				t.Fatalf("err = %v, want ErrInvalidSession", err)
			}
			after, _ := m.Read(context.Background(), r.ID, 0, false, time.Second)
			if len(after) != len(before) {
				t.Fatalf("log grew from %d to %d events on rejected Say", len(before), len(after))
			}
		})
	}
}

func TestSayClosedRoomRejected(t *testing.T) {
	m := newManager(t)
	r, fac, _ := m.Open("topico")
	sess := mustJoin(t, m, r.ID, "alice", "")
	if _, err := m.Close(r.ID, fac, minimalMinutes, false); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := m.Say(r.ID, sess.SessionID, "oi"); !errors.Is(err, ErrClosed) {
		t.Fatalf("err = %v, want ErrClosed", err)
	}
}

func TestResumeReturnsSameSessionWithoutEvent(t *testing.T) {
	m := newManager(t)
	r, _, _ := m.Open("topico")
	sess := mustJoin(t, m, r.ID, "alice", "bio-alice")

	before, _ := m.Read(context.Background(), r.ID, 0, false, time.Second)
	got, err := m.Resume(r.ID, sess.SessionID)
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if got.SessionID != sess.SessionID || got.Nick != "alice" || got.Bio != "bio-alice" || got.RoomID != r.ID {
		t.Fatalf("resumed session = %+v", got)
	}
	after, _ := m.Read(context.Background(), r.ID, 0, false, time.Second)
	if len(after) != len(before) {
		t.Fatalf("Resume appended an event: %d -> %d", len(before), len(after))
	}
}

func TestResumeInvalidSessionRejected(t *testing.T) {
	m := newManager(t)
	r, _, _ := m.Open("topico")
	mustJoin(t, m, r.ID, "alice", "")
	if _, err := m.Resume(r.ID, "deadbeefdeadbeefdeadbeefdeadbeef"); !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("err = %v, want ErrInvalidSession", err)
	}
	if _, err := m.Resume("R99", "deadbeef"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestReadSinceFiltersEarlierEvents(t *testing.T) {
	m := newManager(t)
	r, _, _ := m.Open("topico")
	sess := mustJoin(t, m, r.ID, "alice", "")
	first, err := m.Say(r.ID, sess.SessionID, "um")
	if err != nil {
		t.Fatalf("Say: %v", err)
	}
	if _, err := m.Say(r.ID, sess.SessionID, "dois"); err != nil {
		t.Fatalf("Say: %v", err)
	}

	msgs, err := m.Read(context.Background(), r.ID, first.Seq, false, time.Second)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Body != "dois" {
		t.Fatalf("msgs = %+v", msgs)
	}
}

func TestReadFollowWakesOnNewMessage(t *testing.T) {
	m := newManager(t)
	r, _, _ := m.Open("topico")
	sess := mustJoin(t, m, r.ID, "alice", "")
	baseline, err := m.Read(context.Background(), r.ID, 0, false, time.Second)
	if err != nil {
		t.Fatalf("Read baseline: %v", err)
	}
	since := baseline[len(baseline)-1].Seq

	done := make(chan []Message, 1)
	go func() {
		msgs, err := m.Read(context.Background(), r.ID, since, true, 5*time.Second)
		if err != nil {
			msgs = nil
		}
		done <- msgs
	}()

	time.Sleep(50 * time.Millisecond)
	if _, err := m.Say(r.ID, sess.SessionID, "acordou"); err != nil {
		t.Fatalf("Say: %v", err)
	}

	select {
	case msgs := <-done:
		found := false
		for _, msg := range msgs {
			if msg.Body == "acordou" {
				found = true
			}
		}
		if !found {
			t.Fatalf("msgs = %+v, want the new message", msgs)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Read did not wake up on new message")
	}
}

func TestReadFollowTimesOutEmpty(t *testing.T) {
	m := newManager(t)
	r, _, err := m.Open("topico")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	baseline, err := m.Read(context.Background(), r.ID, 0, false, time.Second)
	if err != nil {
		t.Fatalf("Read baseline: %v", err)
	}
	since := baseline[len(baseline)-1].Seq

	start := time.Now()
	msgs, err := m.Read(context.Background(), r.ID, since, true, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("msgs = %+v, want empty on timeout", msgs)
	}
	if time.Since(start) < 100*time.Millisecond {
		t.Fatalf("returned before the timeout elapsed")
	}
}

func TestCloseKeepsTranscriptReadable(t *testing.T) {
	m := newManager(t)
	r, fac, _ := m.Open("topico")
	sess := mustJoin(t, m, r.ID, "alice", "")
	if _, err := m.Say(r.ID, sess.SessionID, "oi"); err != nil {
		t.Fatalf("Say: %v", err)
	}
	if _, err := m.Close(r.ID, fac, minimalMinutes, false); err != nil {
		t.Fatalf("Close: %v", err)
	}
	msgs, err := m.Read(context.Background(), r.ID, 0, false, time.Second)
	if err != nil {
		t.Fatalf("Read after close: %v", err)
	}
	if len(msgs) == 0 {
		t.Fatalf("msgs empty, want transcript preserved after close")
	}
}

func TestListIncludesClosedRooms(t *testing.T) {
	m := newManager(t)
	r1, _, _ := m.Open("um")
	r2, fac2, _ := m.Open("dois")
	if _, err := m.Close(r2.ID, fac2, minimalMinutes, false); err != nil {
		t.Fatalf("Close: %v", err)
	}

	rooms := m.List()
	if len(rooms) != 2 {
		t.Fatalf("List = %d rooms, want 2", len(rooms))
	}
	var closedSeen bool
	for _, rm := range rooms {
		if rm.ID == r2.ID {
			if rm.ClosedAt == nil {
				t.Fatalf("room %s ClosedAt = nil, want set", rm.ID)
			}
			closedSeen = true
		}
		if rm.ID == r1.ID && rm.ClosedAt != nil {
			t.Fatalf("room %s ClosedAt set, want open", rm.ID)
		}
	}
	if !closedSeen {
		t.Fatalf("closed room %s not in List", r2.ID)
	}
}

func TestSayConcurrentProducesDenseOrderedSeq(t *testing.T) {
	m := newManager(t)
	r, _, _ := m.Open("topico")
	sess := mustJoin(t, m, r.ID, "alice", "")

	const n = 50
	var wg sync.WaitGroup
	errs := make(chan error, n)
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			if _, err := m.Say(r.ID, sess.SessionID, "msg"); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("Say: %v", err)
	}

	msgs, err := m.Read(context.Background(), r.ID, 0, false, time.Second)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	seqs := map[int]bool{}
	min, max := -1, -1
	for _, msg := range msgs {
		if msg.Type != typeMessage {
			continue
		}
		if seqs[msg.Seq] {
			t.Fatalf("duplicate seq %d", msg.Seq)
		}
		seqs[msg.Seq] = true
		if min == -1 || msg.Seq < min {
			min = msg.Seq
		}
		if max == -1 || msg.Seq > max {
			max = msg.Seq
		}
	}
	if len(seqs) != n {
		t.Fatalf("got %d distinct message seqs, want %d", len(seqs), n)
	}
	if max-min+1 != n {
		t.Fatalf("seq range [%d,%d] not dense for %d messages", min, max, n)
	}

	f, err := os.Open(filepath.Join(m.dir, r.ID+".jsonl"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer f.Close()
	var fileSeqs []int
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var rec Message
		if err := json.Unmarshal(bytes.TrimSpace(sc.Bytes()), &rec); err != nil {
			t.Fatalf("Unmarshal line: %v", err)
		}
		if rec.Type == typeMessage {
			fileSeqs = append(fileSeqs, rec.Seq)
		}
	}
	if len(fileSeqs) != n {
		t.Fatalf("file has %d message lines, want %d", len(fileSeqs), n)
	}
	for i := 1; i < len(fileSeqs); i++ {
		if fileSeqs[i] <= fileSeqs[i-1] {
			t.Fatalf("file seqs not increasing at %d: %v", i, fileSeqs)
		}
	}
}

func TestManagerLoadReplaysExistingFiles(t *testing.T) {
	dir := t.TempDir()
	m1, err := NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	r, _, err := m1.Open("topico persistente")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	sess := mustJoin(t, m1, r.ID, "alice", "bio-alice")
	if _, err := m1.Say(r.ID, sess.SessionID, "primeira mensagem"); err != nil {
		t.Fatalf("Say: %v", err)
	}

	m2, err := NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager (reload): %v", err)
	}
	rooms := m2.List()
	if len(rooms) != 1 {
		t.Fatalf("List = %d rooms, want 1", len(rooms))
	}
	got := rooms[0]
	if got.ID != r.ID || got.Topic != "topico persistente" {
		t.Fatalf("room = %+v", got)
	}
	if len(got.Participants) != 1 || got.Participants[0].Nick != "alice" {
		t.Fatalf("Participants = %+v", got.Participants)
	}

	msgs, err := m2.Read(context.Background(), r.ID, 0, false, time.Second)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	var sawMessage bool
	for _, msg := range msgs {
		if msg.Type == typeMessage && msg.Body == "primeira mensagem" {
			sawMessage = true
		}
	}
	if !sawMessage {
		t.Fatalf("msgs = %+v, want the replayed message", msgs)
	}

	r2, _, err := m2.Open("novo topico")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if r2.ID == r.ID {
		t.Fatalf("new room ID collides with reloaded room: %s", r2.ID)
	}
}

func TestSessionSidecarIsPrivateAndTokenNeverPersisted(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	r, _, _ := m.Open("topico")
	sess := mustJoin(t, m, r.ID, "alice", "bio")

	public, err := os.ReadFile(filepath.Join(dir, r.ID+".jsonl"))
	if err != nil {
		t.Fatalf("read public transcript: %v", err)
	}
	hash := sha256.Sum256([]byte(sess.SessionID))
	for name, needle := range map[string]string{"token": sess.SessionID, "hash": hex.EncodeToString(hash[:])} {
		if bytes.Contains(public, []byte(needle)) {
			t.Fatalf("public transcript leaks the session %s", name)
		}
	}

	info, err := os.Stat(filepath.Join(dir, r.ID+".sessions.jsonl"))
	if err != nil {
		t.Fatalf("stat sidecar: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("sidecar mode = %o, want 600", perm)
	}
	sidecar, err := os.ReadFile(filepath.Join(dir, r.ID+".sessions.jsonl"))
	if err != nil {
		t.Fatalf("read sidecar: %v", err)
	}
	if bytes.Contains(sidecar, []byte(sess.SessionID)) {
		t.Fatalf("sidecar stores the raw token, want hash only")
	}
	if !bytes.Contains(sidecar, []byte(hex.EncodeToString(hash[:]))) {
		t.Fatalf("sidecar missing the token hash")
	}
}

func TestReplayRestoresSessionsAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	m1, err := NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	r, _, _ := m1.Open("topico")
	sess := mustJoin(t, m1, r.ID, "alice", "bio-alice")

	m2, err := NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager (reload): %v", err)
	}
	msg, err := m2.Say(r.ID, sess.SessionID, "sobrevivi ao restart")
	if err != nil {
		t.Fatalf("Say with pre-restart capability: %v", err)
	}
	if msg.Nick != "alice" {
		t.Fatalf("Nick = %q, want alice", msg.Nick)
	}
	got, err := m2.Resume(r.ID, sess.SessionID)
	if err != nil {
		t.Fatalf("Resume after restart: %v", err)
	}
	if got.Nick != "alice" || got.Bio != "bio-alice" {
		t.Fatalf("resumed session = %+v", got)
	}
}

func TestReplayIgnoresOrphanSidecarRecord(t *testing.T) {
	dir := t.TempDir()
	m1, err := NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	r, _, _ := m1.Open("topico")
	// simulate a crash between the private record and the public join event:
	// a sidecar row whose nick has no join in the transcript.
	orphan := sha256.Sum256([]byte("orphan-token"))
	line, _ := json.Marshal(map[string]any{"hash": hex.EncodeToString(orphan[:]), "nick": "ghost", "joined_at": time.Now().UTC()})
	f, err := os.OpenFile(filepath.Join(dir, r.ID+".sessions.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open sidecar: %v", err)
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		t.Fatalf("write sidecar: %v", err)
	}
	f.Close()

	m2, err := NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager (reload): %v", err)
	}
	if _, err := m2.Say(r.ID, "orphan-token", "boo"); !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("orphan capability accepted: err = %v, want ErrInvalidSession", err)
	}
}

func TestLegacyTranscriptReadableAndNickRejoinable(t *testing.T) {
	dir := t.TempDir()
	// hand-written legacy transcript: joins/messages, no sessions sidecar.
	legacy := `{"seq":1,"type":"open","topic":"legado","time":"2026-08-01T00:00:00Z"}
{"seq":2,"type":"join","nick":"alice","bio":"veterana","time":"2026-08-01T00:00:01Z"}
{"seq":3,"type":"message","nick":"alice","body":"mensagem antiga","time":"2026-08-01T00:00:02Z"}
`
	if err := os.WriteFile(filepath.Join(dir, "R1.jsonl"), []byte(legacy), 0o644); err != nil {
		t.Fatalf("write legacy transcript: %v", err)
	}
	m, err := NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	msgs, err := m.Read(context.Background(), "R1", 0, false, time.Second)
	if err != nil || len(msgs) != 3 {
		t.Fatalf("Read legacy = %d msgs, err %v; want 3", len(msgs), err)
	}
	// legacy participant has no capability: nothing authenticates as alice...
	if _, err := m.Say("R1", "anything", "oi"); !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("err = %v, want ErrInvalidSession", err)
	}
	// ...until a fresh join mints one (criterion 8); the nick is NOT a collision.
	_, sess, err := m.Join("R1", "alice", "veterana")
	if err != nil {
		t.Fatalf("legacy nick re-join: %v", err)
	}
	if msg, err := m.Say("R1", sess.SessionID, "voltei"); err != nil || msg.Nick != "alice" {
		t.Fatalf("Say after re-join: msg = %+v, err %v", msg, err)
	}
	// with an active session, the nick collides again (criterion 5).
	if _, _, err := m.Join("R1", "alice", ""); !errors.Is(err, ErrNickTaken) {
		t.Fatalf("err = %v, want ErrNickTaken", err)
	}
}

// --- F-0228 criterion 1: facilitator capability ---

func TestOpenMintsFacilitatorCapability(t *testing.T) {
	m := newManager(t)
	r, fac, err := m.Open("topico")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if len(fac) < 32 {
		t.Fatalf("facilitator capability = %q, want >= 32 hex chars", fac)
	}
	if _, err := hex.DecodeString(fac); err != nil {
		t.Fatalf("facilitator capability %q not hex: %v", fac, err)
	}
	// never appears in the room snapshot or the transcript.
	if r.ID == "" {
		t.Fatalf("room not opened")
	}
	msgs, err := m.Read(context.Background(), r.ID, 0, false, time.Second)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	for _, msg := range msgs {
		raw, _ := json.Marshal(msg)
		if bytes.Contains(raw, []byte(fac)) {
			t.Fatalf("facilitator capability leaked into transcript: %s", raw)
		}
	}
}

func TestHandoffFacilitatorRotatesAtomically(t *testing.T) {
	m := newManager(t)
	r, fac, _ := m.Open("topico")
	newFac, err := m.HandoffFacilitator(r.ID, fac)
	if err != nil {
		t.Fatalf("HandoffFacilitator: %v", err)
	}
	if newFac == fac {
		t.Fatalf("handoff returned the same capability")
	}
	// the old capability is immediately invalid.
	if _, err := m.Resolve(r.ID, fac, "I1", "n/a"); !errors.Is(err, ErrNotFacilitator) {
		t.Fatalf("old capability err = %v, want ErrNotFacilitator", err)
	}
	// the new one works (idempotent Close proves it's accepted).
	if _, err := m.Close(r.ID, newFac, minimalMinutes, false); err != nil {
		t.Fatalf("Close with new facilitator capability: %v", err)
	}
}

func TestHandoffFacilitatorRejectsWrongCapability(t *testing.T) {
	m := newManager(t)
	r, _, _ := m.Open("topico")
	if _, err := m.HandoffFacilitator(r.ID, "deadbeefdeadbeefdeadbeefdeadbeef"); !errors.Is(err, ErrNotFacilitator) {
		t.Fatalf("err = %v, want ErrNotFacilitator", err)
	}
}

func TestFacilitatorCapabilitySurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	m1, err := NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	r, fac, _ := m1.Open("topico")
	newFac, err := m1.HandoffFacilitator(r.ID, fac)
	if err != nil {
		t.Fatalf("HandoffFacilitator: %v", err)
	}

	m2, err := NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager (restart): %v", err)
	}
	// old capability still invalid, rotated one still valid, after replay.
	if _, err := m2.Close(r.ID, fac, minimalMinutes, false); !errors.Is(err, ErrNotFacilitator) {
		t.Fatalf("stale capability after restart err = %v, want ErrNotFacilitator", err)
	}
	if _, err := m2.Close(r.ID, newFac, minimalMinutes, false); err != nil {
		t.Fatalf("Close with rotated capability after restart: %v", err)
	}
}

// --- F-0228 criterion 2: typed proposal/decision-request items ---

func TestProposeAppendsTypedEventPreservingSeq(t *testing.T) {
	m := newManager(t)
	r, _, _ := m.Open("topico")
	sess := mustJoin(t, m, r.ID, "alice", "")
	item, err := m.Propose(r.ID, sess.SessionID, "usar postgres", "menos manutenção")
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if item.Kind != typeProposal || item.Author != "alice" || item.Title != "usar postgres" || item.Status != itemStatusOpen {
		t.Fatalf("item = %+v", item)
	}
	if item.SourceSeq != 3 { // open=1, join=2, proposal=3
		t.Fatalf("SourceSeq = %d, want 3", item.SourceSeq)
	}
	if item.ID != "I3" {
		t.Fatalf("ID = %q, want I3 (derived from seq)", item.ID)
	}
}

func TestRequestDecisionAppendsTypedEvent(t *testing.T) {
	m := newManager(t)
	r, _, _ := m.Open("topico")
	sess := mustJoin(t, m, r.ID, "alice", "")
	item, err := m.RequestDecision(r.ID, sess.SessionID, "qual banco?", "postgres ou sqlite")
	if err != nil {
		t.Fatalf("RequestDecision: %v", err)
	}
	if item.Kind != typeDecisionRequest {
		t.Fatalf("Kind = %q, want %q", item.Kind, typeDecisionRequest)
	}
}

func TestProposeRejectsUnauthenticatedSession(t *testing.T) {
	m := newManager(t)
	r, _, _ := m.Open("topico")
	if _, err := m.Propose(r.ID, "deadbeefdeadbeefdeadbeefdeadbeef", "t", "b"); !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("err = %v, want ErrInvalidSession", err)
	}
}

func TestItemsReplayAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	m1, err := NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	r, fac, _ := m1.Open("topico")
	sess := mustJoin(t, m1, r.ID, "alice", "")
	item, err := m1.Propose(r.ID, sess.SessionID, "usar postgres", "menos manutenção")
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if _, err := m1.Resolve(r.ID, fac, item.ID, "aceito"); err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	m2, err := NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager (restart): %v", err)
	}
	pending, err := m2.Pending(r.ID)
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("pending after restart = %+v, want empty (item was resolved before restart)", pending)
	}
}

// --- F-0228 criteria 3-4: pending listing + facilitator-only resolve ---

func TestPendingListsOnlyOpenItemsInSeqOrder(t *testing.T) {
	m := newManager(t)
	r, fac, _ := m.Open("topico")
	sess := mustJoin(t, m, r.ID, "alice", "")
	first, _ := m.Propose(r.ID, sess.SessionID, "primeiro", "")
	second, _ := m.RequestDecision(r.ID, sess.SessionID, "segundo", "")
	if _, err := m.Resolve(r.ID, fac, first.ID, "resolvido"); err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	pending, err := m.Pending(r.ID)
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	if len(pending) != 1 || pending[0].ID != second.ID {
		t.Fatalf("pending = %+v, want only %q", pending, second.ID)
	}
}

func TestResolveRequiresFacilitatorCapability(t *testing.T) {
	m := newManager(t)
	r, _, _ := m.Open("topico")
	sess := mustJoin(t, m, r.ID, "alice", "")
	item, _ := m.Propose(r.ID, sess.SessionID, "t", "b")

	if _, err := m.Resolve(r.ID, sess.SessionID, item.ID, "aceito"); !errors.Is(err, ErrNotFacilitator) {
		t.Fatalf("resolve with a session (not facilitator) err = %v, want ErrNotFacilitator", err)
	}
}

func TestResolveMarksItemResolvedWithResultAndRefSeq(t *testing.T) {
	m := newManager(t)
	r, fac, _ := m.Open("topico")
	sess := mustJoin(t, m, r.ID, "alice", "")
	item, _ := m.Propose(r.ID, sess.SessionID, "t", "b")

	resolved, err := m.Resolve(r.ID, fac, item.ID, "aceito")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolved.Status != itemStatusResolved || resolved.Result != "aceito" || resolved.ResolutionSeq <= item.SourceSeq {
		t.Fatalf("resolved = %+v", resolved)
	}
}

func TestResolveRejectsUnknownOrAlreadyResolvedItem(t *testing.T) {
	m := newManager(t)
	r, fac, _ := m.Open("topico")
	sess := mustJoin(t, m, r.ID, "alice", "")
	item, _ := m.Propose(r.ID, sess.SessionID, "t", "b")

	if _, err := m.Resolve(r.ID, fac, "I999", "x"); !errors.Is(err, ErrItemNotFound) {
		t.Fatalf("resolve unknown item err = %v, want ErrItemNotFound", err)
	}
	if _, err := m.Resolve(r.ID, fac, item.ID, "aceito"); err != nil {
		t.Fatalf("first resolve: %v", err)
	}
	if _, err := m.Resolve(r.ID, fac, item.ID, "de novo"); !errors.Is(err, ErrItemNotFound) {
		t.Fatalf("resolve already-resolved item err = %v, want ErrItemNotFound", err)
	}
}

// --- F-0228 criteria 5-7: structured close ---

func TestCloseRequiresFacilitatorCapability(t *testing.T) {
	m := newManager(t)
	r, _, _ := m.Open("topico")
	if _, err := m.Close(r.ID, "deadbeefdeadbeefdeadbeefdeadbeef", minimalMinutes, false); !errors.Is(err, ErrNotFacilitator) {
		t.Fatalf("err = %v, want ErrNotFacilitator", err)
	}
}

func TestCloseBlocksOnOpenItemsByDefault(t *testing.T) {
	m := newManager(t)
	r, fac, _ := m.Open("topico")
	sess := mustJoin(t, m, r.ID, "alice", "")
	m.Propose(r.ID, sess.SessionID, "t", "b")

	if _, err := m.Close(r.ID, fac, minimalMinutes, false); !errors.Is(err, ErrOpenItems) {
		t.Fatalf("err = %v, want ErrOpenItems", err)
	}
	// the room is still open: Say still works.
	if _, err := m.Say(r.ID, sess.SessionID, "ainda aberta"); err != nil {
		t.Fatalf("Say after blocked close: %v", err)
	}
}

func TestCloseAllowOpenItemsRecordsExceptionInMinutes(t *testing.T) {
	m := newManager(t)
	r, fac, _ := m.Open("topico")
	sess := mustJoin(t, m, r.ID, "alice", "")
	item, _ := m.Propose(r.ID, sess.SessionID, "t", "b")

	closed, err := m.Close(r.ID, fac, minimalMinutes, true)
	if err != nil {
		t.Fatalf("Close --allow-open-items: %v", err)
	}
	if closed.Minutes == nil || len(closed.Minutes.OpenItems) != 1 || closed.Minutes.OpenItems[0] != item.ID {
		t.Fatalf("minutes = %+v, want the open item recorded as an exception", closed.Minutes)
	}
}

func TestCloseRejectsIncompleteMinutes(t *testing.T) {
	m := newManager(t)
	cases := []struct {
		name string
		mn   Minutes
	}{
		{"empty conclusion", Minutes{Rationale: "ok"}},
		{"empty rationale", Minutes{Conclusion: "ok"}},
		{"both blank", Minutes{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, fac, _ := m.Open("topico " + tc.name)
			if _, err := m.Close(r.ID, fac, tc.mn, false); !errors.Is(err, ErrIncompleteMinutes) {
				t.Fatalf("err = %v, want ErrIncompleteMinutes", err)
			}
		})
	}
	// lists left empty are fine — only the scalar fields are required.
	r, fac, _ := m.Open("topico listas vazias")
	if _, err := m.Close(r.ID, fac, minimalMinutes, false); err != nil {
		t.Fatalf("Close with empty lists: %v", err)
	}
}

func TestCloseIdempotentReturnsSameMinutes(t *testing.T) {
	m := newManager(t)
	r, fac, _ := m.Open("topico")
	first, err := m.Close(r.ID, fac, minimalMinutes, false)
	if err != nil {
		t.Fatalf("first Close: %v", err)
	}
	second, err := m.Close(r.ID, fac, Minutes{Conclusion: "outra coisa", Rationale: "ignorada"}, false)
	if err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if second.Minutes.Conclusion != first.Minutes.Conclusion {
		t.Fatalf("idempotent close changed the minutes: %+v -> %+v", first.Minutes, second.Minutes)
	}
}

func TestCloseMinutesSurviveRestart(t *testing.T) {
	dir := t.TempDir()
	m1, err := NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	r, fac, _ := m1.Open("topico")
	mn := Minutes{Conclusion: "reverter o deploy", Rationale: "mais rápido", Owners: []string{"alice"}}
	if _, err := m1.Close(r.ID, fac, mn, false); err != nil {
		t.Fatalf("Close: %v", err)
	}

	m2, err := NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager (restart): %v", err)
	}
	rooms := m2.List()
	if len(rooms) != 1 || rooms[0].Minutes == nil || rooms[0].Minutes.Conclusion != mn.Conclusion {
		t.Fatalf("rooms after restart = %+v, want the minutes preserved", rooms)
	}
}

func TestCloseStillRejectsJoinAndSayReadStillWorks(t *testing.T) {
	m := newManager(t)
	r, fac, _ := m.Open("topico")
	sess := mustJoin(t, m, r.ID, "alice", "")
	if _, err := m.Close(r.ID, fac, minimalMinutes, false); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, _, err := m.Join(r.ID, "bob", ""); !errors.Is(err, ErrClosed) {
		t.Fatalf("Join after close err = %v, want ErrClosed", err)
	}
	if _, err := m.Say(r.ID, sess.SessionID, "oi"); !errors.Is(err, ErrClosed) {
		t.Fatalf("Say after close err = %v, want ErrClosed", err)
	}
	if _, err := m.Read(context.Background(), r.ID, 0, false, time.Second); err != nil {
		t.Fatalf("Read after close: %v", err)
	}
}
