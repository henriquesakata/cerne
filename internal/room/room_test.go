package room

import (
	"bufio"
	"bytes"
	"context"
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

func TestOpenAssignsSequentialIDs(t *testing.T) {
	m := newManager(t)
	r1, err := m.Open("primeiro topico")
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
	r2, err := m.Open("segundo")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if r2.ID != "R2" {
		t.Fatalf("ID = %q, want R2", r2.ID)
	}
}

func TestJoinRegistersParticipant(t *testing.T) {
	m := newManager(t)
	r, _ := m.Open("topico")
	got, err := m.Join(r.ID, "alice", "otimista")
	if err != nil {
		t.Fatalf("Join: %v", err)
	}
	if len(got.Participants) != 1 || got.Participants[0].Nick != "alice" || got.Participants[0].Bio != "otimista" {
		t.Fatalf("Participants = %+v", got.Participants)
	}
}

func TestJoinDuplicateNickRejected(t *testing.T) {
	m := newManager(t)
	r, _ := m.Open("topico")
	if _, err := m.Join(r.ID, "alice", ""); err != nil {
		t.Fatalf("first Join: %v", err)
	}
	if _, err := m.Join(r.ID, "alice", ""); !errors.Is(err, ErrNickTaken) {
		t.Fatalf("second Join err = %v, want ErrNickTaken", err)
	}
}

func TestJoinUnknownRoomNotFound(t *testing.T) {
	m := newManager(t)
	if _, err := m.Join("R99", "alice", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestJoinClosedRoomRejected(t *testing.T) {
	m := newManager(t)
	r, _ := m.Open("topico")
	if _, err := m.Close(r.ID); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := m.Join(r.ID, "alice", ""); !errors.Is(err, ErrClosed) {
		t.Fatalf("err = %v, want ErrClosed", err)
	}
}

func TestSayAppendsMessageWithSeq(t *testing.T) {
	m := newManager(t)
	r, _ := m.Open("topico")
	if _, err := m.Join(r.ID, "alice", ""); err != nil {
		t.Fatalf("Join: %v", err)
	}
	msg, err := m.Say(r.ID, "alice", "oi pessoal")
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

func TestSayNotJoinedRejected(t *testing.T) {
	m := newManager(t)
	r, _ := m.Open("topico")
	if _, err := m.Say(r.ID, "alice", "oi"); !errors.Is(err, ErrNotJoined) {
		t.Fatalf("err = %v, want ErrNotJoined", err)
	}
}

func TestSayClosedRoomRejected(t *testing.T) {
	m := newManager(t)
	r, _ := m.Open("topico")
	if _, err := m.Join(r.ID, "alice", ""); err != nil {
		t.Fatalf("Join: %v", err)
	}
	if _, err := m.Close(r.ID); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := m.Say(r.ID, "alice", "oi"); !errors.Is(err, ErrClosed) {
		t.Fatalf("err = %v, want ErrClosed", err)
	}
}

func TestReadSinceFiltersEarlierEvents(t *testing.T) {
	m := newManager(t)
	r, _ := m.Open("topico")
	if _, err := m.Join(r.ID, "alice", ""); err != nil {
		t.Fatalf("Join: %v", err)
	}
	first, err := m.Say(r.ID, "alice", "um")
	if err != nil {
		t.Fatalf("Say: %v", err)
	}
	if _, err := m.Say(r.ID, "alice", "dois"); err != nil {
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
	r, _ := m.Open("topico")
	if _, err := m.Join(r.ID, "alice", ""); err != nil {
		t.Fatalf("Join: %v", err)
	}
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
	if _, err := m.Say(r.ID, "alice", "acordou"); err != nil {
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
	r, err := m.Open("topico")
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
	r, _ := m.Open("topico")
	if _, err := m.Join(r.ID, "alice", ""); err != nil {
		t.Fatalf("Join: %v", err)
	}
	if _, err := m.Say(r.ID, "alice", "oi"); err != nil {
		t.Fatalf("Say: %v", err)
	}
	if _, err := m.Close(r.ID); err != nil {
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
	r1, _ := m.Open("um")
	r2, _ := m.Open("dois")
	if _, err := m.Close(r2.ID); err != nil {
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
	r, _ := m.Open("topico")
	if _, err := m.Join(r.ID, "alice", ""); err != nil {
		t.Fatalf("Join: %v", err)
	}

	const n = 50
	var wg sync.WaitGroup
	errs := make(chan error, n)
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			if _, err := m.Say(r.ID, "alice", "msg"); err != nil {
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
	r, err := m1.Open("topico persistente")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := m1.Join(r.ID, "alice", "bio-alice"); err != nil {
		t.Fatalf("Join: %v", err)
	}
	if _, err := m1.Say(r.ID, "alice", "primeira mensagem"); err != nil {
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

	r2, err := m2.Open("novo topico")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if r2.ID == r.ID {
		t.Fatalf("new room ID collides with reloaded room: %s", r2.ID)
	}
}
