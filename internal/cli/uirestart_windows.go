//go:build windows

package cli

import (
	"net"
	"os"
	"os/exec"
	"os/signal"
)

// dupListener: Windows has no exec and Go cannot hand it a listener
// (net.FileListener is not supported there), so the port is free for a
// moment; F-0478 criterion 7 brings the panel back to the binary on disk if
// another process takes it meanwhile.
func dupListener(net.Listener) (*os.File, error) { return nil, nil }

// restartUI starts the binary on disk in this console and waits for it,
// leaving with its exit code. The console's Ctrl+C reaches the child too, so
// this process ignores it and lets the child decide.
//
// ponytail: each restart leaves one waiting parent (about one per release);
// a single supervisor process if that ever weighs.
func restartUI(rr *restartRequest) error {
	c := exec.Command(rr.path, os.Args[1:]...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	c.Env = append(restartEnv(), restartedEnv+"=1")
	if err := c.Start(); err != nil {
		return err
	}
	signal.Ignore(os.Interrupt)
	_ = c.Wait()
	os.Exit(c.ProcessState.ExitCode())
	return nil
}
