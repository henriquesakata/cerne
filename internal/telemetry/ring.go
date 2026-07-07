package telemetry

import "sync"

// ringSize bounds the in-process history of finished operations — enough to
// inspect a burst of approvals, small enough to be negligible memory.
const ringSize = 64

// DefaultRing holds the most recent finished operations, newest first on
// snapshot. Served by /api/debug/timings and `cerne doctor --timings`.
var DefaultRing = newRing(ringSize)

type ring struct {
	mu   sync.Mutex
	buf  []Record
	next int
	full bool
}

func newRing(n int) *ring {
	return &ring{buf: make([]Record, n)}
}

func (r *ring) add(rec Record) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf[r.next] = rec
	r.next = (r.next + 1) % len(r.buf)
	if r.next == 0 {
		r.full = true
	}
}

// snapshot returns the stored records newest-first.
func (r *ring) snapshot() []Record {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := r.next
	if r.full {
		n = len(r.buf)
	}
	out := make([]Record, 0, n)
	for i := 1; i <= n; i++ {
		out = append(out, r.buf[(r.next-i+len(r.buf))%len(r.buf)])
	}
	return out
}

// Snapshot exposes the default ring's records, newest first.
func Snapshot() []Record { return DefaultRing.snapshot() }
