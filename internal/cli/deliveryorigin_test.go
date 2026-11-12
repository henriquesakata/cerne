package cli

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// F-0355 A2 (CLI): a commit_url host that is not the origin remote's never
// reaches the forge; the complete still succeeds with the crit. 3 line.
func TestCompleteForgeHostNotOriginNeverAsksForge(t *testing.T) {
	f := &cliFakeForge{url: "https://github.com/o/r/pull/12"}
	projDir, repoDir := forgeProject(t, "github", f)
	if out, err := exec.Command("git", "-C", projDir, "remote", "set-url", "origin", "https://evil.example/o/r.git").CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	seedDeliveryFeature(t, repoDir, "F-0001", domain.StateInDevelopment, "spec.md", "plan.md", "result.md")
	_, stderr, err := execCerneSplit(t, "feature", "complete", "F-0001", "--commit", "abc1234", "--dir", projDir)
	if err != nil {
		t.Fatalf("complete must succeed: %v", err)
	}
	if !strings.Contains(stderr, "(host github.com is not the origin remote); link it with 'cerne feature deliver F-0001 --mr <url>'") || f.calls != 0 {
		t.Fatalf("calls %d, stderr:\n%s", f.calls, stderr)
	}
	if got, _ := store.LoadFeature(repoDir, "F-0001"); got.DeliveryMRURL != "" || got.State != domain.StateImplemented {
		t.Fatalf("state %s, MR %q", got.State, got.DeliveryMRURL)
	}
}
