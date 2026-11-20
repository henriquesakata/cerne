package refresh

import (
	"os/exec"
	"strings"
	"testing"
)

func latestTag(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	out, err := exec.Command("git", "tag", "-l", "v*", "--sort=-v:refname").Output()
	if err != nil {
		t.Skip("not in a git checkout")
	}
	for _, tag := range strings.Fields(string(out)) {
		if !strings.Contains(tag, "-") { // a stable release
			return tag
		}
	}
	t.Skip("no release tag in this checkout")
	return ""
}

func TestKnownCoversTheLatestTag(t *testing.T) {
	tag := latestTag(t)
	if msg := missingRelease(known, tag); msg != "" {
		t.Fatal(msg)
	}
}

func TestKnownMissingReleaseFails(t *testing.T) {
	c := Contents{Tags: map[string]string{"v0.1.0": ""}}
	if msg := missingRelease(c, "v0.2.0"); !strings.Contains(msg, "v0.2.0") {
		t.Fatalf("a list without the latest tag must fail naming it, got %q", msg)
	}
	c.Tags["v0.2.0"] = "does not build"
	if msg := missingRelease(c, "v0.2.0"); !strings.Contains(msg, "does not build") {
		t.Fatalf("a skipped latest tag must fail with the reason, got %q", msg)
	}
	c.Tags["v0.2.0"] = ""
	if msg := missingRelease(c, "v0.2.0"); msg != "" {
		t.Fatalf("a covered tag passes, got %q", msg)
	}
}
