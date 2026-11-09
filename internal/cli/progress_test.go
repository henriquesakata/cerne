package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// F-0876 crit. 2: a slow phase is never mute. It announces itself, beats while
// it runs (with the elapsed time) and closes with its total.
func TestPhaseBeatsWhileSlow(t *testing.T) {
	old := progressInterval
	progressInterval = 10 * time.Millisecond
	t.Cleanup(func() { progressInterval = old })

	var buf bytes.Buffer
	stop := startPhase(&buf, "passo lento")
	time.Sleep(60 * time.Millisecond)
	stop()

	out := buf.String()
	if !strings.HasPrefix(out, "cerne: passo lento...\n") {
		t.Fatalf("phase must announce itself first:\n%s", out)
	}
	if n := strings.Count(out, "cerne: passo lento still running ("); n < 2 {
		t.Fatalf("want >=2 heartbeats during a 60ms phase at 10ms, got %d:\n%s", n, out)
	}
	if !strings.Contains(out, "cerne: passo lento: done (") {
		t.Fatalf("phase must close with its elapsed time:\n%s", out)
	}
	// Stopped means stopped: nothing is written after stop returns.
	before := buf.Len()
	time.Sleep(40 * time.Millisecond)
	if buf.Len() != before {
		t.Fatalf("heartbeat kept writing after stop")
	}
}

// A fast phase prints the announcement and the close, no heartbeat.
func TestPhaseFastHasNoHeartbeat(t *testing.T) {
	var buf bytes.Buffer
	startPhase(&buf, "rapido")()
	if strings.Contains(buf.String(), "still running") {
		t.Fatalf("fast phase must not beat:\n%s", buf.String())
	}
}

// push reports its phases on stderr, never on stdout (stdout stays the result).
func TestPushReportsPhasesOnStderr(t *testing.T) {
	projDir, _, _, specPath := pulledProject(t)
	mustWrite(t, specPath, realDoc("F-0001", "Edicao para ver as fases."))
	stdout, stderr, err := execCerneSplit(t, "push", "--dir", projDir)
	if err != nil {
		t.Fatalf("push: %v", err)
	}
	for _, want := range []string{"cerne: reading changed documents...", "cerne: reconciling with the backend...", "cerne: reconciling with the backend: done ("} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("stderr missing %q:\n%s", want, stderr)
		}
	}
	if strings.Contains(stdout, "cerne: ") {
		t.Fatalf("progress leaked to stdout:\n%s", stdout)
	}
}
