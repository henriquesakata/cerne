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
// tests can point it at a real pipe end (Stat's chardevice check needs an
// *os.File).
var stdinBody = os.Stdin

// readOptionalBody reads an entity body from --file when given, or from a
// piped stdin. An empty body is valid: the entity is born a stub. Guards, in
// order: an interactive terminal never blocks (chardevice check); an injected
// cobra In (tests, programmatic use) is read directly; the real piped stdin
// must produce its first byte within stdinReadyWindow or it reads as empty
// (F-0157). The window is enforced by a probe goroutine, not read deadlines:
// Windows anonymous pipes reject SetReadDeadline, and the former fail-open
// fallback re-opened the very blocking read the window exists to prevent
// (F-0168). Shared by the optional-body entities: feature, decision, note,
// runbook and errata.
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
	// Two-stage probe: the goroutine reads the first byte (blocking), then the
	// remainder. Data crosses only through the buffered channels, so there is
	// no shared state to race on. The window applies to the FIRST byte alone —
	// once it lands, the rest is read to EOF with no deadline, so a slow
	// producer never truncates.
	type stage struct {
		data []byte
		err  error
	}
	firstCh := make(chan stage, 1)
	restCh := make(chan stage, 1)
	go func() {
		var first [1]byte
		n, err := f.Read(first[:])
		firstCh <- stage{data: append([]byte(nil), first[:n]...), err: err}
		if err != nil {
			return
		}
		rest, rerr := io.ReadAll(f)
		restCh <- stage{data: rest, err: rerr}
	}()
	select {
	case fb := <-firstCh:
		if fb.err != nil {
			if errors.Is(fb.err, io.EOF) {
				return string(fb.data), nil // closed (empty) pipe: valid body as-is
			}
			return "", fb.err
		}
		rest := <-restCh
		if rest.err != nil {
			return "", rest.err
		}
		return string(fb.data) + string(rest.data), nil
	case <-time.After(stdinReadyWindow):
		// Open pipe, nothing coming: no body (the F-0157 incident). The probe
		// goroutine stays parked in Read until the process exits — a conscious
		// leak, harmless in a short-lived CLI and cheaper than per-platform
		// peek syscalls.
		return "", nil
	}
}
