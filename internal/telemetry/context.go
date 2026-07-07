package telemetry

import "context"

type ctxKey struct{}

// NewContext returns ctx carrying op, so an HTTP middleware's Op can collect
// phases from the app layer it fans into.
func NewContext(ctx context.Context, op *Op) context.Context {
	return context.WithValue(ctx, ctxKey{}, op)
}

// OpFromContext returns the Op carried by ctx, or nil (all Op methods are
// nil-safe, so callers never guard).
func OpFromContext(ctx context.Context) *Op {
	op, _ := ctx.Value(ctxKey{}).(*Op)
	return op
}
