package telemetry

import (
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestMetricsObserveHTTP(t *testing.T) {
	m := NewMetrics(prometheus.NewRegistry())
	m.ObserveHTTP("/features/{id}/transition", "POST", "200", "cerne", 42*time.Millisecond)
	if got := testutil.CollectAndCount(m.HTTPDur); got != 1 {
		t.Fatalf("http histogram series = %d, want 1", got)
	}
}

func TestObserveOpRecordsPhasesAndRetries(t *testing.T) {
	m := NewMetrics(prometheus.NewRegistry())
	m.observe(Record{
		Kind:    "transition",
		Retries: 2,
		Phases: []PhaseRecord{
			{Name: "fetch", Dur: time.Millisecond},
			{Name: "push", Dur: 2 * time.Millisecond},
		},
	})
	if got := testutil.CollectAndCount(m.PhaseDur); got != 2 {
		t.Fatalf("phase histogram series = %d, want 2", got)
	}
	if got := testutil.ToFloat64(m.PushRetries); got != 2 {
		t.Fatalf("push retries = %v, want 2", got)
	}
}

func TestLintCompileAndSSEInstruments(t *testing.T) {
	m := NewMetrics(prometheus.NewRegistry())
	m.ObserveLint("cerne", 3*time.Second)
	m.ObserveCompile("cerne", time.Second)
	m.SSEClients.Inc()
	if got := testutil.ToFloat64(m.SSEClients); got != 1 {
		t.Fatalf("sse gauge = %v, want 1", got)
	}
	if got := testutil.CollectAndCount(m.LintDur); got != 1 {
		t.Fatalf("lint histogram series = %d, want 1", got)
	}
	if got := testutil.CollectAndCount(m.CompileDur); got != 1 {
		t.Fatalf("compile histogram series = %d, want 1", got)
	}
}

func TestGoRuntimeCollectorRegistered(t *testing.T) {
	reg := prometheus.NewRegistry()
	NewMetrics(reg)
	families, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range families {
		if strings.HasPrefix(f.GetName(), "go_") {
			return
		}
	}
	t.Fatal("no go_* runtime metrics registered")
}

func TestOpEndFeedsDefaultMetrics(t *testing.T) {
	op := StartOp("transition")
	op.Phase("push")()
	op.End("ok")
	if got := testutil.CollectAndCount(Default.PhaseDur); got == 0 {
		t.Fatal("default metrics did not observe the finished op")
	}
}
