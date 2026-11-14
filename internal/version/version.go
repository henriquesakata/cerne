// Package version holds the binary's build version as a leaf package, so both
// the CLI (`cerne --version`) and the server (`GET /api/version`) can report
// it without an import cycle (cli already imports server for `cerne ui`).
package version

import (
	"regexp"
	"runtime/debug"
)

// Version defaults to "dev" for local builds and is stamped at release time
// via:
//
//	-ldflags "-X github.com/henriquesakata/cerne/internal/version.Version=<tag>"
//
// A build without the stamp falls back to the module version Go records, when
// it is a release tag (`go install …/cmd/cerne@v0.10.0`, F-0410).
var Version = "dev"

func init() {
	if Version == "dev" {
		Version = fromBuildInfo(debug.ReadBuildInfo())
	}
}

var (
	releaseRe = regexp.MustCompile(`^v\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$`)
	pseudoRe  = regexp.MustCompile(`\d{14}-[0-9a-f]{12}$`)
)

// fromBuildInfo returns the main module's version when it is a release tag,
// else "dev". Since Go 1.24 a local `go build` stamps a VCS pseudo-version
// (v0.9.1-0.20261113210843-d962cf8b5776, +dirty), which can name an older
// minor than the tag it was built from; local builds stay "dev" (N-0008).
func fromBuildInfo(bi *debug.BuildInfo, ok bool) string {
	if !ok || bi == nil {
		return "dev"
	}
	v := bi.Main.Version
	if !releaseRe.MatchString(v) || pseudoRe.MatchString(v) {
		return "dev"
	}
	return v
}

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
