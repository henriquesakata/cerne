package cli

import (
	"errors"
	"fmt"
)

// exitError carries a process exit code other than 1 (F-0460: feature wait's
// unreachable, timeout and signal codes). Only cerne's own commands return it,
// so an error from a child process (exec.ExitError) never leaks its code.
type exitError struct{ code int }

func (e exitError) Error() string { return fmt.Sprintf("exit status %d", e.code) }

// ExitCode is the process exit code for err: its exitError code, else 1.
func ExitCode(err error) int {
	var e exitError
	if errors.As(err, &e) {
		return e.code
	}
	return 1
}
