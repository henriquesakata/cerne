//go:build windows

package e2e_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Black-box tests of install.ps1 (F-0410): the script runs for real, in
// Windows PowerShell 5.1 and in PowerShell 7, against the local stand-in for
// the GitHub release pages (release_server_test.go). CI runs them on
// windows-latest.

// psHosts returns the PowerShell hosts installed here, or skips.
func psHosts(t *testing.T) []string {
	t.Helper()
	var hosts []string
	for _, h := range []string{"powershell", "pwsh"} {
		if _, err := exec.LookPath(h); err == nil {
			hosts = append(hosts, h)
		}
	}
	if len(hosts) == 0 {
		t.Skip("no PowerShell host installed")
	}
	return hosts
}

// keepUserPath restores the registry user PATH when the test ends: the
// script appends its install dir to it.
func keepUserPath(t *testing.T, host string) {
	t.Helper()
	const read = `$k = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment')
$v = $k.GetValue('Path', $null, 'DoNotExpandEnvironmentNames')
if ($null -eq $v) { '<none>' } else { [string]$k.GetValueKind('Path') + '|' + $v }`
	out, err := exec.Command(host, "-NoProfile", "-NonInteractive", "-Command", read).Output()
	if err != nil {
		t.Fatalf("read user PATH: %v", err)
	}
	saved := strings.TrimRight(string(out), "\r\n")
	t.Cleanup(func() {
		const restore = `$k = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment', $true)
if ($env:CERNE_TEST_SAVED -eq '<none>') { $k.DeleteValue('Path', $false) }
else { $kind, $v = $env:CERNE_TEST_SAVED -split '\|', 2; $k.SetValue('Path', $v, $kind) }`
		cmd := exec.Command(host, "-NoProfile", "-NonInteractive", "-Command", restore)
		cmd.Env = append(os.Environ(), "CERNE_TEST_SAVED="+saved)
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("restore user PATH: %v\n%s", err, b)
		}
	})
}

// userPathEntries reads the registry user PATH, unexpanded, split on ';'.
func userPathEntries(t *testing.T, host string) []string {
	t.Helper()
	out, err := exec.Command(host, "-NoProfile", "-NonInteractive", "-Command",
		`[Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment').GetValue('Path', '', 'DoNotExpandEnvironmentNames')`).Output()
	if err != nil {
		t.Fatalf("read user PATH: %v", err)
	}
	return strings.Split(strings.TrimRight(string(out), "\r\n"), ";")
}

// sessionAlive is printed after the script by runPS1's wrapper: an `exit` in
// the script would end the session before it.
const sessionAlive = "SESSION-ALIVE"

// runPS1 runs install.ps1 the way the README does, as text through
// Invoke-Expression inside a session, then prints sessionAlive. A thrown
// error makes the wrapper exit 1 after the marker.
func runPS1(t *testing.T, host string, env ...string) (string, error) {
	t.Helper()
	script, err := filepath.Abs("../install.ps1")
	if err != nil {
		t.Fatal(err)
	}
	keepUserPath(t, host)
	const wrapper = `$failed = $false
try { Invoke-Expression (Get-Content -Raw -LiteralPath $env:CERNE_TEST_SCRIPT) }
catch { Write-Host "ERROR: $_"; $failed = $true }
Write-Host '` + sessionAlive + `'
if ($failed) { exit 1 }`
	cmd := exec.Command(host, "-NoProfile", "-NonInteractive", "-Command", wrapper)
	cmd.Env = append(os.Environ(), append([]string{"CERNE_TEST_SCRIPT=" + script, "CERNE_VERSION="}, env...)...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func assertInstalledExe(t *testing.T, dir, version string) {
	t.Helper()
	got, err := os.ReadFile(filepath.Join(dir, "cerne.exe"))
	if err != nil {
		t.Fatalf("cerne.exe not installed: %v", err)
	}
	if string(got) != fakeBinary(version) {
		t.Errorf("installed content = %q, want the %s asset", got, version)
	}
}

// eachHost runs fn as a subtest per PowerShell host.
func eachHost(t *testing.T, fn func(t *testing.T, host string)) {
	for _, h := range psHosts(t) {
		t.Run(h, func(t *testing.T) { fn(t, h) })
	}
}

// Criterion 5.
func TestInstallPS1_InstallsPinnedVersion(t *testing.T) {
	eachHost(t, func(t *testing.T, host string) {
		srv, _ := releaseServer(t, false)
		dir := t.TempDir()
		out, err := runPS1(t, host, "CERNE_BASE_URL="+srv.URL, "CERNE_VERSION=v1.2.3", "CERNE_INSTALL_DIR="+dir)
		if err != nil {
			t.Fatalf("install failed: %v\n%s", err, out)
		}
		assertInstalledExe(t, dir, "v1.2.3")
		if want := "Installed cerne v1.2.3 to " + filepath.Join(dir, "cerne.exe"); !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	})
}

// Criterion 6.
func TestInstallPS1_ChecksumMismatch(t *testing.T) {
	eachHost(t, func(t *testing.T, host string) {
		srv, _ := releaseServer(t, true)
		dir := t.TempDir()
		out, err := runPS1(t, host, "CERNE_BASE_URL="+srv.URL, "CERNE_VERSION=v1.2.3", "CERNE_INSTALL_DIR="+dir)
		if err == nil {
			t.Fatalf("install succeeded with a tampered SHA256SUMS:\n%s", out)
		}
		if !strings.Contains(out, "checksum mismatch for cerne_v1.2.3_windows_amd64.exe") {
			t.Errorf("output does not name the checksum mismatch and the file:\n%s", out)
		}
		if !strings.Contains(out, sessionAlive) {
			t.Errorf("the session ended inside the script (exit instead of throw):\n%s", out)
		}
		if entries, _ := os.ReadDir(dir); len(entries) != 0 {
			t.Errorf("install dir not empty after a checksum mismatch: %v", entries)
		}
	})
}

// Criterion 7.
func TestInstallPS1_ResolvesLatest(t *testing.T) {
	eachHost(t, func(t *testing.T, host string) {
		srv, _ := releaseServer(t, false)
		dir := t.TempDir()
		out, err := runPS1(t, host, "CERNE_BASE_URL="+srv.URL, "CERNE_INSTALL_DIR="+dir)
		if err != nil {
			t.Fatalf("install failed: %v\n%s", err, out)
		}
		assertInstalledExe(t, dir, fakeLatest)
	})
}

// Criterion 8: the install dir lands on the user PATH once, however many
// times the script runs.
func TestInstallPS1_PathOnce(t *testing.T) {
	eachHost(t, func(t *testing.T, host string) {
		srv, _ := releaseServer(t, false)
		dir := t.TempDir()
		for i := 0; i < 2; i++ {
			if out, err := runPS1(t, host, "CERNE_BASE_URL="+srv.URL, "CERNE_VERSION=v1.2.3", "CERNE_INSTALL_DIR="+dir); err != nil {
				t.Fatalf("install %d failed: %v\n%s", i+1, err, out)
			}
		}
		n := 0
		for _, e := range userPathEntries(t, host) {
			if strings.EqualFold(strings.TrimRight(e, `\`), strings.TrimRight(dir, `\`)) {
				n++
			}
		}
		if n != 1 {
			t.Errorf("user PATH holds the install dir %d times, want 1", n)
		}
	})
}

// Criterion 9.
func TestInstallPS1_Architecture(t *testing.T) {
	eachHost(t, func(t *testing.T, host string) {
		srv, hits := releaseServer(t, false)
		dir := t.TempDir()
		out, err := runPS1(t, host, "CERNE_BASE_URL="+srv.URL, "CERNE_VERSION=v1.2.3", "CERNE_INSTALL_DIR="+dir,
			"PROCESSOR_ARCHITECTURE=x86", "PROCESSOR_ARCHITEW6432=")
		if err == nil {
			t.Fatalf("install succeeded on 32-bit x86:\n%s", out)
		}
		if !strings.Contains(out, "unsupported architecture: x86") {
			t.Errorf("output does not name the refused architecture:\n%s", out)
		}
		if n := hits.Load(); n != 0 {
			t.Errorf("made %d request(s) before refusing the architecture", n)
		}

		out, err = runPS1(t, host, "CERNE_BASE_URL="+srv.URL, "CERNE_VERSION=v1.2.3", "CERNE_INSTALL_DIR="+dir,
			"PROCESSOR_ARCHITECTURE=ARM64", "PROCESSOR_ARCHITEW6432=")
		if err != nil {
			t.Fatalf("install failed on ARM64: %v\n%s", err, out)
		}
		assertInstalledExe(t, dir, "v1.2.3")
		if !strings.Contains(out, "emulation") {
			t.Errorf("output does not say the amd64 build runs under emulation:\n%s", out)
		}
	})
}

func TestInstallPS1_RejectsMalformedVersion(t *testing.T) {
	eachHost(t, func(t *testing.T, host string) {
		srv, hits := releaseServer(t, false)
		out, err := runPS1(t, host, "CERNE_BASE_URL="+srv.URL, "CERNE_VERSION=v1/../../x", "CERNE_INSTALL_DIR="+t.TempDir())
		if err == nil {
			t.Fatalf("install accepted a version with path characters:\n%s", out)
		}
		if n := hits.Load(); n != 0 {
			t.Errorf("made %d request(s) with a malformed version", n)
		}
	})
}

func TestInstallPS1_UpgradeReplacesAndLeavesNoStagingFile(t *testing.T) {
	eachHost(t, func(t *testing.T, host string) {
		srv, _ := releaseServer(t, false)
		dir := t.TempDir()
		for _, v := range []string{"v1.2.3", "v1.2.4"} {
			if out, err := runPS1(t, host, "CERNE_BASE_URL="+srv.URL, "CERNE_VERSION="+v, "CERNE_INSTALL_DIR="+dir); err != nil {
				t.Fatalf("install %s failed: %v\n%s", v, err, out)
			}
		}
		assertInstalledExe(t, dir, "v1.2.4")
		if entries, _ := os.ReadDir(dir); len(entries) != 1 {
			t.Errorf("install dir holds %d entries, want only cerne.exe: %v", len(entries), entries)
		}
	})
}
