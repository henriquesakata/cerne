//go:build !windows

package e2e_test

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Black-box tests of scripts/homebrew-formula.sh (F-0410): the formula the
// release writes to the Homebrew tap, rendered from a fake SHA256SUMS.

var formulaAssets = []string{"darwin_amd64", "darwin_arm64", "linux_amd64", "linux_arm64", "windows_amd64.exe"}

// fakeSums writes a SHA256SUMS in the build-release.sh layout, one distinct
// hash per asset of version, skipping the assets in omit.
func fakeSums(t *testing.T, version string, omit ...string) string {
	t.Helper()
	var b strings.Builder
	for _, a := range formulaAssets {
		if contains(omit, a) {
			continue
		}
		fmt.Fprintf(&b, "%s  cerne_%s_%s\n", assetHash(version, a), version, a)
	}
	path := filepath.Join(t.TempDir(), "SHA256SUMS")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func assetHash(version, asset string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(version+asset)))
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func renderFormula(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	script, err := filepath.Abs("../scripts/homebrew-formula.sh")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", append([]string{script}, args...)...)
	var out, errOut strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err = cmd.Run()
	return out.String(), errOut.String(), err
}

// Criterion 1: the version without the v, and each platform block pins the
// URL and hash of its own asset.
func TestHomebrewFormula_Render(t *testing.T) {
	out, stderr, err := renderFormula(t, "v9.9.9", fakeSums(t, "v9.9.9"))
	if err != nil {
		t.Fatalf("render: %v\n%s", err, stderr)
	}
	if !strings.Contains(out, `version "9.9.9"`) {
		t.Errorf(`formula lacks version "9.9.9":\n%s`, out)
	}
	// The block structure with every url/sha256 in place, in order.
	var got []string
	for _, line := range strings.Split(out, "\n") {
		l := strings.TrimSpace(line)
		for _, p := range []string{"on_macos", "on_linux", "on_arm", "on_intel", "url ", "sha256 "} {
			if strings.HasPrefix(l, p) {
				got = append(got, l)
			}
		}
	}
	base := "https://github.com/henriquesakata/cerne/releases/download/v9.9.9/cerne_v9.9.9_"
	pin := func(asset string) []string {
		return []string{`url "` + base + asset + `"`, `sha256 "` + assetHash("v9.9.9", asset) + `"`}
	}
	var want []string
	want = append(want, "on_macos do", "on_arm do")
	want = append(want, pin("darwin_arm64")...)
	want = append(want, "on_intel do")
	want = append(want, pin("darwin_amd64")...)
	want = append(want, "on_linux do", "on_arm do")
	want = append(want, pin("linux_arm64")...)
	want = append(want, "on_intel do")
	want = append(want, pin("linux_amd64")...)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("platform blocks:\n got %q\nwant %q", got, want)
	}
}

// Criterion 2: a platform missing from SHA256SUMS fails, naming the asset,
// and prints no formula.
func TestHomebrewFormula_MissingAsset(t *testing.T) {
	out, stderr, err := renderFormula(t, "v9.9.9", fakeSums(t, "v9.9.9", "linux_arm64"))
	if err == nil {
		t.Fatal("render succeeded with linux_arm64 missing")
	}
	if !strings.Contains(stderr, "cerne_v9.9.9_linux_arm64") {
		t.Errorf("stderr does not name the missing asset: %q", stderr)
	}
	if out != "" {
		t.Errorf("printed a formula despite the missing asset:\n%s", out)
	}
}

// Criterion 3: valid Ruby that installs the binary as cerne and tests it.
func TestHomebrewFormula_RubySyntax(t *testing.T) {
	out, stderr, err := renderFormula(t, "v9.9.9", fakeSums(t, "v9.9.9"))
	if err != nil {
		t.Fatalf("render: %v\n%s", err, stderr)
	}
	for _, want := range []string{`bin.install Dir["cerne_*"].first => "cerne"`, "test do", `#{bin}/cerne --version`} {
		if !strings.Contains(out, want) {
			t.Errorf("formula lacks %q:\n%s", want, out)
		}
	}
	ruby, err := exec.LookPath("ruby")
	if err != nil {
		t.Skip("ruby not installed")
	}
	path := filepath.Join(t.TempDir(), "cerne.rb")
	if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
		t.Fatal(err)
	}
	if b, err := exec.Command(ruby, "-c", path).CombinedOutput(); err != nil {
		t.Fatalf("ruby -c: %v\n%s", err, b)
	}
}
