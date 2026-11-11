// Package stats computes flow metrics (lead time, cycle time, throughput) from
// the event log, and process metrics (cost per delivered feature, quality guard
// and change-marker verdicts, F-0321) from collected samples — purely, no I/O,
// no writes.
package stats

import (
	"sort"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
)

const (
	eventCreated   = "feature.created"
	eventStarted   = "feature.started"
	eventCompleted = "feature.completed"
)

// FeatureMetric is the lead/cycle time for one completed feature.
type FeatureMetric struct {
	ID    string
	Lead  time.Duration // created -> completed
	Cycle time.Duration // started -> completed
}

// Metrics is the aggregate flow-metrics result of Compute.
type Metrics struct {
	Features          []FeatureMetric
	LeadMedian        time.Duration
	LeadP90           time.Duration
	CycleMedian       time.Duration
	CycleP90          time.Duration
	ThroughputByMonth map[string]int // "YYYY-MM" -> features completed that month
	Excluded          int            // completed features missing created/started events
}

// Compute aggregates flow metrics from events. since restricts the window to
// features completed at or after since (zero value = no restriction). A
// completed feature missing its created or started event is excluded from
// lead/cycle (and counted in Excluded) but still counts toward throughput,
// since its completion timestamp is known regardless.
func Compute(events []domain.Event, since time.Time) (Metrics, error) {
	type times struct {
		created, started, completed          time.Time
		hasCreated, hasStarted, hasCompleted bool
	}
	byFeature := map[string]*times{}
	for _, ev := range events {
		t, ok := byFeature[ev.EntityID]
		if !ok {
			t = &times{}
			byFeature[ev.EntityID] = t
		}
		switch ev.Type {
		case eventCreated:
			if !t.hasCreated || ev.At.Before(t.created) {
				t.created, t.hasCreated = ev.At, true
			}
		case eventStarted:
			if !t.hasStarted || ev.At.Before(t.started) {
				t.started, t.hasStarted = ev.At, true
			}
		case eventCompleted:
			if !t.hasCompleted || ev.At.Before(t.completed) {
				t.completed, t.hasCompleted = ev.At, true
			}
		}
	}

	m := Metrics{ThroughputByMonth: map[string]int{}}
	var leads, cycles []time.Duration
	ids := make([]string, 0, len(byFeature))
	for id := range byFeature {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		t := byFeature[id]
		if !t.hasCompleted || t.completed.Before(since) {
			continue
		}
		m.ThroughputByMonth[t.completed.UTC().Format("2006-01")]++
		if !t.hasCreated || !t.hasStarted {
			m.Excluded++
			continue
		}
		lead := t.completed.Sub(t.created)
		cycle := t.completed.Sub(t.started)
		m.Features = append(m.Features, FeatureMetric{ID: id, Lead: lead, Cycle: cycle})
		leads = append(leads, lead)
		cycles = append(cycles, cycle)
	}

	m.LeadMedian, m.LeadP90 = medianAndP90(leads)
	m.CycleMedian, m.CycleP90 = medianAndP90(cycles)
	return m, nil
}

// medianAndP90 computes the median and 90th percentile (nearest-rank) of
// durations. Empty input returns zero values.
func medianAndP90(d []time.Duration) (median, p90 time.Duration) {
	if len(d) == 0 {
		return 0, 0
	}
	sorted := append([]time.Duration(nil), d...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	n := len(sorted)
	if n%2 == 1 {
		median = sorted[n/2]
	} else {
		median = (sorted[n/2-1] + sorted[n/2]) / 2
	}

	idx := int(0.9*float64(n)+0.999999) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= n {
		idx = n - 1
	}
	p90 = sorted[idx]
	return median, p90
}
