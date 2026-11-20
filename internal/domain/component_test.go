package domain

import "testing"

func TestNearComponent(t *testing.T) {
	tests := []struct {
		name  string
		vocab []string
		want  string // "" = not near
	}{
		// same root: lower, no - or _, an optional final ing/es/s on either side
		{"tests", []string{"test"}, "test"},
		{"testing", []string{"tests"}, "tests"},
		{"skills", []string{"skill"}, "skill"},
		{"tooling", []string{"tools"}, "tools"},
		{"notes", []string{"note"}, "note"},
		{"classes", []string{"class"}, "class"},
		{"git-back", []string{"gitback"}, "gitback"},
		{"api", []string{"apis"}, "apis"},
		// up to 3 letters: only the same root refuses
		{"ci", []string{"ui"}, ""},
		{"git", []string{"go"}, ""},
		{"mcp", []string{"mcpx"}, ""},
		// 4 or 5 letters: up to one character
		{"lint", []string{"link"}, "link"},
		{"gitea", []string{"gitee"}, "gitee"},
		{"lint", []string{"lime"}, ""},
		{"node", []string{"nodejs"}, ""}, // the shorter name sets the tier
		// 6 letters or more: up to two characters
		{"materialise", []string{"materialize"}, "materialize"},
		{"doclang", []string{"doclint"}, "doclint"},
		{"gitback", []string{"github"}, ""},
		// an existing name is not new, a genuinely new one is not near
		{"test", []string{"test", "tests"}, ""},
		{"kpack", []string{"gitea", "test"}, ""},
		{"kpack", nil, ""},
		// the closest suggestion wins: a same root beats a distance
		{"tests", []string{"testx", "test"}, "test"},
	}
	for _, tt := range tests {
		t.Run(tt.name+"/"+tt.want, func(t *testing.T) {
			got, ok := NearComponent(tt.name, tt.vocab)
			if ok != (tt.want != "") || got != tt.want {
				t.Fatalf("NearComponent(%q, %v) = %q, %v; want %q", tt.name, tt.vocab, got, ok, tt.want)
			}
		})
	}
}

func TestSameComponentRoot(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{"testing", "tests", true},
		{"tool", "tooling", true},
		{"foo_bar", "foo-bar", true},
		{"lint", "link", false}, // near by distance, not by root
		{"es", "s", false},      // a suffix alone is no root
	}
	for _, tt := range tests {
		if got := SameComponentRoot(tt.a, tt.b); got != tt.want {
			t.Errorf("SameComponentRoot(%q, %q) = %v; want %v", tt.a, tt.b, got, tt.want)
		}
	}
}
