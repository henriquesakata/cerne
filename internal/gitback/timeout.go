package gitback

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"
)

// ErrNetworkTimeout is the sentinel for a network operation (push, fetch)
// aborted because the remote did not complete within netTimeout. It wraps
// context.DeadlineExceeded, so errors.Is matches either.
var ErrNetworkTimeout = errors.New("tempo limite de rede excedido")

// defaultNetTimeout leaves room for a large repo on a slow-but-alive network
// without ever leaving the operator hung for minutes.
const defaultNetTimeout = 60 * time.Second

// netTimeout bounds each gitback network operation. Overridable via the
// CERNE_NET_TIMEOUT environment variable (a Go duration, e.g. "90s"); unset,
// unparsable or non-positive values keep the default.
var netTimeout = netTimeoutFromEnv()

func netTimeoutFromEnv() time.Duration {
	v := os.Getenv("CERNE_NET_TIMEOUT")
	if v == "" {
		return defaultNetTimeout
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return defaultNetTimeout
	}
	return d
}

// runWithNetTimeout runs op under the package network deadline. The context
// alone is not enough: go-git transports do not always honor cancellation once
// a connection is established (the observed post-push SSH hang), so the
// deadline is also enforced from outside — when it expires before op returns,
// the op goroutine is abandoned (it dies with the short-lived CLI process) and
// the caller gets ErrNetworkTimeout immediately instead of hanging.
func runWithNetTimeout(name string, op func(ctx context.Context) error) error {
	ctx, cancel := context.WithTimeout(context.Background(), netTimeout)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- op(ctx) }()
	select {
	case err := <-done:
		if err != nil && ctx.Err() != nil {
			return timeoutErr(name, errors.Join(ctx.Err(), err))
		}
		return err
	case <-ctx.Done():
		return timeoutErr(name, ctx.Err())
	}
}

func timeoutErr(name string, cause error) error {
	return fmt.Errorf("%w: %s após %s: %w", ErrNetworkTimeout, name, netTimeout, cause)
}
