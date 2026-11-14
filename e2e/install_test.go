//go:build !windows

package e2e_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// Black-box tests of install.sh (F-0357): the script runs for real against a
// local server that stands in for the GitHub release pages (release_server_test.go).

// runInstall runs install.sh under shell with a clean environment plus env.
func runInstall(t *testing.T, shell string, env ...string) (string, error) {
	t.Helper()
	script, err := filepath.Abs("../install.sh")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(shell, script)
	cmd.Env = append([]string{"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir()}, env...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func assertInstalled(t *testing.T, dir, version string) {
	t.Helper()
	bin := filepath.Join(dir, "cerne")
	got, err := os.ReadFile(bin)
	if err != nil {
		t.Fatalf("binary not installed: %v", err)
	}
	if string(got) != fakeBinary(version) {
		t.Errorf("installed content = %q, want the %s asset", got, version)
	}
	info, err := os.Stat(bin)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&0o111 == 0 {
		t.Errorf("installed binary is not executable: %v", info.Mode())
	}
}

func TestInstallScript_InstallsPinnedVersion(t *testing.T) {
	srv, _ := releaseServer(t, false)
	dir := t.TempDir()
	out, err := runInstall(t, "sh", "CERNE_BASE_URL="+srv.URL, "CERNE_VERSION=v1.2.3", "CERNE_INSTALL_DIR="+dir)
	if err != nil {
		t.Fatalf("install failed: %v\n%s", err, out)
	}
	assertInstalled(t, dir, "v1.2.3")
}

func TestInstallScript_ChecksumMismatch(t *testing.T) {
	srv, _ := releaseServer(t, true)
	dir := t.TempDir()
	out, err := runInstall(t, "sh", "CERNE_BASE_URL="+srv.URL, "CERNE_VERSION=v1.2.3", "CERNE_INSTALL_DIR="+dir)
	if err == nil {
		t.Fatalf("install succeeded with a tampered SHA256SUMS:\n%s", out)
	}
	if !strings.Contains(out, "checksum mismatch") {
		t.Errorf("output does not name the checksum mismatch:\n%s", out)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("install dir not empty after a checksum mismatch: %v", entries)
	}
}

func TestInstallScript_ResolvesLatest(t *testing.T) {
	srv, _ := releaseServer(t, false)
	dir := t.TempDir()
	out, err := runInstall(t, "sh", "CERNE_BASE_URL="+srv.URL, "CERNE_INSTALL_DIR="+dir)
	if err != nil {
		t.Fatalf("install failed: %v\n%s", err, out)
	}
	assertInstalled(t, dir, fakeLatest)
}

func TestInstallScript_DefaultDirFallback(t *testing.T) {
	if syscall.Access("/usr/local/bin", 2 /* W_OK */) == nil {
		t.Skip("/usr/local/bin is writable here: the script would install into it")
	}
	srv, _ := releaseServer(t, false)
	home := t.TempDir()
	out, err := runInstall(t, "sh", "CERNE_BASE_URL="+srv.URL, "CERNE_VERSION=v1.2.3", "HOME="+home)
	if err != nil {
		t.Fatalf("install failed: %v\n%s", err, out)
	}
	assertInstalled(t, filepath.Join(home, ".local", "bin"), "v1.2.3")
}

func TestInstallScript_WarnsOffPath(t *testing.T) {
	srv, _ := releaseServer(t, false)
	dir := t.TempDir()
	out, err := runInstall(t, "sh", "CERNE_BASE_URL="+srv.URL, "CERNE_VERSION=v1.2.3", "CERNE_INSTALL_DIR="+dir)
	if err != nil {
		t.Fatalf("install failed: %v\n%s", err, out)
	}
	if want := `export PATH="` + dir + `:$PATH"`; !strings.Contains(out, want) {
		t.Errorf("output lacks the export line %q:\n%s", want, out)
	}

	out, err = runInstall(t, "sh", "CERNE_BASE_URL="+srv.URL, "CERNE_VERSION=v1.2.3", "CERNE_INSTALL_DIR="+dir,
		"PATH="+dir+":"+os.Getenv("PATH"))
	if err != nil {
		t.Fatalf("install failed: %v\n%s", err, out)
	}
	if strings.Contains(out, "export PATH") {
		t.Errorf("warned although the install dir is on PATH:\n%s", out)
	}
}

func TestInstallScript_UnsupportedPlatform(t *testing.T) {
	srv, hits := releaseServer(t, false)
	dir := t.TempDir()
	shim := t.TempDir()
	if err := os.WriteFile(filepath.Join(shim, "uname"), []byte("#!/bin/sh\necho plan9\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := runInstall(t, "sh", "CERNE_BASE_URL="+srv.URL, "CERNE_VERSION=v1.2.3", "CERNE_INSTALL_DIR="+dir,
		"PATH="+shim+":"+os.Getenv("PATH"))
	if err == nil {
		t.Fatalf("install succeeded on an unsupported platform:\n%s", out)
	}
	if !strings.Contains(out, "plan9") {
		t.Errorf("output does not name the refused platform:\n%s", out)
	}
	if n := hits.Load(); n != 0 {
		t.Errorf("made %d request(s) before refusing the platform", n)
	}
}

func TestInstallScript_RunsUnderDash(t *testing.T) {
	if _, err := exec.LookPath("dash"); err != nil {
		t.Skip("dash not installed")
	}
	srv, _ := releaseServer(t, false)
	dir := t.TempDir()
	out, err := runInstall(t, "dash", "CERNE_BASE_URL="+srv.URL, "CERNE_INSTALL_DIR="+dir)
	if err != nil {
		t.Fatalf("install under dash failed: %v\n%s", err, out)
	}
	assertInstalled(t, dir, fakeLatest)
}

func TestInstallScript_RejectsMalformedVersion(t *testing.T) {
	srv, hits := releaseServer(t, false)
	dir := t.TempDir()
	out, err := runInstall(t, "sh", "CERNE_BASE_URL="+srv.URL, "CERNE_VERSION=v1/../../x", "CERNE_INSTALL_DIR="+dir)
	if err == nil {
		t.Fatalf("install accepted a version with path characters:\n%s", out)
	}
	if n := hits.Load(); n != 0 {
		t.Errorf("made %d request(s) with a malformed version", n)
	}
}

func TestInstallScript_RefusesDirectoryAtDestination(t *testing.T) {
	srv, _ := releaseServer(t, false)
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "cerne"), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := runInstall(t, "sh", "CERNE_BASE_URL="+srv.URL, "CERNE_VERSION=v1.2.3", "CERNE_INSTALL_DIR="+dir)
	if err == nil {
		t.Fatalf("install reported success over a directory named cerne:\n%s", out)
	}
}

func TestInstallScript_UpgradeReplacesAndLeavesNoStagingFile(t *testing.T) {
	srv, _ := releaseServer(t, false)
	dir := t.TempDir()
	for _, v := range []string{"v1.2.3", "v1.2.4"} {
		if out, err := runInstall(t, "sh", "CERNE_BASE_URL="+srv.URL, "CERNE_VERSION="+v, "CERNE_INSTALL_DIR="+dir); err != nil {
			t.Fatalf("install %s failed: %v\n%s", v, err, out)
		}
	}
	assertInstalled(t, dir, "v1.2.4")
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("install dir holds %d entries, want only cerne: %v", len(entries), entries)
	}
}

func TestInstallScript_NotesShadowingBinary(t *testing.T) {
	srv, _ := releaseServer(t, false)
	dir := t.TempDir()
	other := t.TempDir()
	if err := os.WriteFile(filepath.Join(other, "cerne"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := runInstall(t, "sh", "CERNE_BASE_URL="+srv.URL, "CERNE_VERSION=v1.2.3", "CERNE_INSTALL_DIR="+dir,
		"PATH="+other+":"+dir+":"+os.Getenv("PATH"))
	if err != nil {
		t.Fatalf("install failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, filepath.Join(other, "cerne")) {
		t.Errorf("output does not name the cerne that shadows the new one:\n%s", out)
	}
}
