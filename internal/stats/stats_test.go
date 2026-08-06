package stats

import (
	"fmt"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
)

func mkEvent(id, typ string, at time.Time) domain.Event {
	return domain.Event{ID: "ev-" + id + typ, EntityID: id, Type: typ, At: at}
}

func day(n int) time.Time {
	return time.Date(2026, 1, n, 0, 0, 0, 0, time.UTC)
}

func TestCompute_LeadAndCycleForSingleFeature(t *testing.T) {
	events := []domain.Event{
		mkEvent("F-0001", "feature.created", day(1)),
		mkEvent("F-0001", "feature.started", day(3)),
		mkEvent("F-0001", "feature.completed", day(10)),
	}
	m, err := Compute(events, time.Time{})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if len(m.Features) != 1 {
		t.Fatalf("Features = %d, want 1", len(m.Features))
	}
	f := m.Features[0]
	if f.ID != "F-0001" {
		t.Fatalf("ID = %q", f.ID)
	}
	if f.Lead != 9*24*time.Hour {
		t.Fatalf("Lead = %v, want 216h", f.Lead)
	}
	if f.Cycle != 7*24*time.Hour {
		t.Fatalf("Cycle = %v, want 168h", f.Cycle)
	}
	if m.Excluded != 0 {
		t.Fatalf("Excluded = %d, want 0", m.Excluded)
	}
}

func TestCompute_MedianOddAndEven(t *testing.T) {
	// three features with lead 1,2,3 days -> median 2 days.
	events := []domain.Event{
		mkEvent("F-0001", "feature.created", day(1)), mkEvent("F-0001", "feature.started", day(1)), mkEvent("F-0001", "feature.completed", day(2)),
		mkEvent("F-0002", "feature.created", day(1)), mkEvent("F-0002", "feature.started", day(1)), mkEvent("F-0002", "feature.completed", day(3)),
		mkEvent("F-0003", "feature.created", day(1)), mkEvent("F-0003", "feature.started", day(1)), mkEvent("F-0003", "feature.completed", day(4)),
	}
	m, err := Compute(events, time.Time{})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if m.LeadMedian != 2*24*time.Hour {
		t.Fatalf("LeadMedian (odd N) = %v, want 48h", m.LeadMedian)
	}

	// add a 4th feature, lead 4 days -> even N median = avg(2,3) = 2.5 days.
	events = append(events,
		mkEvent("F-0004", "feature.created", day(1)), mkEvent("F-0004", "feature.started", day(1)), mkEvent("F-0004", "feature.completed", day(5)),
	)
	m, err = Compute(events, time.Time{})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if m.LeadMedian != 60*time.Hour {
		t.Fatalf("LeadMedian (even N) = %v, want 60h", m.LeadMedian)
	}
}

func TestCompute_P90(t *testing.T) {
	events := []domain.Event{}
	for i := 1; i <= 10; i++ {
		id := fmt.Sprintf("F-%04d", i)
		events = append(events,
			mkEvent(id, "feature.created", day(1)),
			mkEvent(id, "feature.started", day(1)),
			mkEvent(id, "feature.completed", day(1+i)),
		)
	}
	m, err := Compute(events, time.Time{})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if m.LeadP90 != 9*24*time.Hour {
		t.Fatalf("LeadP90 = %v, want 216h (nearest-rank over leads 1..10 days)", m.LeadP90)
	}
}

func TestCompute_SinceWindowFiltersByCompletion(t *testing.T) {
	events := []domain.Event{
		mkEvent("F-0001", "feature.created", day(1)), mkEvent("F-0001", "feature.started", day(1)), mkEvent("F-0001", "feature.completed", day(2)),
		mkEvent("F-0002", "feature.created", day(20)), mkEvent("F-0002", "feature.started", day(20)), mkEvent("F-0002", "feature.completed", day(25)),
	}
	m, err := Compute(events, day(10))
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if len(m.Features) != 1 || m.Features[0].ID != "F-0002" {
		t.Fatalf("Features = %+v, want only F-0002", m.Features)
	}
}

func TestCompute_MissingStartedExcludesFromLeadCycleButCountsThroughput(t *testing.T) {
	events := []domain.Event{
		// bulk-imported feature: only created+completed, no started.
		mkEvent("F-0001", "feature.created", day(1)),
		mkEvent("F-0001", "feature.completed", day(2)),
	}
	m, err := Compute(events, time.Time{})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if len(m.Features) != 0 {
		t.Fatalf("Features = %+v, want none (excluded)", m.Features)
	}
	if m.Excluded != 1 {
		t.Fatalf("Excluded = %d, want 1", m.Excluded)
	}
	if got := m.ThroughputByMonth["2026-01"]; got != 1 {
		t.Fatalf("ThroughputByMonth[2026-01] = %d, want 1", got)
	}
}

func TestCompute_NotYetCompletedFeatureIgnored(t *testing.T) {
	events := []domain.Event{
		mkEvent("F-0001", "feature.created", day(1)),
		mkEvent("F-0001", "feature.started", day(2)),
	}
	m, err := Compute(events, time.Time{})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if len(m.Features) != 0 || m.Excluded != 0 {
		t.Fatalf("m = %+v, want empty and not excluded (feature simply not done yet)", m)
	}
}

func TestCompute_ThroughputByMonth(t *testing.T) {
	events := []domain.Event{
		mkEvent("F-0001", "feature.created", day(1)), mkEvent("F-0001", "feature.started", day(1)), mkEvent("F-0001", "feature.completed", day(5)),
		mkEvent("F-0002", "feature.created", day(1)), mkEvent("F-0002", "feature.started", day(1)),
		mkEvent("F-0002", "feature.completed", time.Date(2026, 2, 3, 0, 0, 0, 0, time.UTC)),
	}
	m, err := Compute(events, time.Time{})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if m.ThroughputByMonth["2026-01"] != 1 || m.ThroughputByMonth["2026-02"] != 1 {
		t.Fatalf("ThroughputByMonth = %+v", m.ThroughputByMonth)
	}
}

func TestCompute_NoWrites(t *testing.T) {
	// Compute takes only in-memory events and returns a value — no I/O
	// side channel exists in its signature, so this test just documents
	// the invariant (spec criterion 6) via the API shape.
	events := []domain.Event{mkEvent("F-0001", "feature.created", day(1))}
	if _, err := Compute(events, time.Time{}); err != nil {
		t.Fatalf("Compute: %v", err)
	}
}
