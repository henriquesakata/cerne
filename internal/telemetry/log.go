package telemetry

import (
	"io"
	"log/slog"
	"sync/atomic"
)

// logger is the destination of per-op log lines. It starts as a discard logger
// so library use (CLI one-shots, tests) emits nothing unless configured.
var loggerV atomic.Pointer[slog.Logger]

func init() {
	loggerV.Store(slog.New(slog.NewTextHandler(io.Discard, nil)))
}

// logger returns the current op logger.
var logger = loggerHandle{}

type loggerHandle struct{}

func (loggerHandle) Info(msg string, args ...any) { loggerV.Load().Info(msg, args...) }
func (loggerHandle) Warn(msg string, args ...any) { loggerV.Load().Warn(msg, args...) }

// Info and Warn emit one structured line on the process logger (F-0149) for
// events outside an Op — e.g. the model catalog falling back to its baseline.
func Info(msg string, args ...any) { logger.Info(msg, args...) }
func Warn(msg string, args ...any) { logger.Warn(msg, args...) }

// SetLogger replaces the op logger (used by servers and tests).
func SetLogger(l *slog.Logger) *slog.Logger {
	prev := loggerV.Load()
	if l != nil {
		loggerV.Store(l)
	}
	return prev
}

// SetupLogger builds and installs the process logger: JSON for servers (stdout,
// scraped by the platform's log collector), text for CLI runs (stderr, so it
// never pollutes command output). Returns it so callers can also slog.SetDefault.
func SetupLogger(w io.Writer, json bool, level slog.Level) *slog.Logger {
	opts := &slog.HandlerOptions{Level: level}
	var h slog.Handler
	if json {
		h = slog.NewJSONHandler(w, opts)
	} else {
		h = slog.NewTextHandler(w, opts)
	}
	l := slog.New(h)
	SetLogger(l)
	return l
}
