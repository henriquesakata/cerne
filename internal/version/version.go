// Package version holds the binary's build version as a leaf package, so both
// the CLI (`cerne --version`) and the server (`GET /api/version`) can report
// it without an import cycle (cli already imports server for `cerne ui`).
package version

import "regexp"

// Version defaults to "dev" for local builds and is stamped at release time
// via:
//
//	-ldflags "-X gitlab.com/rockt/cerne/internal/version.Version=<tag>"
var Version = "dev"

var shortRe = regexp.MustCompile(`^v?(\d+\.\d+)`)

// Short derives the user-facing MAJOR.MINOR from the stamped Version
// ("v0.3.0-12-gd7ed7433" → "0.3"); a string without two numeric components
// ("dev", a bare hash) is returned unchanged (F-0144).
func Short() string {
	if m := shortRe.FindStringSubmatch(Version); m != nil {
		return m[1]
	}
	return Version
}
