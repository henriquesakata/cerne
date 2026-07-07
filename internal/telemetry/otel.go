package telemetry

import (
	"context"
	"sync/atomic"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

// tracingActive gates span emission: false (the default) means End() never
// touches the OTel API, so an unconfigured binary does zero tracing work.
var tracingActive atomic.Bool

// InitTracer wires the OTLP/HTTP trace exporter when endpoint is non-empty
// (from .cernerc [telemetry] otlp_endpoint or OTEL_EXPORTER_OTLP_ENDPOINT).
// An empty endpoint returns (nil, nil) — tracing stays off, no provider, no
// sockets: telemetry never leaves the host without explicit configuration
// (F-0149 criterion 8). The returned shutdown flushes pending spans.
func InitTracer(ctx context.Context, endpoint, serviceName string) (func(context.Context) error, error) {
	if endpoint == "" {
		return nil, nil
	}
	exp, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(endpoint))
	if err != nil {
		return nil, err
	}
	res, err := resource.Merge(resource.Default(),
		resource.NewWithAttributes(semconv.SchemaURL, semconv.ServiceName(serviceName)))
	if err != nil {
		return nil, err
	}
	tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exp), sdktrace.WithResource(res))
	enableTracing(tp)
	return func(ctx context.Context) error {
		tracingActive.Store(false)
		return tp.Shutdown(ctx)
	}, nil
}

// enableTracing installs tp as the global provider and opens the span gate.
// Split from InitTracer so tests can inject an in-memory exporter.
func enableTracing(tp *sdktrace.TracerProvider) {
	otel.SetTracerProvider(tp)
	tracingActive.Store(true)
}

// emitSpans reconstructs a finished op as one root span with a child span per
// phase, using the recorded timestamps. Emitting at End keeps the hot path
// free of tracing calls and needs no context threading through the app layer.
func emitSpans(rec Record) {
	if !tracingActive.Load() {
		return
	}
	tr := otel.Tracer("cerne")
	ctx, root := tr.Start(context.Background(), rec.Kind,
		trace.WithTimestamp(rec.Start),
		trace.WithAttributes(
			attribute.String("cerne.op_id", rec.ID),
			attribute.String("cerne.route", rec.Route),
			attribute.String("cerne.project", rec.Project),
			attribute.String("cerne.status", rec.Status),
			attribute.Int("cerne.push_retries", rec.Retries),
		))
	for _, p := range rec.Phases {
		_, span := tr.Start(ctx, p.Name, trace.WithTimestamp(p.Start))
		span.End(trace.WithTimestamp(p.Start.Add(p.Dur)))
	}
	root.End(trace.WithTimestamp(rec.Start.Add(rec.Dur)))
}
