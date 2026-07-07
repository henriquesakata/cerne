package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"
)

// stdinReadyWindow is how long a piped stdin gets to deliver its FIRST byte.
// A harness/CI that runs commands with an open, silent stdin pipe (no EOF —
// the F-0157 incident: 41 minutes blocked in a read syscall) reads as "no
// body" after this window; once one byte arrives, the rest is read to EOF
// with no deadline, so a slow producer never truncates.
const stdinReadyWindow = 500 * time.Millisecond

// stdinBody is the file the optional-body readers consume — a package var so
// tests can point it at a real pipe end (SetReadDeadline needs an *os.File).
var stdinBody = os.Stdin

// readOptionalBody reads an entity body from --file when given, or from a
// piped stdin. An empty body is valid: the entity is born a stub. Guards, in
// order: an interactive terminal never blocks (chardevice check); an injected
// cobra In (tests, programmatic use) is read directly; the real piped stdin
// must produce its first byte within stdinReadyWindow or it reads as empty
// (F-0157). A platform whose pipes reject read deadlines falls back to the
// plain blocking read — never a regression in what can be read. Shared by the
// optional-body entities: feature, decision, note, runbook and errata.
func readOptionalBody(cmd *cobra.Command, file string) (string, error) {
	if file != "" {
		data, err := os.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("--file: %w", err)
		}
		return string(data), nil
	}
	if in := cmd.InOrStdin(); in != io.Reader(os.Stdin) {
		data, err := io.ReadAll(in)
		if err != nil {
			return "", err
		}
		return string(data), nil
	}
	f := stdinBody
	info, err := f.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice != 0 {
		return "", nil // interactive terminal (or unknowable): no body
	}
	if err := f.SetReadDeadline(time.Now().Add(stdinReadyWindow)); err != nil {
		// Fail-open: no deadline support on this descriptor/platform keeps
		// today's behavior rather than inventing a lesser read path.
		data, rerr := io.ReadAll(f)
		if rerr != nil {
			return "", rerr
		}
		return string(data), nil
	}
	first := make([]byte, 1)
	n, rerr := f.Read(first)
	_ = f.SetReadDeadline(time.Time{})
	if rerr != nil {
		if errors.Is(rerr, os.ErrDeadlineExceeded) && n == 0 {
			return "", nil // open pipe, nothing coming: no body (the incident)
		}
		if errors.Is(rerr, io.EOF) {
			return string(first[:n]), nil // closed empty pipe: valid empty body
		}
		return "", rerr
	}
	rest, rerr := io.ReadAll(f)
	if rerr != nil {
		return "", rerr
	}
	return string(first[:n]) + string(rest), nil
}
