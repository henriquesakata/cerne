//go:build !windows

package e2e_test

import (
	"os"
	"strings"
	"testing"
)

// jobOf returns the text of one top-level job of a workflow file.
func jobOf(t *testing.T, wf, name string) string {
	t.Helper()
	i := strings.Index(wf, "\n  "+name+":\n")
	if i < 0 {
		t.Fatalf("workflow has no job %q", name)
	}
	job := wf[i+1:]
	// The next top-level job starts at a line with exactly two spaces of indent.
	for _, l := range strings.SplitAfter(job, "\n")[1:] {
		if len(l) > 3 && strings.HasPrefix(l, "  ") && l[2] != ' ' && l[2] != '#' && strings.HasSuffix(strings.TrimSpace(l), ":") {
			return job[:strings.Index(job, l)]
		}
	}
	return job
}

// F-0507 crit. 4: the aur job runs after the release, for stable tags only, in
// an Arch container; it refuses to run without its key, checks the package
// before pushing, verifies the AUR host key against the published fingerprints
// and commits only when something changed.
func TestAURRelease_PublishJob(t *testing.T) {
	raw, err := os.ReadFile("../.github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	job := jobOf(t, string(raw), "aur")
	for _, want := range []string{
		"needs: release",
		"if: ${{ !contains(github.ref_name, '-') }}",
		"container: archlinux:base-devel",
		"secrets.AUR_SSH_PRIVATE_KEY",
		"::error::AUR_SSH_PRIVATE_KEY is not set",
		"still installs the previous version",
		"StrictHostKeyChecking=yes",
		"ssh://aur@aur.archlinux.org/cerne-bin.git",
		"git diff --cached --quiet ||",
		`commit -q -m "cerne $GITHUB_REF_NAME"`,
	} {
		if !strings.Contains(job, want) {
			t.Errorf("aur job lacks %q", want)
		}
	}
	for _, bad := range []string{"StrictHostKeyChecking=no", "ssh-keyscan"} {
		if strings.Contains(job, bad) {
			t.Errorf("aur job uses %q: the host key must be pinned", bad)
		}
	}
	// The three published fingerprints, as `ssh-keygen -lf` prints them for
	// the pinned known_hosts lines (checked by hand against aur.archlinux.org).
	for _, want := range []string{"aur.archlinux.org ssh-ed25519 ", "aur.archlinux.org ecdsa-sha2-nistp256 ", "aur.archlinux.org ssh-rsa "} {
		if !strings.Contains(job, want) {
			t.Errorf("aur job lacks the pinned host key %q", want)
		}
	}
	check := strings.Index(job, `sh scripts/aur-check.sh "$GITHUB_REF_NAME" aur-out`)
	push := strings.Index(job, "git push")
	if check < 0 || push < 0 || check > push {
		t.Errorf("aur-check.sh (%d) must come before the push (%d)", check, push)
	}
}

// F-0507 crit. 5: after the aur job, a clean container installs from the AUR
// itself and compares the version.
func TestAURRelease_CheckJob(t *testing.T) {
	raw, err := os.ReadFile("../.github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	job := jobOf(t, string(raw), "aur-check")
	for _, want := range []string{
		"needs: aur",
		"if: ${{ !contains(github.ref_name, '-') }}",
		"container: archlinux:base-devel",
		"git clone https://aur.archlinux.org/cerne-bin.git",
		"makepkg -si",
		`want="cerne ${GITHUB_REF_NAME#v}"`,
		"::error::the AUR installed",
	} {
		if !strings.Contains(job, want) {
			t.Errorf("aur-check job lacks %q", want)
		}
	}
}
