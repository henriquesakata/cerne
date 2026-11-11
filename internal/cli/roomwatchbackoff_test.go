package cli

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/room"
)

// fakeRoomReader replays a fixed sequence of readAsSession results (error or
// events), repeating the last step once exhausted — enough to drive the
// backoff loop deterministically without a real server.
type fakeRoomReader struct {
	steps []func() ([]roomWatchEvent, error)
	i     int
	calls int
}

func (f *fakeRoomReader) readAsSession(ctx context.Context, id string, since int, session string) ([]roomWatchEvent, error) {
	f.calls++
	step := f.steps[f.i]
	if f.i < len(f.steps)-1 {
		f.i++
	}
	return step()
}

func transientStep() ([]roomWatchEvent, error) {
	return nil, fmt.Errorf("%w: simulated transient failure", errRoomTransient)
}

func emptyStep() ([]roomWatchEvent, error) {
	return nil, nil
}

func eventStep(seq int, nick, body string) func() ([]roomWatchEvent, error) {
	return func() ([]roomWatchEvent, error) {
		f := false
		msg := room.Message{Seq: seq, Type: "message", Nick: nick, Body: body}
		return []roomWatchEvent{{Message: msg, Own: &f}}, nil
	}
}

func TestRoomWatchBackoffOnTransientThenRecovers(t *testing.T) {
	reader := &fakeRoomReader{steps: []func() ([]roomWatchEvent, error){
		transientStep,
		transientStep,
		emptyStep, // success resets the backoff attempt counter
		transientStep,
		eventStep(1, "bob", "oi"),
	}}

	var slept []time.Duration
	old := roomWatchSleep
	roomWatchSleep = func(ctx context.Context, d time.Duration) { slept = append(slept, d) }
	defer func() { roomWatchSleep = old }()

	cpPath := filepath.Join(t.TempDir(), "cursor.json")
	err := roomWatchLoop(context.Background(), io.Discard, reader, "R1", "sess", cpPath, 0, true)
	if err != nil {
		t.Fatalf("roomWatchLoop: %v", err)
	}
	if len(slept) != 3 {
		t.Fatalf("slept %d times (%v), want 3 (2 before the empty success, 1 after it)", len(slept), slept)
	}
	if slept[1] <= slept[0] {
		t.Fatalf("backoff durations = %v, want the 2nd consecutive transient failure to sleep longer than the 1st", slept)
	}
	if slept[2] != slept[0] {
		t.Fatalf("backoff after a successful (empty) read = %v, want it reset back to the 1st attempt's duration %v", slept[2], slept[0])
	}
}

func TestRoomWatchBackoffCappedAtMax(t *testing.T) {
	steps := make([]func() ([]roomWatchEvent, error), 0, 12)
	for i := 0; i < 11; i++ {
		steps = append(steps, transientStep)
	}
	steps = append(steps, eventStep(1, "bob", "oi"))
	reader := &fakeRoomReader{steps: steps}

	var slept []time.Duration
	old := roomWatchSleep
	roomWatchSleep = func(ctx context.Context, d time.Duration) { slept = append(slept, d) }
	defer func() { roomWatchSleep = old }()

	cpPath := filepath.Join(t.TempDir(), "cursor.json")
	if err := roomWatchLoop(context.Background(), io.Discard, reader, "R1", "sess", cpPath, 0, true); err != nil {
		t.Fatalf("roomWatchLoop: %v", err)
	}
	for _, d := range slept {
		if d > roomWatchBackoffMax {
			t.Fatalf("backoff duration %v exceeds the cap %v", d, roomWatchBackoffMax)
		}
	}
	if slept[len(slept)-1] != roomWatchBackoffMax {
		t.Fatalf("last backoff (after %d consecutive failures) = %v, want it to have reached the cap %v", len(slept), slept[len(slept)-1], roomWatchBackoffMax)
	}
}

func TestRoomWatchDefinitiveErrorSkipsBackoff(t *testing.T) {
	reader := &fakeRoomReader{steps: []func() ([]roomWatchEvent, error){
		func() ([]roomWatchEvent, error) { return nil, fmt.Errorf("room not found") },
	}}
	var slept []time.Duration
	old := roomWatchSleep
	roomWatchSleep = func(ctx context.Context, d time.Duration) { slept = append(slept, d) }
	defer func() { roomWatchSleep = old }()

	cpPath := filepath.Join(t.TempDir(), "cursor.json")
	err := roomWatchLoop(context.Background(), io.Discard, reader, "R1", "sess", cpPath, 0, true)
	if err == nil {
		t.Fatal("roomWatchLoop with a definitive error: want error, got nil")
	}
	if len(slept) != 0 {
		t.Fatalf("slept %v on a definitive error, want no backoff at all", slept)
	}
}

func TestRoomWatchCtrlCDuringBackoffExits0(t *testing.T) {
	reader := &fakeRoomReader{steps: []func() ([]roomWatchEvent, error){transientStep}}
	// real roomWatchSleep (not overridden): proves Ctrl+C interrupts an
	// in-progress backoff wait instead of blocking it out.
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	cpPath := filepath.Join(t.TempDir(), "cursor.json")
	start := time.Now()
	err := roomWatchLoop(ctx, io.Discard, reader, "R1", "sess", cpPath, 0, true)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("roomWatchLoop cancelled mid-backoff: want nil (clean exit), got %v", err)
	}
	if elapsed > 200*time.Millisecond {
		t.Fatalf("roomWatchLoop took %v to return after cancel, want it to return promptly (well under the %v backoff)", elapsed, roomWatchBackoffBase)
	}
}
