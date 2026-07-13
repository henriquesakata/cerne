package hooks

import (
	"os"
	"strings"
	"testing"
)

// TestParityMatrixGolden (F-0174 crit. 6): the committed doc is exactly what
// the in-code table renders — the published matrix cannot drift from the
// installer. Regenerate with: go test ./internal/hooks/ -run Golden -update
// (or copy ParityMatrix() output into docs/provider-parity.md).
func TestParityMatrixGolden(t *testing.T) {
	want, err := os.ReadFile("../../docs/provider-parity.md")
	if err != nil {
		t.Fatalf("golden missing (generate it from ParityMatrix()): %v", err)
	}
	if got := ParityMatrix(); got != string(want) {
		t.Fatalf("docs/provider-parity.md drifted from the code table — regenerate it from ParityMatrix().\n--- got ---\n%s", got)
	}
}

// TestParityMatrixCoversProviders: every provider column and the degraded
// marker are present — a row cannot silently omit a provider.
func TestParityMatrixCoversProviders(t *testing.T) {
	m := ParityMatrix()
	for _, want := range []string{"Claude Code", "Cursor", "Codex CLI", "Gemini CLI", "degraded"} {
		if !strings.Contains(m, want) {
			t.Fatalf("matrix missing %q", want)
		}
	}
}
