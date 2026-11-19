package skills

import (
	"strings"
	"testing"
)

// TestCerneAuthoringDocumentsGist (F-0459 criterion 9): the authoring skill
// teaches --gist, note gist and the rule for writing one.
func TestCerneAuthoringDocumentsGist(t *testing.T) {
	c := fullAuthoring(t)
	for _, sub := range []string{"--gist", "cerne note gist N-NNNN", "what the agent must do or avoid", "160 runes"} {
		if !strings.Contains(c, sub) {
			t.Errorf("skill missing %q", sub)
		}
	}
}
