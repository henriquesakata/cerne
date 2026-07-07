package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

// swapStdinBody points the optional-body reader at f; restore deferred.
func swapStdinBody(t *testing.T, f *os.File) {
	t.Helper()
	prev := stdinBody
	stdinBody = f
	t.Cleanup(func() { stdinBody = prev })
}

func plainCmd() *cobra.Command { return &cobra.Command{} }

// TestOpenPipeWithoutDataDoesNotBlock is the F-0157 incident regression: an
// open pipe that never delivers a byte (agent harness, CI) must read as "no
// body" within the readiness window — not block for 41 minutes.
func TestOpenPipeWithoutDataDoesNotBlock(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close() // never written to, never closed before the read
	swapStdinBody(t, r)

	start := time.Now()
	body, err := readOptionalBody(plainCmd(), "")
	if err != nil {
		t.Fatalf("readOptionalBody: %v", err)
	}
	if body != "" {
		t.Fatalf("body = %q, want empty for a silent pipe", body)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("took %v, must return within the readiness window", elapsed)
	}
}

// TestSlowProducerBodyStaysWhole pins criterion 2: once the first byte lands
// inside the window, the whole body is read to EOF with no deadline — a slow
// close after the first byte never truncates.
func TestSlowProducerBodyStaysWhole(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	swapStdinBody(t, r)

	go func() {
		_, _ = w.WriteString("corpo ")
		time.Sleep(1 * time.Second) // longer than the readiness window
		_, _ = w.WriteString("inteiro")
		_ = w.Close()
	}()
	body, err := readOptionalBody(plainCmd(), "")
	if err != nil {
		t.Fatalf("readOptionalBody: %v", err)
	}
	if body != "corpo inteiro" {
		t.Fatalf("body = %q, want the whole slow body", body)
	}
}

// TestClosedEmptyPipeReadsEmpty: a pipe closed with no data is a valid empty
// body (today's behavior for `: | cerne note new`).
func TestClosedEmptyPipeReadsEmpty(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	_ = w.Close()
	swapStdinBody(t, r)

	body, err := readOptionalBody(plainCmd(), "")
	if err != nil || body != "" {
		t.Fatalf("body, err = %q, %v — want empty, nil", body, err)
	}
}

// TestInjectedReaderBypassesDeadline: a cobra-injected In (tests, programmatic
// use) is read directly — the deadline machinery only touches the real stdin.
func TestInjectedReaderBypassesDeadline(t *testing.T) {
	cmd := plainCmd()
	cmd.SetIn(strings.NewReader("via SetIn"))
	body, err := readOptionalBody(cmd, "")
	if err != nil || body != "via SetIn" {
		t.Fatalf("body, err = %q, %v", body, err)
	}
}

// TestFilePathStillWins: --file bypasses stdin entirely.
func TestFilePathStillWins(t *testing.T) {
	p := filepath.Join(t.TempDir(), "body.md")
	if err := os.WriteFile(p, []byte("do arquivo"), 0o644); err != nil {
		t.Fatal(err)
	}
	r, w, _ := os.Pipe()
	defer r.Close()
	defer w.Close()
	swapStdinBody(t, r) // silent pipe must not even be consulted

	body, err := readOptionalBody(plainCmd(), p)
	if err != nil || body != "do arquivo" {
		t.Fatalf("body, err = %q, %v", body, err)
	}
}

// TestStdinReadsConfinedToHelper is the source guard (criterion 5): the
// blocking-read pattern must not reappear outside stdinbody.go.
func TestStdinReadsConfinedToHelper(t *testing.T) {
	matches, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range matches {
		if f == "stdinbody.go" || strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, banned := range []string{"io.ReadAll(os.Stdin)", "stdinPiped()", "io.ReadAll(cmd.InOrStdin())"} {
			if strings.Contains(string(src), banned) {
				t.Errorf("%s contains %q — optional stdin bodies must go through readOptionalBody (F-0157)", f, banned)
			}
		}
	}
}
