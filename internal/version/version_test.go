package version

import "testing"

func TestShort(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"git describe", "v0.3.0-12-gd7ed7433", "0.3"},
		{"clean tag", "v0.3.0", "0.3"},
		{"no v prefix", "0.14.2", "0.14"},
		{"two-digit minor describe", "v1.23.4-5-gabc1234", "1.23"},
		{"dev build", "dev", "dev"},
		{"bare hash", "d7ed7433", "d7ed7433"},
		{"single number", "v1", "v1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orig := Version
			Version = tt.in
			defer func() { Version = orig }()
			if got := Short(); got != tt.want {
				t.Errorf("Short() with Version=%q = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
