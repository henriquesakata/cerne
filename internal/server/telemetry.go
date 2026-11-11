package server

import (
	"net/http"
	"net/http/pprof"
	"os"
	"strconv"
	"time"

	"github.com/henriquesakata/cerne/internal/telemetry"
)

// statusWriter captures the response code for the request log and metric.
type statusWriter struct {
	http.ResponseWriter
	code int
}

func (w *statusWriter) WriteHeader(code int) {
	w.code = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// instrumented wraps a data-route handler with the telemetry middleware
// (F-0149): one Op per request — its id correlates the request log line with
// the app-layer phases collected through the context — plus the HTTP duration
// histogram. The metric/log route label is the TEMPLATED pattern from the
// route table, never the raw path (bounded cardinality).
func instrumented(rt dataRoute) scopedHandler {
	return func(w http.ResponseWriter, r *http.Request, repoDir, projDir, projectID string) {
		op := telemetry.StartOp("http")
		op.Route = rt.pattern
		op.Project = projectID
		sw := &statusWriter{ResponseWriter: w, code: http.StatusOK}
		rt.handler(sw, r.WithContext(telemetry.NewContext(r.Context(), op)), repoDir, projDir, projectID)
		code := strconv.Itoa(sw.code)
		telemetry.Default.ObserveHTTP(rt.pattern, rt.method, code, projectID, time.Since(op.Start))
		op.End(code)
	}
}

// mountTelemetry registers the server-wide observability endpoints: the
// Prometheus registry and the recent-operations ring. Shared by the three
// servers (single-project, local ui, remote serve).
func mountTelemetry(mux *http.ServeMux) {
	mux.Handle("GET /metrics", telemetry.MetricsHandler())
	mux.HandleFunc("GET /api/debug/timings", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, telemetry.Snapshot())
	})
}

// mountPprof exposes net/http/pprof only when CERNE_PPROF=1 (F-0149
// criterion 6). Mounted solely on the loopback-bound servers (`cerne ui`
// binds 127.0.0.1 by design); the remote `cerne serve` never gets it.
func mountPprof(mux *http.ServeMux) {
	if os.Getenv("CERNE_PPROF") != "1" {
		return
	}
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
}
