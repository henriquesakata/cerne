package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/selfupdate"
	"github.com/henriquesakata/cerne/internal/version"
)

// updateNotice is the session-start line that asks the agent to have the human
// run cerne update (F-0479): "" unless update.notify is on, the notifier is not
// switched off (CERNE_NO_UPDATE_NOTIFIER, CI), cerne update can swap this
// binary, a newer release exists, and this process is the first on the machine
// to hear of it. The only network read is the cached, short-timeout release
// check; any failure is "" (fail-open, N-0021). It changes nothing on disk but
// the check cache and the once-per-version marker.
func updateNotice(projDir string) string {
	if os.Getenv("CERNE_NO_UPDATE_NOTIFIER") != "" || os.Getenv("CI") != "" || !project.UpdateNotify(projDir) {
		return ""
	}
	exe, err := updateExecutable()
	if err != nil {
		return ""
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		resolved = exe
	}
	switch selfupdate.Detect(exe, resolved, version.Version, selfupdate.GoBins()) {
	case selfupdate.Dev, selfupdate.GoInstall:
		return ""
	}
	dir, err := selfupdate.NoticeDir()
	if err != nil {
		return ""
	}
	base := os.Getenv("CERNE_BASE_URL")
	if base == "" {
		base = selfupdate.DefaultBaseURL
	}
	tag, err := selfupdate.LatestCached(base, dir, time.Now(), 2*time.Second)
	if err != nil || !selfupdate.Newer(tag, version.Version) || !selfupdate.ClaimNotice(dir, tag) {
		return ""
	}
	return fmt.Sprintf("cerne: cerne %s is out (this is %s): ask the human to run `cerne update` in their terminal.\n", tag, version.Version)
}
