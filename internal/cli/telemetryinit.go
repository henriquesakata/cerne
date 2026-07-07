package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"gitlab.com/rockt/cerne/internal/project"
	"gitlab.com/rockt/cerne/internal/telemetry"
)

// initServerTelemetry configures the long-running servers' telemetry (F-0149):
// JSON logs to stdout (the platform's log collector scrapes container stdout)
// and the opt-in OTLP trace exporter — OTEL_EXPORTER_OTLP_ENDPOINT wins over
// the project's [telemetry] otlp_endpoint; with neither, tracing stays off.
// Returns the shutdown that flushes pending spans.
func initServerTelemetry(ctx context.Context, dir string) func() {
	level := slog.LevelInfo
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	service := "cerne"
	if dir == "" {
		dir, _ = os.Getwd()
	}
	if cfg, _, err := project.LoadConfig(dir); err == nil {
		switch cfg.Telemetry.LogLevel {
		case "debug":
			level = slog.LevelDebug
		case "warn":
			level = slog.LevelWarn
		case "error":
			level = slog.LevelError
		}
		if endpoint == "" {
			endpoint = cfg.Telemetry.OtlpEndpoint
		}
		if cfg.Telemetry.ServiceName != "" {
			service = cfg.Telemetry.ServiceName
		}
	}
	telemetry.SetupLogger(os.Stdout, true, level)
	shutdown, err := telemetry.InitTracer(ctx, endpoint, service)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: trace exporter not started: %v\n", err)
	}
	return func() {
		if shutdown != nil {
			_ = shutdown(context.Background())
		}
	}
}
