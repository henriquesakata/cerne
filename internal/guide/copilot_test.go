package guide

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/hooks"
)

// TestGuideShowsDegradedCopilotRows (F-0444 crit. 4): the first section the
// guide returns for a Copilot question names every capability the parity
// matrix marks degraded for Copilot, and says what is verified.
func TestGuideShowsDegradedCopilotRows(t *testing.T) {
	got := Search(Corpus(&cobra.Command{Use: "cerne"}), "what is degraded or unverified when cerne runs on GitHub Copilot", 3)
	if len(got) == 0 || !strings.Contains(got[0].Title, "degraded or unverified") {
		t.Fatalf("first section is not the Copilot degradation answer: %+v", got)
	}
	text := got[0].Text
	n := 0
	for _, r := range hooks.ParityRows() {
		if d, _ := r.Degraded(hooks.ProviderCopilot); d {
			n++
			if !strings.Contains(text, r.Capability) {
				t.Errorf("degraded row %q missing from the guide answer:\n%s", r.Capability, text)
			}
		}
	}
	if n < 4 {
		t.Fatalf("expected the compaction, author, subagent, stop and double-run rows degraded, got %d", n)
	}
	for _, want := range []string{"Copilot CLI 1.0.112", "VS Code", "rewrit"} {
		if !strings.Contains(text, want) {
			t.Errorf("answer lacks %q", want)
		}
	}
}
