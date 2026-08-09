package server

import (
	"net/http"
	"os"

	"gitlab.com/rockt/cerne/internal/harness"
)

// harnessHome resolves the user layer's home directory; a test overrides it so
// fixtures never read the developer's real ~ (F-0251).
var harnessHome = func() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}

// handleHarness returns the effective harness inventory of the project
// (F-0251): strictly read-only, degraded layers reported in the payload —
// never an empty error page.
func handleHarness(w http.ResponseWriter, repoDir, projDir string) {
	inv := harness.Inspect(harness.Options{ProjDir: projDir, RepoDir: repoDir, Home: harnessHome()})
	writeJSON(w, http.StatusOK, inv)
}
