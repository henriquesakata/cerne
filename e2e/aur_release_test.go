//go:build !windows

package e2e_test

import (
	"crypto/sha256"
	"encoding/base64"
	"os"
	"strings"
	"testing"

	"go.yaml.in/yaml/v2"
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

// wfStep and wfJob are the parts of a workflow job the structure tests read.
type wfStep struct {
	Name string            `yaml:"name"`
	Uses string            `yaml:"uses"`
	Run  string            `yaml:"run"`
	Env  map[string]string `yaml:"env"`
	If   string            `yaml:"if"`
	With map[string]string `yaml:"with"`
}

type wfJob struct {
	Needs     string   `yaml:"needs"`
	If        string   `yaml:"if"`
	RunsOn    string   `yaml:"runs-on"`
	Container string   `yaml:"container"`
	Steps     []wfStep `yaml:"steps"`
}

// parseJob reads one job of a workflow file as data.
func parseJob(t *testing.T, file, name string) wfJob {
	t.Helper()
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var wf struct {
		Jobs map[string]wfJob `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(raw, &wf); err != nil {
		t.Fatalf("%s: %v", file, err)
	}
	j, ok := wf.Jobs[name]
	if !ok {
		t.Fatalf("%s has no job %q", file, name)
	}
	return j
}

// stepWith is the index of the first step whose run holds s, or -1.
func (j wfJob) stepWith(s string) int {
	for i, st := range j.Steps {
		if strings.Contains(st.Run, s) {
			return i
		}
	}
	return -1
}

// The fingerprints aur.archlinux.org publishes on its home page
// (https://aur.archlinux.org/, "SSH Host Keys"), checked on 2026-11-25.
var aurHostFingerprints = map[string]string{
	"ssh-ed25519":         "SHA256:RFzBCUItH9LZS0cKB5UE6ceAYhBD5C8GeOBip8Z11+4",
	"ecdsa-sha2-nistp256": "SHA256:uTa/0PndEgPZTf76e1DFqXKJEXKsn7m9ivhLQtzGOCI",
	"ssh-rsa":             "SHA256:5s5cIyReIfNNVGRFdDbe3hdYiI5OelHGpw2rOUud3Q8",
}

// F-0507 crit. 4: the aur job runs after the release, for stable tags only, in
// an Arch container; it refuses to run without its key, checks the package
// before pushing, verifies the AUR host key against the published fingerprints
// and commits only when something changed. The shell branches themselves
// (secret missing, no diff, prerelease) are proved at the first stable tag.
// The job also waits for the owner's AUR_PUBLISH variable: AUR registration
// was closed when v0.16.0 shipped, so there was no account to push from, and
// a job that fails every release would hide a real failure.
func TestAURRelease_PublishJob(t *testing.T) {
	job := parseJob(t, "../.github/workflows/release.yml", "aur")
	if job.Needs != "release" || job.If != "${{ !contains(github.ref_name, '-') && vars.AUR_PUBLISH == 'true' }}" || job.Container != "archlinux:base-devel" {
		t.Errorf("aur job needs %q, if %q, container %q", job.Needs, job.If, job.Container)
	}
	key := job.stepWith("::error::AUR_SSH_PRIVATE_KEY is not set")
	check := job.stepWith(`sh scripts/aur-check.sh "$GITHUB_REF_NAME" aur-out`)
	push := job.stepWith("git push")
	if key < 0 || check < 0 || push < 0 || !(key < check && check < push) {
		t.Fatalf("steps: key check %d, aur-check.sh %d, push %d; want them in that order", key, check, push)
	}
	if !strings.Contains(job.Steps[key].Run, "still installs the previous version") || job.Steps[key].Env["AUR_KEY"] != "${{ secrets.AUR_SSH_PRIVATE_KEY }}" {
		t.Errorf("key check step: %+v", job.Steps[key])
	}
	run := job.Steps[push].Run
	for _, want := range []string{
		"umask 077",
		"ssh://aur@aur.archlinux.org/cerne-bin.git",
		"git diff --cached --quiet ||",
		`commit -q -m "cerne $GITHUB_REF_NAME"`,
		// OpenSSH finds the default known_hosts through the passwd home, not
		// $HOME (a container job's differ): name every file explicitly.
		"UserKnownHostsFile=",
		"IdentitiesOnly=yes",
		"StrictHostKeyChecking=yes",
		"-i ",
	} {
		if !strings.Contains(run, want) {
			t.Errorf("push step lacks %q", want)
		}
	}
	for _, bad := range []string{"StrictHostKeyChecking=no", "ssh-keyscan"} {
		if strings.Contains(run, bad) {
			t.Errorf("push step uses %q: the host key must be pinned", bad)
		}
	}
}

// F-0507 crit. 4: the pinned known_hosts lines are the keys whose fingerprints
// aur.archlinux.org publishes: decode each blob, hash it the way OpenSSH
// prints it and compare.
func TestAURRelease_PinnedHostKeys(t *testing.T) {
	job := parseJob(t, "../.github/workflows/release.yml", "aur")
	run := job.Steps[job.stepWith("git push")].Run
	seen := map[string]bool{}
	for _, l := range strings.Split(run, "\n") {
		f := strings.Fields(l)
		if len(f) != 3 || f[0] != "aur.archlinux.org" {
			continue
		}
		blob, err := base64.StdEncoding.DecodeString(f[2])
		if err != nil {
			t.Fatalf("%s key: %v", f[1], err)
		}
		sum := sha256.Sum256(blob)
		got := "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
		if want := aurHostFingerprints[f[1]]; got != want {
			t.Errorf("%s: pinned key hashes to %s, the AUR publishes %s", f[1], got, want)
		}
		seen[f[1]] = true
	}
	if len(seen) != len(aurHostFingerprints) {
		t.Errorf("pinned key types %v, want %d", seen, len(aurHostFingerprints))
	}
}

// F-0507 crit. 5: after the aur job, a clean container installs from the AUR
// itself and compares the version.
func TestAURRelease_CheckJob(t *testing.T) {
	job := parseJob(t, "../.github/workflows/release.yml", "aur-check")
	if job.Needs != "aur" || job.If != "${{ !contains(github.ref_name, '-') }}" || job.Container != "archlinux:base-devel" {
		t.Errorf("aur-check job needs %q, if %q, container %q", job.Needs, job.If, job.Container)
	}
	i := job.stepWith("git clone https://aur.archlinux.org/cerne-bin.git")
	if i < 0 {
		t.Fatal("aur-check job does not clone the AUR package")
	}
	run := job.Steps[i].Run
	for _, want := range []string{
		"makepkg -si",
		"chmod 440 /etc/sudoers.d/builder",
		`want="cerne ${GITHUB_REF_NAME#v}"`,
		"::error::the AUR installed",
	} {
		if !strings.Contains(run, want) {
			t.Errorf("aur-check step lacks %q", want)
		}
	}
}

// F-0508 crit. 4: after the tap, the release job writes bucket/cerne.json to the
// Scoop bucket for stable tags only, refusing to run without its token and
// committing only when something changed. The shell branches themselves
// (token missing, no diff, prerelease) are proved at the first stable tag.
func TestScoopRelease_BucketSteps(t *testing.T) {
	job := parseJob(t, "../.github/workflows/release.yml", "release")
	tap := job.stepWith("update the Homebrew tap")
	if tap < 0 {
		for i, s := range job.Steps {
			if s.Name == "update the Homebrew tap" {
				tap = i
			}
		}
	}
	token, checkout, update := -1, -1, -1
	for i, s := range job.Steps {
		switch {
		case s.Name == "check the Scoop bucket token":
			token = i
		case s.Uses != "" && s.With["repository"] == "henriquesakata/scoop-bucket":
			checkout = i
		case s.Name == "update the Scoop bucket":
			update = i
		}
	}
	if token < 0 || checkout < 0 || update < 0 || !(tap < token && token < checkout && checkout < update) {
		t.Fatalf("steps: tap %d, token check %d, checkout %d, update %d; want them in that order", tap, token, checkout, update)
	}
	stable := "${{ !contains(github.ref_name, '-') }}"
	for _, i := range []int{token, checkout, update} {
		if job.Steps[i].If != stable {
			t.Errorf("step %q runs on %q, want stable tags only", job.Steps[i].Name, job.Steps[i].If)
		}
	}
	tok := job.Steps[token]
	if tok.Env["BUCKET_TOKEN"] != "${{ secrets.SCOOP_BUCKET_TOKEN }}" ||
		!strings.Contains(tok.Run, "::error::SCOOP_BUCKET_TOKEN is not set") ||
		!strings.Contains(tok.Run, "the Scoop bucket still installs the previous version") {
		t.Errorf("token step: %+v", tok)
	}
	if job.Steps[checkout].With["token"] != "${{ secrets.SCOOP_BUCKET_TOKEN }}" {
		t.Errorf("checkout does not use the bucket token: %+v", job.Steps[checkout].With)
	}
	for _, want := range []string{
		`sh scripts/scoop-manifest.sh "$GITHUB_REF_NAME" dist/SHA256SUMS >scoop-bucket/bucket/cerne.json`,
		"git diff --cached --quiet ||",
		`commit -q -m "cerne $GITHUB_REF_NAME"`,
	} {
		if !strings.Contains(job.Steps[update].Run, want) {
			t.Errorf("update step lacks %q", want)
		}
	}
}

// F-0508 crit. 5 and 6: once the bucket is updated, a Windows runner adds it,
// installs from it and checks the version, and that cerne update names Scoop
// without its exit code 1 (a part "not done") failing the job.
func TestScoopRelease_CheckJob(t *testing.T) {
	job := parseJob(t, "../.github/workflows/release.yml", "scoop-check")
	if job.Needs != "release" || job.If != "${{ !contains(github.ref_name, '-') }}" || job.RunsOn != "windows-latest" {
		t.Errorf("scoop-check job needs %q, if %q, runs-on %q", job.Needs, job.If, job.RunsOn)
	}
	i := job.stepWith("scoop install henriquesakata/cerne")
	if i < 0 {
		t.Fatal("scoop-check does not install from the bucket")
	}
	run := job.Steps[i].Run
	for _, want := range []string{
		"get.scoop.sh",
		"scoop bucket add henriquesakata https://github.com/henriquesakata/scoop-bucket",
		"cerne --version",
		"scoop update cerne",
		// The pwsh wrapper ends a step with `exit $LASTEXITCODE`, and cerne
		// update exits 1 by design when a part is not done.
		"$ErrorActionPreference = 'Continue'",
		"$global:LASTEXITCODE = 0",
	} {
		if !strings.Contains(run, want) {
			t.Errorf("scoop-check step lacks %q", want)
		}
	}
	// The binary line is what has to carry the fix.
	if !strings.Contains(run, "binary") {
		t.Error("scoop-check does not assert on the binary line")
	}
	update, reset := strings.Index(run, "cerne update"), strings.LastIndex(run, "$global:LASTEXITCODE = 0")
	if update < 0 || reset < update {
		t.Errorf("the exit code is reset (%d) before cerne update runs (%d)", reset, update)
	}
}
