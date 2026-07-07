package telemetry

import (
	"context"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// TestTracingOffByDefault pins criterion 8 (F-0149): with no endpoint
// configured, InitTracer is a no-op — nothing is exported, no network.
func TestTracingOffByDefault(t *testing.T) {
	shutdown, err := InitTracer(context.Background(), "", "cerne-test")
	if err != nil {
		t.Fatalf("InitTracer(\"\") must not fail: %v", err)
	}
	if shutdown != nil {
		t.Fatal("InitTracer(\"\") must not build a provider")
	}
	if tracingActive.Load() {
		t.Fatal("tracing must stay inactive without an endpoint")
	}
}

// TestSpansCarryOpPhases pins criterion 4: with tracing enabled, a finished
// op exports one root span plus a child span per phase, retries as attribute.
func TestSpansCarryOpPhases(t *testing.T) {
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	enableTracing(tp)
	defer func() {
		tracingActive.Store(false)
		_ = tp.Shutdown(context.Background())
	}()

	op := StartOp("transition")
	op.Phase("fetch")()
	op.Phase("push")()
	op.AddRetry()
	op.End("ok")

	spans := exp.GetSpans()
	if len(spans) != 3 {
		t.Fatalf("spans = %d, want 3 (root + 2 phases)", len(spans))
	}
	names := map[string]bool{}
	var rootTrace, phaseTrace string
	for _, s := range spans {
		names[s.Name] = true
		if s.Name == "transition" {
			rootTrace = s.SpanContext.TraceID().String()
		}
		if s.Name == "push" {
			phaseTrace = s.SpanContext.TraceID().String()
		}
	}
	if !names["transition"] || !names["fetch"] || !names["push"] {
		t.Fatalf("span names = %v, want transition/fetch/push", names)
	}
	if rootTrace == "" || rootTrace != phaseTrace {
		t.Fatalf("phase span not in the root's trace (%s vs %s)", phaseTrace, rootTrace)
	}
}
