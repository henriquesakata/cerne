package cli

import (
	"fmt"
	"io"
	"time"
)

// progressInterval is how often a running phase says it is still alive. A var
// so the test can shrink it.
var progressInterval = 10 * time.Second

// startPhase announces a phase on w, beats every progressInterval with the
// elapsed time while it runs, and returns the func that closes it with its
// total (F-0876): "done" when the phase succeeded, "failed" when stop(false)
// says it did not, so a phase that broke never claims success. It is for phases that can be silent for minutes — backend
// lock wait, network, a slow step — so a long push is not mistaken for a hung
// one. The returned stop waits for the heartbeat goroutine to exit, so nothing
// is written to w afterwards (w need not be goroutine-safe).
func startPhase(w io.Writer, name string) (stop func(ok bool)) {
	start := time.Now()
	fmt.Fprintf(w, "cerne: %s...\n", name)
	quit, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		t := time.NewTicker(progressInterval)
		defer t.Stop()
		for {
			select {
			case <-quit:
				return
			case <-t.C:
				fmt.Fprintf(w, "cerne: %s still running (%s)\n", name, time.Since(start).Round(time.Second))
			}
		}
	}()
	return func(ok bool) {
		close(quit)
		<-done
		outcome := "done"
		if !ok {
			outcome = "failed"
		}
		fmt.Fprintf(w, "cerne: %s: %s (%s)\n", name, outcome, time.Since(start).Round(100*time.Millisecond))
	}
}
