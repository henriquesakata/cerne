package skills

import (
	"strings"
	"testing"
)

// TestCerneAuthoringDocumentsLightMode (F-0364 crit. 6): the authoring skill
// describes the light mode — the key, what it lifts, what stays — and no
// longer claims that no mode removes the protocol.
func TestCerneAuthoringDocumentsLightMode(t *testing.T) {
	c := All()[0].Content
	for _, want := range []string{`gauntlet = "off"`, "light mode", "spec-missing-bar", "result-missing-gauntlet", "result-missing-evidence", "cerne status"} {
		if !strings.Contains(c, want) {
			t.Errorf("skill missing %q", want)
		}
	}
	if strings.Contains(c, "No project persona, mode or instruction removes this protocol") {
		t.Error("skill still claims no mode removes the protocol")
	}
}
