package server

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"gitlab.com/rockt/cerne/internal/telemetry"
)

// TestInstrumentedRouteUsesTemplatedLabel pins criterion 7 (F-0149): the log
// line and the metric label carry the route PATTERN, never the raw path with
// entity ids in it.
func TestInstrumentedRouteUsesTemplatedLabel(t *testing.T) {
	repoDir, projDir := newBackend(t)
	var buf bytes.Buffer
	prev := telemetry.SetLogger(slog.New(slog.NewJSONHandler(&buf, nil)))
	defer telemetry.SetLogger(prev)

	h := New(repoDir, projDir, "demo")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/features/F-9999", nil))

	if strings.Contains(buf.String(), "F-9999") {
		t.Fatalf("log line leaks the raw path:\n%s", buf.String())
	}
	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatalf("no JSON log line for the request: %v\n%s", err, buf.String())
	}
	if line["route"] != "/features/{id}" {
		t.Fatalf("route = %v, want /features/{id}", line["route"])
	}
	if line["project"] != "demo" {
		t.Fatalf("project = %v, want demo", line["project"])
	}
	if got := testutil.CollectAndCount(telemetry.Default.HTTPDur); got == 0 {
		t.Fatal("http duration histogram not observed")
	}
}

// TestMetricsAndTimingsEndpoints pins criteria 3 and 5: /metrics serves the
// Prometheus registry (with go_* runtime metrics) and /api/debug/timings the
// ring buffer, on the single-project mux.
func TestMetricsAndTimingsEndpoints(t *testing.T) {
	repoDir, projDir := newBackend(t)
	h := New(repoDir, projDir, "demo")

	op := telemetry.StartOp("lint")
	op.Phase("scan")()
	op.End("ok")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "go_goroutines") {
		t.Fatalf("/metrics: code=%d, want 200 with go_* metrics", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/debug/timings", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/api/debug/timings: code=%d, want 200", rec.Code)
	}
	var records []telemetry.Record
	if err := json.Unmarshal(rec.Body.Bytes(), &records); err != nil {
		t.Fatalf("timings body is not a record array: %v", err)
	}
	found := false
	for _, r := range records {
		if r.ID == op.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("timings does not include finished op %s", op.ID)
	}
}

// TestTransitionRecordsPhasesWithRequestOp pins the HTTP↔phases correlation
// (criteria 1+2, F-0149): the transition's phases land on the REQUEST's op —
// same op_id in the ring record and the request log line — including the push.
func TestTransitionRecordsPhasesWithRequestOp(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	writeDoc(t, repoDir, "F-0001", "spec.md", realDoc("F-0001", "Problema real descrito aqui."))
	commitBackend(t, repoDir)
	var buf bytes.Buffer
	prev := telemetry.SetLogger(slog.New(slog.NewJSONHandler(&buf, nil)))
	defer telemetry.SetLogger(prev)

	var got map[string]string
	rec := doJSON(t, New(repoDir, projDir, "demo"), http.MethodPost, "/api/features/F-0001/transition",
		`{"action":"approve"}`, &got)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}

	// Identify OUR record by route — the newest ring entry can belong to an
	// async autocommit worker from a neighbouring test on a loaded machine.
	var newest telemetry.Record
	for _, r := range telemetry.Snapshot() {
		if r.Route == "/features/{id}/transition" {
			newest = r
			break
		}
	}
	if newest.ID == "" {
		t.Fatalf("no ring record with route /features/{id}/transition; ring: %+v", telemetry.Snapshot())
	}
	var sawPush, sawLock bool
	for _, p := range newest.Phases {
		if p.Name == "push" {
			sawPush = true
		}
		if p.Name == "lock_wait" {
			sawLock = true
		}
	}
	if !sawPush || !sawLock {
		t.Fatalf("phases missing push/lock_wait: %+v", newest.Phases)
	}
	if !strings.Contains(buf.String(), newest.ID) {
		t.Fatalf("request log does not carry the ring op_id %s:\n%s", newest.ID, buf.String())
	}
}

// TestLintObservedPerProject pins criterion 3's lint histogram and the lint
// phase: a lint request is an operation, so it reaches the ring.
func TestLintObservedPerProject(t *testing.T) {
	repoDir, projDir := newBackend(t)
	// A unique project label: CollectAndCount counts SERIES, so reusing a
	// label another test already touched would read as "not observed".
	const proj = "demo-lint-histogram"
	before := testutil.CollectAndCount(telemetry.Default.LintDur)

	rec := httptest.NewRecorder()
	New(repoDir, projDir, proj).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/lint", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("lint status = %d", rec.Code)
	}
	if got := testutil.CollectAndCount(telemetry.Default.LintDur); got <= before {
		t.Fatalf("lint histogram not observed (%d -> %d)", before, got)
	}
	newest := telemetry.Snapshot()[0]
	if newest.Route != "/lint" || len(newest.Phases) == 0 || newest.Phases[0].Name != "lint" {
		t.Fatalf("ring newest = %+v, want /lint with a lint phase", newest)
	}
}

// TestPprofGatedByEnv pins criterion 6 (F-0149): /debug/pprof/ exists only
// when CERNE_PPROF=1 was set at mux build time.
func TestPprofGatedByEnv(t *testing.T) {
	repoDir, projDir := newBackend(t)

	// Ungated, /debug/pprof/ falls through to the SPA (any unknown path serves
	// index.html) — the profiler page itself must not exist.
	rec := httptest.NewRecorder()
	New(repoDir, projDir, "demo").ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil))
	if strings.Contains(rec.Body.String(), "Types of profiles available") {
		t.Fatal("pprof page served without CERNE_PPROF")
	}

	t.Setenv("CERNE_PPROF", "1")
	rec = httptest.NewRecorder()
	New(repoDir, projDir, "demo").ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Types of profiles available") {
		t.Fatalf("pprof gated on: code=%d, want the profiler index page", rec.Code)
	}
}

// TestSSEGaugeTracksStreamClients pins the SSE client gauge: one connected
// stream raises it, disconnect lowers it.
func TestSSEGaugeTracksStreamClients(t *testing.T) {
	hub := newMultiHub(make(chan struct{}))
	before := testutil.ToFloat64(telemetry.Default.SSEClients)

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/stream", nil).WithContext(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		hub.handleStream(httptest.NewRecorder(), req)
	}()

	deadline := time.After(2 * time.Second)
	for testutil.ToFloat64(telemetry.Default.SSEClients) != before+1 {
		select {
		case <-deadline:
			t.Fatal("gauge never incremented for a connected stream")
		case <-time.After(5 * time.Millisecond):
		}
	}
	cancel()
	<-done
	if got := testutil.ToFloat64(telemetry.Default.SSEClients); got != before {
		t.Fatalf("gauge after disconnect = %v, want %v", got, before)
	}
}
