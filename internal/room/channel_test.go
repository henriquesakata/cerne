package room

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestChannelAppendAssignsUniqueSeqs (F-0325): two Channel values over one
// dir stand in for two processes; 20 concurrent appends get seqs 1..20, none
// repeated — the lockfile, not process memory, serializes them.
func TestChannelAppendAssignsUniqueSeqs(t *testing.T) {
	dir := t.TempDir()
	a, b := OpenChannel(dir), OpenChannel(dir)
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := range 20 {
		c := a
		if i%2 == 1 {
			c = b
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.Append(Message{Type: typeMessage, Nick: "W-0001", Body: "oi", Time: time.Now().UTC()}); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("Append: %v", err)
	}
	msgs, err := a.Messages()
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	var seqs []int
	for _, m := range msgs {
		seqs = append(seqs, m.Seq)
	}
	sort.Ints(seqs)
	if len(seqs) != 20 {
		t.Fatalf("got %d messages, want 20: %v", len(seqs), seqs)
	}
	for i, s := range seqs {
		if s != i+1 {
			t.Fatalf("seqs = %v, want 1..20", seqs)
		}
	}
}

// TestChannelReadSkipsCorruptLines: a torn or garbage line is skipped, never
// an error — a hook reading a damaged channel must still run (N-0021) — and
// the next append still lands on a line of its own.
func TestChannelReadSkipsCorruptLines(t *testing.T) {
	dir := t.TempDir()
	c := OpenChannel(dir)
	if _, err := c.Append(Message{Type: typeMessage, Nick: "W-0001", Body: "um"}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	f, err := os.OpenFile(filepath.Join(dir, ChannelID, "messages.jsonl"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("garbage\n{\"seq\":2,\"bo")
	f.Close()
	m, err := c.Append(Message{Type: typeMessage, Nick: "W-0001", Body: "dois"})
	if err != nil {
		t.Fatalf("Append after corruption: %v", err)
	}
	if m.Seq != 2 {
		t.Fatalf("seq after corruption = %d, want 2", m.Seq)
	}
	msgs, err := c.Messages()
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	if len(msgs) != 2 || msgs[0].Body != "um" || msgs[1].Body != "dois" {
		t.Fatalf("messages = %+v, want um, dois", msgs)
	}
	if err := os.WriteFile(filepath.Join(dir, ChannelID, "deliveries.jsonl"), []byte("{nope\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if dels, err := c.Deliveries(); err != nil || len(dels) != 0 {
		t.Fatalf("Deliveries = %v, %v; want empty, nil", dels, err)
	}
}

// TestChannelMissingDirIsEmpty: no channel yet reads as empty, not an error,
// and creates nothing.
func TestChannelMissingDirIsEmpty(t *testing.T) {
	dir := t.TempDir()
	c := OpenChannel(dir)
	msgs, err := c.Messages()
	if err != nil || len(msgs) != 0 {
		t.Fatalf("Messages = %v, %v; want empty, nil", msgs, err)
	}
	dels, err := c.Deliveries()
	if err != nil || len(dels) != 0 {
		t.Fatalf("Deliveries = %v, %v; want empty, nil", dels, err)
	}
	if v := c.Version(); v != 0 {
		t.Fatalf("Version = %d, want 0", v)
	}
	if _, err := os.Stat(filepath.Join(dir, ChannelID)); !os.IsNotExist(err) {
		t.Fatalf("reading created the channel dir: %v", err)
	}
}

// TestChannelStaleLockIsStolen: a lock left by a dead holder (older than the
// stale window) is taken over; a fresh one makes the writer give up quickly
// with ErrChannelBusy instead of hanging a hook.
func TestChannelStaleLockIsStolen(t *testing.T) {
	dir := t.TempDir()
	c := OpenChannel(dir)
	lock := filepath.Join(dir, ChannelID, "lock")
	if err := os.MkdirAll(filepath.Dir(lock), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lock, []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	_, err := c.Append(Message{Type: typeMessage, Nick: "W-0001", Body: "oi"})
	if !errors.Is(err, ErrChannelBusy) {
		t.Fatalf("fresh foreign lock: err = %v, want ErrChannelBusy", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("busy append took %v, want ~300ms", d)
	}

	old := time.Now().Add(-11 * time.Second)
	if err := os.Chtimes(lock, old, old); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Append(Message{Type: typeMessage, Nick: "W-0001", Body: "oi"}); err != nil {
		t.Fatalf("stale lock not stolen: %v", err)
	}
	if _, err := os.Stat(lock); !os.IsNotExist(err) {
		t.Fatalf("lock not released after append: %v", err)
	}
}

// TestChannelBodyCap: a body over 2000 runes is refused, so every append
// stays one small write.
func TestChannelBodyCap(t *testing.T) {
	c := OpenChannel(t.TempDir())
	if _, err := c.Append(Message{Type: typeMessage, Body: strings.Repeat("ã", 2000)}); err != nil {
		t.Fatalf("2000 runes refused: %v", err)
	}
	if _, err := c.Append(Message{Type: typeMessage, Body: strings.Repeat("ã", 2001)}); !errors.Is(err, ErrMessageTooLong) {
		t.Fatalf("2001 runes: err = %v, want ErrMessageTooLong", err)
	}
}

// TestChannelUpdateAppendsDelivery: Update hands fn both logs and appends the
// delivery it returns; nil appends nothing.
func TestChannelUpdateAppendsDelivery(t *testing.T) {
	c := OpenChannel(t.TempDir())
	if _, err := c.Append(Message{Type: typeMessage, Nick: "W-0001", Body: "oi"}); err != nil {
		t.Fatal(err)
	}
	v0 := c.Version()
	err := c.Update(func(msgs []Message, dels []Delivery) (*Delivery, error) {
		if len(msgs) != 1 || len(dels) != 0 {
			t.Fatalf("fn got %d msgs, %d dels", len(msgs), len(dels))
		}
		return &Delivery{Session: "s1", Work: "W-0002", Seqs: []int{1}, Through: 1, At: time.Now().UTC()}, nil
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if err := c.Update(func([]Message, []Delivery) (*Delivery, error) { return nil, nil }); err != nil {
		t.Fatalf("Update nil: %v", err)
	}
	dels, err := c.Deliveries()
	if err != nil || len(dels) != 1 || dels[0].Session != "s1" || dels[0].Through != 1 {
		t.Fatalf("Deliveries = %+v, %v", dels, err)
	}
	if c.Version() == v0 {
		t.Fatal("Version unchanged after a delivery was recorded")
	}
}

// TestChannelWaitWakesOnChange: Wait returns as soon as the version moves
// (a message or a delivery), and on timeout otherwise.
func TestChannelWaitWakesOnChange(t *testing.T) {
	c := OpenChannel(t.TempDir())
	v := c.Version()
	go func() {
		time.Sleep(100 * time.Millisecond)
		_, _ = c.Append(Message{Type: typeMessage, Nick: "W-0001", Body: "oi"})
	}()
	start := time.Now()
	got := c.Wait(t.Context(), v, 5*time.Second)
	if got == v || time.Since(start) > 3*time.Second {
		t.Fatalf("Wait = %d after %v, want a new version promptly", got, time.Since(start))
	}
	if again := c.Wait(t.Context(), got, 600*time.Millisecond); again != got {
		t.Fatalf("Wait with no change = %d, want %d", again, got)
	}
}

// TestManagerIgnoresChannelDir: the war room manager never lists the channel
// — its dir is skipped by load, so the two stores coexist under rooms/.
func TestManagerIgnoresChannelDir(t *testing.T) {
	dir := t.TempDir()
	if _, err := OpenChannel(dir).Append(Message{Type: typeMessage, Nick: "W-0001", Body: "oi"}); err != nil {
		t.Fatal(err)
	}
	m, err := NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	for _, r := range m.List() {
		if r.ID == ChannelID {
			t.Fatalf("manager lists the channel: %+v", r)
		}
	}
}

// TestChannelSeqSurvivesTrim (F-0325, critic C16): the seq high-water mark is
// kept apart from messages.jsonl, so trimming or compacting the log never
// hands out a seq below a session's watermark again.
func TestChannelSeqSurvivesTrim(t *testing.T) {
	dir := t.TempDir()
	c := OpenChannel(dir)
	for range 3 {
		if _, err := c.Append(Message{Type: typeMessage, Nick: "W-0001", Body: "oi"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, ChannelID, "messages.jsonl"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := c.Append(Message{Type: typeMessage, Nick: "W-0001", Body: "depois do corte"})
	if err != nil || m.Seq != 4 {
		t.Fatalf("seq after trimming = %d (%v), want 4", m.Seq, err)
	}
}
