package domain

import "testing"

func TestNormalizePriority(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"empty stays empty", "", "", false},
		{"blank stays empty", "   ", "", false},
		{"valid p0", "p0", "p0", false},
		{"valid p3", "p3", "p3", false},
		{"lowercases and trims", " P1 ", "p1", false},
		{"unknown rejected", "p9", "", true},
		{"unknown word rejected", "urgent", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizePriority(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (got=%q)", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}
