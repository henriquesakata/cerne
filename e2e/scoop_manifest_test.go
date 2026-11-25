//go:build !windows

package e2e_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Black-box tests of scripts/scoop-manifest.sh (F-0508): the manifest the
// release writes to the Scoop bucket, rendered from a fake SHA256SUMS.

func renderScoop(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	script, err := filepath.Abs("../scripts/scoop-manifest.sh")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", append([]string{script}, args...)...)
	var out, errOut strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err = cmd.Run()
	return out.String(), errOut.String(), err
}

// F-0508 crit. 1: valid JSON with the release's version, URL and hash of the
// Windows asset, and the checkver/autoupdate that follow later releases.
func TestScoopManifest_Render(t *testing.T) {
	out, stderr, err := renderScoop(t, "v9.9.9", fakeSums(t, "v9.9.9"))
	if err != nil {
		t.Fatalf("render: %v\n%s", err, stderr)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, out)
	}
	rel := "https://github.com/henriquesakata/cerne/releases/download/"
	want := map[string]any{
		"version":     "9.9.9",
		"description": "Living documentation for projects built with AI agents",
		"homepage":    "https://github.com/henriquesakata/cerne",
		"license":     "Apache-2.0",
		"architecture": map[string]any{"64bit": map[string]any{
			"url":  rel + "v9.9.9/cerne_v9.9.9_windows_amd64.exe#/cerne.exe",
			"hash": assetHash("v9.9.9", "windows_amd64.exe"),
		}},
		"bin":      "cerne.exe",
		"checkver": map[string]any{"github": "https://github.com/henriquesakata/cerne"},
		"autoupdate": map[string]any{
			"architecture": map[string]any{"64bit": map[string]any{
				"url": rel + "v$version/cerne_v$version_windows_amd64.exe#/cerne.exe",
			}},
			"hash": map[string]any{"url": "$baseurl/SHA256SUMS", "regex": `$sha256\s+\*?$basename`},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("manifest:\n got %v\nwant %v", got, want)
	}
}

// F-0508 crit. 2: a missing Windows asset fails, names it, and prints nothing.
func TestScoopManifest_MissingAsset(t *testing.T) {
	out, stderr, err := renderScoop(t, "v9.9.9", fakeSums(t, "v9.9.9", "windows_amd64.exe"))
	if err == nil {
		t.Fatal("render succeeded with the Windows asset missing")
	}
	if !strings.Contains(stderr, "cerne_v9.9.9_windows_amd64.exe") {
		t.Errorf("stderr does not name the missing asset: %q", stderr)
	}
	if out != "" {
		t.Errorf("printed a manifest despite the missing asset:\n%s", out)
	}
}

// F-0508 crit. 2: a tag that is not v<number>... fails, names it, prints nothing.
func TestScoopManifest_BadTag(t *testing.T) {
	for _, tag := range []string{"1.0.0", "latest", "v"} {
		out, stderr, err := renderScoop(t, tag, fakeSums(t, "v9.9.9"))
		if err == nil {
			t.Errorf("render succeeded for tag %q", tag)
		}
		if want := "tag must look like v1.2.3, got '" + tag + "'"; !strings.Contains(stderr, want) {
			t.Errorf("tag %q: stderr %q lacks %q", tag, stderr, want)
		}
		if out != "" {
			t.Errorf("tag %q: printed a manifest:\n%s", tag, out)
		}
	}
}

// F-0508 crit. 7: INSTALL.md tells a Windows user how to install with Scoop and
// lists Scoop among the ways cerne updates.
func TestInstallDoc_Scoop(t *testing.T) {
	raw, err := os.ReadFile("../INSTALL.md")
	if err != nil {
		t.Fatal(err)
	}
	doc := string(raw)
	i := strings.Index(doc, "\n## Scoop (Windows)\n")
	if i < 0 {
		t.Fatal("INSTALL.md has no '## Scoop (Windows)' section")
	}
	section := doc[i+1:]
	if j := strings.Index(section[3:], "\n## "); j >= 0 {
		section = section[:j+3]
	}
	for _, want := range []string{
		"scoop bucket add henriquesakata https://github.com/henriquesakata/scoop-bucket",
		"scoop install henriquesakata/cerne",
	} {
		if !strings.Contains(section, want) {
			t.Errorf("Scoop section lacks %q", want)
		}
	}
	if win := strings.Index(doc, "\n## Quick install (Windows)\n"); win < 0 || win > i {
		t.Error("the Scoop section must come after the Windows quick install")
	}
	upd := strings.Index(doc, "\n### Updating\n")
	if upd < 0 || !strings.Contains(doc[upd:], "- **Scoop:**") {
		t.Error("### Updating lacks the Scoop line")
	}
}

// F-0508 crit. 3: the PR workflow installs the generated manifest with Scoop on
// Windows, checks the version, and has Scoop's own checkver rewrite the
// previous release's manifest to the latest one.
func TestScoopManifest_PackagingWorkflow(t *testing.T) {
	job := parseJob(t, "../.github/workflows/packaging.yml", "scoop")
	if job.RunsOn != "windows-latest" {
		t.Errorf("scoop job runs on %q", job.RunsOn)
	}
	gen := job.stepWith("scripts/scoop-manifest.sh")
	install := job.stepWith("scoop install .\\cerne.json")
	checkver := job.stepWith("checkver.ps1")
	if gen < 0 || install < 0 || checkver < 0 || !(gen < install && install < checkver) {
		t.Fatalf("steps: generate %d, install %d, checkver %d; want them in that order", gen, install, checkver)
	}
	if !strings.Contains(job.Steps[install].Run, "cerne --version") || !strings.Contains(job.Steps[checkver].Run, "-Update") {
		t.Error("install must check cerne --version and checkver must run with -Update")
	}
	if i := job.stepWith("get.scoop.sh"); i < 0 || !strings.Contains(job.Steps[i].Run, "-RunAsAdmin") || i > gen {
		t.Errorf("Scoop must be installed (with -RunAsAdmin) before the manifests are used: step %d", i)
	}
	raw, err := os.ReadFile("../.github/workflows/packaging.yml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "- 'scripts/scoop-manifest.sh'") {
		t.Error("packaging.yml does not run when scripts/scoop-manifest.sh changes")
	}
}
