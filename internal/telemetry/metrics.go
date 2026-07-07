package telemetry

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics holds cerne's Prometheus instruments. Label cardinality is bounded
// by design (F-0149 criterion 7): routes are the templated patterns from the
// route table, project is the studio app slug — never entity ids or actors
// (those live only in logs/traces).
type Metrics struct {
	reg         *prometheus.Registry
	HTTPDur     *prometheus.HistogramVec
	PhaseDur    *prometheus.HistogramVec
	PushRetries prometheus.Counter
	LintDur     *prometheus.HistogramVec
	CompileDur  *prometheus.HistogramVec
	SSEClients  prometheus.Gauge
}

// Default is the process-wide metrics set, served by GET /metrics.
var Default = NewMetrics(prometheus.NewRegistry())

// NewMetrics registers cerne's instruments plus the Go runtime collector
// (go_memstats_* is what makes heap pathologies visible) on reg.
func NewMetrics(reg *prometheus.Registry) *Metrics {
	m := &Metrics{
		reg: reg,
		HTTPDur: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "cerne_http_request_duration_seconds",
			Help:    "HTTP request duration by templated route.",
			Buckets: prometheus.DefBuckets,
		}, []string{"route", "method", "code", "project"}),
		PhaseDur: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "cerne_op_phase_duration_seconds",
			Help:    "Duration of each phase (lock_wait, fetch, merge, commit, push, snapshot) per operation kind.",
			Buckets: prometheus.DefBuckets,
		}, []string{"op", "phase"}),
		PushRetries: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "cerne_push_retries_total",
			Help: "Non-fast-forward push retries in the reconcile loop.",
		}),
		LintDur: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "cerne_lint_duration_seconds",
			Help:    "Full lint pass duration.",
			Buckets: prometheus.DefBuckets,
		}, []string{"project"}),
		CompileDur: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "cerne_compile_duration_seconds",
			Help:    "Wiki compile duration.",
			Buckets: []float64{1, 5, 15, 30, 60, 120, 300},
		}, []string{"project"}),
		SSEClients: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "cerne_sse_clients",
			Help: "Connected SSE stream clients.",
		}),
	}
	reg.MustRegister(m.HTTPDur, m.PhaseDur, m.PushRetries, m.LintDur, m.CompileDur, m.SSEClients)
	reg.MustRegister(collectors.NewGoCollector())
	return m
}

// ObserveHTTP records one served request. route is the templated pattern.
func (m *Metrics) ObserveHTTP(route, method, code, project string, d time.Duration) {
	m.HTTPDur.WithLabelValues(route, method, code, project).Observe(d.Seconds())
}

// ObserveLint records one full lint pass.
func (m *Metrics) ObserveLint(project string, d time.Duration) {
	m.LintDur.WithLabelValues(project).Observe(d.Seconds())
}

// ObserveCompile records one wiki compile.
func (m *Metrics) ObserveCompile(project string, d time.Duration) {
	m.CompileDur.WithLabelValues(project).Observe(d.Seconds())
}

// observe feeds a finished op record into the instruments.
func (m *Metrics) observe(rec Record) {
	for _, p := range rec.Phases {
		m.PhaseDur.WithLabelValues(rec.Kind, p.Name).Observe(p.Dur.Seconds())
	}
	if rec.Retries > 0 {
		m.PushRetries.Add(float64(rec.Retries))
	}
}

// observeOp routes a finished record to the default metrics.
func observeOp(rec Record) { Default.observe(rec) }

// MetricsHandler serves the default registry (GET /metrics).
func MetricsHandler() http.Handler {
	return promhttp.HandlerFor(Default.reg, promhttp.HandlerOpts{})
}
