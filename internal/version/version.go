// Package version holds the binary's build version as a leaf package, so both
// the CLI (`cerne --version`) and the server (`GET /api/version`) can report
// it without an import cycle (cli already imports server for `cerne ui`).
package version

// Version defaults to "dev" for local builds and is stamped at release time
// via:
//
//	-ldflags "-X gitlab.com/rockt/cerne/internal/version.Version=<tag>"
var Version = "dev"
