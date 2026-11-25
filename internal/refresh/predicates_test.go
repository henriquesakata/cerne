package refresh

import "testing"

func TestIsWholeFileDir(t *testing.T) {
	for rel, want := range map[string]bool{
		".claude/hooks":                      true,
		".claude/skills/cerne-authoring/ref": true,
		".claude/skills/time":                false,
		".claude/hooks-mine":                 false,
		"hooks":                              false,
	} {
		if got := IsWholeFileDir(rel); got != want {
			t.Errorf("IsWholeFileDir(%q) = %v, want %v", rel, got, want)
		}
	}
}

func TestIsMarkerFile(t *testing.T) {
	for rel, want := range map[string]bool{
		"AGENTS.md":      true,
		"GEMINI.md":      true,
		"docs/AGENTS.md": false,
		"CLAUDE.md":      false,
	} {
		if got := IsMarkerFile(rel); got != want {
			t.Errorf("IsMarkerFile(%q) = %v, want %v", rel, got, want)
		}
	}
}
