// Package telemetry instruments cerne's hot paths (HTTP requests, lifecycle
// transitions, lint, compile) with structured timing: one Op per operation,
// named phases inside it, a bounded in-process ring of recent records, and a
// structured log line per finished Op (F-0149). By default the only output is
// the local log — nothing ever leaves the host without explicit configuration.
package telemetry

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"sync"
	"time"
)

// PhaseRecord is one timed phase of an Op, in insertion order.
type PhaseRecord struct {
	Name string        `json:"name"`
	Dur  time.Duration `json:"dur_ns"`
	Ms   float64       `json:"ms"`
}

// Record is the finished, immutable snapshot of an Op, as stored in the ring
// and serialized by /api/debug/timings.
type Record struct {
	ID      string        `json:"op_id"`
	Kind    string        `json:"kind"`
	Route   string        `json:"route,omitempty"`
	Project string        `json:"project,omitempty"`
	Status  string        `json:"status"`
	Start   time.Time     `json:"start"`
	Dur     time.Duration `json:"dur_ns"`
	Ms      float64       `json:"ms"`
	Retries int           `json:"retries,omitempty"`
	Phases  []PhaseRecord `json:"phases,omitempty"`
}

// Op is one in-flight operation. All methods are safe on a nil receiver, so
// instrumented code never has to guard the "no telemetry" path.
type Op struct {
	ID      string
	Kind    string
	Route   string
	Project string
	Start   time.Time

	mu      sync.Mutex
	phases  []PhaseRecord
	retries int
}

// StartOp mints an Op with a fresh random id.
func StartOp(kind string) *Op {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return &Op{ID: hex.EncodeToString(b[:]), Kind: kind, Start: time.Now()}
}

// Phase starts a named phase and returns the func that ends it, recording the
// elapsed duration. Phases are kept in call order.
func (o *Op) Phase(name string) func() {
	if o == nil {
		return func() {}
	}
	start := time.Now()
	return func() {
		d := time.Since(start)
		o.mu.Lock()
		o.phases = append(o.phases, PhaseRecord{Name: name, Dur: d, Ms: float64(d) / float64(time.Millisecond)})
		o.mu.Unlock()
	}
}

// AddRetry counts one push retry on the Op.
func (o *Op) AddRetry() {
	if o == nil {
		return
	}
	o.mu.Lock()
	o.retries++
	o.mu.Unlock()
}

// snapshot freezes the Op's current state into a Record.
func (o *Op) snapshot() Record {
	o.mu.Lock()
	defer o.mu.Unlock()
	d := time.Since(o.Start)
	rec := Record{
		ID: o.ID, Kind: o.Kind, Route: o.Route, Project: o.Project,
		Start: o.Start, Dur: d, Ms: float64(d) / float64(time.Millisecond),
		Retries: o.retries,
		Phases:  append([]PhaseRecord(nil), o.phases...),
	}
	return rec
}

// End finishes the Op: the record lands in the default ring, is observed by
// the default metrics (when wired), and is emitted as one structured log line.
// Phase-less successful plain requests stay out of the ring — it holds
// *operations* (transitions, lint, anything with phases or failures), so a
// burst of board polls cannot evict the records worth inspecting.
func (o *Op) End(status string) {
	if o == nil {
		return
	}
	rec := o.snapshot()
	rec.Status = status
	plainHTTP := rec.Kind == "http" && len(rec.Phases) == 0 && rec.Retries == 0 &&
		len(status) == 3 && status[0] == '2'
	if !plainHTTP {
		DefaultRing.add(rec)
	}
	observeOp(rec)
	logger.Info("op",
		slog.String("op_id", rec.ID),
		slog.String("kind", rec.Kind),
		slog.String("route", rec.Route),
		slog.String("project", rec.Project),
		slog.String("status", rec.Status),
		slog.Float64("ms", rec.Ms),
		slog.Int("retries", rec.Retries),
		slog.Any("phases", rec.Phases),
	)
}
