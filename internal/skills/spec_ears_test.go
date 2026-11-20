package skills

import (
	"strings"
	"testing"
)

// B-0058: the spec step of the guide carries the EARS rule the prompts
// carry, in both language forms, so an agent following it writes EARS.
func TestSkillSpecStepAsksForEARS(t *testing.T) {
	var step string
	for _, l := range strings.Split(All()[0].Content, "\n") {
		if strings.HasPrefix(l, "2. **Spec**") {
			step = l
		}
	}
	for _, want := range []string{"EARS", "WHEN", "THE SYSTEM SHALL", "QUANDO", "O SISTEMA DEVE"} {
		if !strings.Contains(step, want) {
			t.Errorf("spec step lacks %q", want)
		}
	}
}
