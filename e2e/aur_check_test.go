//go:build !windows

package e2e_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// F-0507 crit. 3: scripts/aur-check.sh builds and installs cerne-bin from the
// latest published release and checks `cerne --version`. It needs root in an
// archlinux:base-devel container (pacman, makepkg) and the network, so it runs
// only there; see the plan for the docker line.
func TestAURCheck(t *testing.T) {
	if _, err := exec.LookPath("makepkg"); err != nil || os.Geteuid() != 0 {
		t.Skip("needs root and makepkg: run it in archlinux:base-devel")
	}
	tag, err := exec.Command("sh", "-c", `curl -fsSL https://api.github.com/repos/henriquesakata/cerne/releases/latest | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p'`).Output()
	if err != nil || len(strings.TrimSpace(string(tag))) == 0 {
		t.Fatalf("cannot find the latest release: %v", err)
	}
	out := t.TempDir()
	b, err := exec.Command("sh", "../scripts/aur-check.sh", strings.TrimSpace(string(tag)), out).CombinedOutput()
	t.Logf("%s", b)
	if err != nil {
		t.Fatalf("aur-check: %v", err)
	}
	for _, f := range []string{"PKGBUILD", ".SRCINFO"} {
		if _, err := os.Stat(out + "/" + f); err != nil {
			t.Errorf("out-dir lacks %s: %v", f, err)
		}
	}
}

// F-0507 crit. 3: the PR workflow runs the script in an Arch container, only
// when the packaging changes.
func TestAURCheck_PackagingWorkflow(t *testing.T) {
	raw, err := os.ReadFile("../.github/workflows/packaging.yml")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"pull_request:",
		"- 'scripts/aur-*.sh'",
		"- '.github/workflows/packaging.yml'",
		"container: archlinux:base-devel",
		"releases/latest",
		`sh scripts/aur-check.sh "$tag"`,
	} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("packaging.yml lacks %q", want)
		}
	}
}
