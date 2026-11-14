package version

import (
	"runtime/debug"
	"testing"
)

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

// TestFromBuildInfo pins which module versions a build without -X adopts
// (F-0410 criterion 11): a release tag from `go install …@vX.Y.Z` does; the
// pseudo-version and +dirty suffix Go stamps on a local build do not.
func TestFromBuildInfo(t *testing.T) {
	tests := []struct {
		name, mod string
		ok        bool
		want      string
	}{
		{"release tag", "v0.10.0", true, "v0.10.0"},
		{"prerelease tag", "v1.0.0-rc.1", true, "v1.0.0-rc.1"},
		{"pseudo-version", "v0.9.1-0.20261113210843-d962cf8b5776", true, "dev"},
		{"pseudo-version on a prerelease", "v1.0.0-rc.1.0.20261113210843-d962cf8b5776", true, "dev"},
		{"dirty", "v0.10.0+dirty", true, "dev"},
		{"devel", "(devel)", true, "dev"},
		{"empty", "", true, "dev"},
		{"no build info", "", false, "dev"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var bi *debug.BuildInfo
			if tt.ok {
				bi = &debug.BuildInfo{Main: debug.Module{Version: tt.mod}}
			}
			if got := fromBuildInfo(bi, tt.ok); got != tt.want {
				t.Errorf("fromBuildInfo(%q, %v) = %q, want %q", tt.mod, tt.ok, got, tt.want)
			}
		})
	}
}
