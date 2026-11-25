//go:build !windows

package e2e_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Black-box tests of scripts/aur-pkgbuild.sh (F-0507): the PKGBUILD the release
// pushes to the AUR as cerne-bin, rendered from a fake SHA256SUMS.

func renderPKGBUILD(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	script, err := filepath.Abs("../scripts/aur-pkgbuild.sh")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", append([]string{script}, args...)...)
	var out, errOut strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err = cmd.Run()
	return out.String(), errOut.String(), err
}

// F-0507 crit. 1: the package metadata, and per architecture the URL and hash
// of its own asset.
func TestAURPKGBUILD_Render(t *testing.T) {
	out, stderr, err := renderPKGBUILD(t, "v9.9.9", fakeSums(t, "v9.9.9"))
	if err != nil {
		t.Fatalf("render: %v\n%s", err, stderr)
	}
	base := "https://github.com/henriquesakata/cerne/releases/download/v${pkgver}/cerne_v${pkgver}_"
	for _, want := range []string{
		"pkgname=cerne-bin\n",
		"pkgver=9.9.9\n",
		"pkgrel=1\n",
		"arch=('x86_64' 'aarch64')\n",
		"license=('Apache-2.0')\n",
		"provides=('cerne')\n",
		"conflicts=('cerne')\n",
		"options=('!strip' '!debug')\n",
		`source_x86_64=("cerne-${pkgver}-x86_64::` + base + "linux_amd64\")\n",
		"sha256sums_x86_64=('" + assetHash("v9.9.9", "linux_amd64") + "')\n",
		`source_aarch64=("cerne-${pkgver}-aarch64::` + base + "linux_arm64\")\n",
		"sha256sums_aarch64=('" + assetHash("v9.9.9", "linux_arm64") + "')\n",
		`install -Dm755 "cerne-${pkgver}-${CARCH}" "${pkgdir}/usr/bin/cerne"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("PKGBUILD lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "@") {
		t.Errorf("PKGBUILD carries an e-mail address:\n%s", out)
	}
	if bash, err := exec.LookPath("bash"); err == nil {
		cmd := exec.Command(bash, "-n")
		cmd.Stdin = strings.NewReader(out)
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("bash -n: %v\n%s", err, b)
		}
	}
}

// F-0507 crit. 2: a missing Linux asset fails, says which, and prints nothing.
func TestAURPKGBUILD_MissingAsset(t *testing.T) {
	for _, asset := range []string{"linux_amd64", "linux_arm64"} {
		out, stderr, err := renderPKGBUILD(t, "v9.9.9", fakeSums(t, "v9.9.9", asset))
		if err == nil {
			t.Fatalf("render succeeded with %s missing", asset)
		}
		if !strings.Contains(stderr, "cerne_v9.9.9_"+asset) {
			t.Errorf("%s: stderr does not name the missing asset: %q", asset, stderr)
		}
		if out != "" {
			t.Errorf("%s: printed a PKGBUILD despite the missing asset:\n%s", asset, out)
		}
	}
}

// F-0507 crit. 7: INSTALL.md tells an Arch user how to install from the AUR
// (with and without a helper) and lists the AUR among the ways cerne updates.
func TestInstallDoc_AUR(t *testing.T) {
	raw, err := os.ReadFile("../INSTALL.md")
	if err != nil {
		t.Fatal(err)
	}
	doc := string(raw)
	i := strings.Index(doc, "\n## Arch Linux (AUR)\n")
	if i < 0 {
		t.Fatal("INSTALL.md has no '## Arch Linux (AUR)' section")
	}
	section := doc[i+1:]
	if j := strings.Index(section[3:], "\n## "); j >= 0 {
		section = section[:j+3]
	}
	for _, want := range []string{"yay -S cerne-bin", "git clone https://aur.archlinux.org/cerne-bin.git", "cd cerne-bin", "makepkg -si"} {
		if !strings.Contains(section, want) {
			t.Errorf("AUR section lacks %q", want)
		}
	}
	if brew := strings.Index(doc, "\n## Homebrew (macOS and Linux)\n"); brew < 0 || brew > i {
		t.Error("the AUR section must come after the Homebrew one")
	}
	upd := strings.Index(doc, "\n### Updating\n")
	if upd < 0 || !strings.Contains(doc[upd:], "- **Arch Linux (AUR):**") {
		t.Error("### Updating lacks the AUR line")
	}
}

// F-0507 crit. 2: a tag the pkgver cannot take (prerelease hyphen, no v).
func TestAURPKGBUILD_BadTag(t *testing.T) {
	for _, tag := range []string{"v1.0.0-rc.1", "1.0.0", "v1.0", "v1.0.0.1"} {
		out, stderr, err := renderPKGBUILD(t, tag, fakeSums(t, tag))
		if err == nil {
			t.Errorf("render succeeded for tag %q", tag)
		}
		if !strings.Contains(stderr, tag) {
			t.Errorf("tag %q: stderr does not name it: %q", tag, stderr)
		}
		if out != "" {
			t.Errorf("tag %q: printed a PKGBUILD:\n%s", tag, out)
		}
	}
}

