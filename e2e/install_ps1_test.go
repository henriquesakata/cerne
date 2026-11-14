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

// psEnv is the environment of a PowerShell child: this process's, minus two
// variables a real session would not carry, plus extra. PROCESSOR_ARCHITEW6432
// only exists in a 32-bit process, and Windows copies it over
// PROCESSOR_ARCHITECTURE when it starts a 64-bit one, even when it is empty.
// PSModulePath, inherited from the pwsh CI step, would make Windows
// PowerShell 5.1 load PowerShell 7's modules and miss Get-FileHash; pwsh
// resets it when it starts powershell.exe itself, a Go test does not.
func psEnv(host string, extra ...string) []string {
	var env []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if strings.EqualFold(k, "PROCESSOR_ARCHITEW6432") || (host == "powershell" && strings.EqualFold(k, "PSModulePath")) {
			continue
		}
		env = append(env, kv)
	}
	return append(env, extra...)
}

// keepUserPath restores the registry user PATH when the test ends: the
// script appends its install dir to it.
func keepUserPath(t *testing.T, host string) {
	t.Helper()
	const read = `$k = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment')
$v = $k.GetValue('Path', $null, 'DoNotExpandEnvironmentNames')
if ($null -eq $v) { '<none>' } else { [string]$k.GetValueKind('Path') + '|' + $v }`
	cmd := exec.Command(host, "-NoProfile", "-NonInteractive", "-Command", read)
	cmd.Env = psEnv(host)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("read user PATH: %v", err)
	}
	saved := strings.TrimRight(string(out), "\r\n")
	t.Cleanup(func() {
		const restore = `$k = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment', $true)
if ($env:CERNE_TEST_SAVED -eq '<none>') { $k.DeleteValue('Path', $false) }
else { $kind, $v = $env:CERNE_TEST_SAVED -split '\|', 2; $k.SetValue('Path', $v, $kind) }`
		cmd := exec.Command(host, "-NoProfile", "-NonInteractive", "-Command", restore)
		cmd.Env = psEnv(host, "CERNE_TEST_SAVED="+saved)
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("restore user PATH: %v\n%s", err, b)
		}
	})
}

// userPath reads the registry user PATH unexpanded, with its value kind.
func userPath(t *testing.T, host string) (kind string, entries []string) {
	t.Helper()
	cmd := exec.Command(host, "-NoProfile", "-NonInteractive", "-Command",
		`$k = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment')
[string]$k.GetValueKind('Path') + '|' + $k.GetValue('Path', '', 'DoNotExpandEnvironmentNames')`)
	cmd.Env = psEnv(host)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("read user PATH: %v", err)
	}
	kind, raw, _ := strings.Cut(strings.TrimRight(string(out), "\r\n"), "|")
	return kind, strings.Split(raw, ";")
}

// seedUserPath replaces the registry user PATH (keepUserPath restores it).
func seedUserPath(t *testing.T, host, raw string) {
	t.Helper()
	cmd := exec.Command(host, "-NoProfile", "-NonInteractive", "-Command",
		`[Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment', $true).SetValue('Path', $env:CERNE_TEST_SEED, 'ExpandString')`)
	cmd.Env = psEnv(host, "CERNE_TEST_SEED="+raw)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("seed user PATH: %v\n%s", err, b)
	}
}

// sessionAlive is printed after the script by runPS1's wrapper: an `exit` in
// the script would end the session before it.
const sessionAlive = "SESSION-ALIVE"

// runPS1 runs install.ps1 the way the README does, as text through
// Invoke-Expression inside a session, CERNE_TEST_RUNS times (default 1), from
// CERNE_TEST_CWD when set. Then it prints how many entries of the session's
// $env:Path are CERNE_INSTALL_DIR, and sessionAlive. A thrown error makes the
// wrapper exit 1 after the marker.
func runPS1(t *testing.T, host string, env ...string) (string, error) {
	t.Helper()
	script, err := filepath.Abs("../install.ps1")
	if err != nil {
		t.Fatal(err)
	}
	keepUserPath(t, host)
	const wrapper = `$failed = $false
if ($env:CERNE_TEST_CWD) { Set-Location -LiteralPath $env:CERNE_TEST_CWD }
$runs = 1
if ($env:CERNE_TEST_RUNS) { $runs = [int]$env:CERNE_TEST_RUNS }
for ($i = 0; $i -lt $runs; $i++) {
  try { Invoke-Expression (Get-Content -Raw -LiteralPath $env:CERNE_TEST_SCRIPT) }
  catch { Write-Host "ERROR: $_"; $failed = $true }
}
if ($env:CERNE_INSTALL_DIR) {
  $abs = $ExecutionContext.SessionState.Path.GetUnresolvedProviderPathFromPSPath($env:CERNE_INSTALL_DIR).TrimEnd('\')
  Write-Host ('SESSION-PATH-COUNT=' + @($env:Path -split ';' | Where-Object { $_.TrimEnd('\') -ieq $abs }).Count)
}
Write-Host '` + sessionAlive + `'
if ($failed) { exit 1 }`
	cmd := exec.Command(host, "-NoProfile", "-NonInteractive", "-Command", wrapper)
	cmd.Env = psEnv(host, append([]string{"CERNE_TEST_SCRIPT=" + script, "CERNE_VERSION="}, env...)...)
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

// Criterion 8: the install dir lands on the user PATH and on the session's
// PATH once, however many times the script runs in that session, and the
// user PATH keeps its unexpanded entries and its REG_EXPAND_SZ kind.
func TestInstallPS1_PathOnce(t *testing.T) {
	eachHost(t, func(t *testing.T, host string) {
		keepUserPath(t, host)
		seedUserPath(t, host, `%USERPROFILE%\cerne-test-keep;C:\cerne-test-a`)
		srv, _ := releaseServer(t, false)
		dir := t.TempDir()
		out, err := runPS1(t, host, "CERNE_BASE_URL="+srv.URL, "CERNE_VERSION=v1.2.3", "CERNE_INSTALL_DIR="+dir, "CERNE_TEST_RUNS=2")
		if err != nil {
			t.Fatalf("install failed: %v\n%s", err, out)
		}
		if !strings.Contains(out, "SESSION-PATH-COUNT=1") {
			t.Errorf("the session PATH does not hold the install dir exactly once:\n%s", out)
		}
		kind, entries := userPath(t, host)
		if kind != "ExpandString" {
			t.Errorf("user PATH kind = %q, want ExpandString", kind)
		}
		if len(entries) < 2 || entries[0] != `%USERPROFILE%\cerne-test-keep` || entries[1] != `C:\cerne-test-a` {
			t.Errorf("user PATH lost or expanded its existing entries: %q", entries)
		}
		n := 0
		for _, e := range entries {
			if strings.EqualFold(strings.TrimRight(e, `\`), strings.TrimRight(dir, `\`)) {
				n++
			}
		}
		if n != 1 {
			t.Errorf("user PATH holds the install dir %d times, want 1: %q", n, entries)
		}
	})
}

// A relative CERNE_INSTALL_DIR resolves against the session's folder, and the
// user PATH gets the absolute dir.
func TestInstallPS1_RelativeInstallDir(t *testing.T) {
	eachHost(t, func(t *testing.T, host string) {
		srv, _ := releaseServer(t, false)
		cwd := t.TempDir()
		out, err := runPS1(t, host, "CERNE_BASE_URL="+srv.URL, "CERNE_VERSION=v1.2.3", `CERNE_INSTALL_DIR=rel\bin`, "CERNE_TEST_CWD="+cwd)
		if err != nil {
			t.Fatalf("install failed: %v\n%s", err, out)
		}
		abs := filepath.Join(cwd, "rel", "bin")
		assertInstalledExe(t, abs, "v1.2.3")
		want, err := os.Stat(abs)
		if err != nil {
			t.Fatal(err)
		}
		// The temp dir may come as a short 8.3 name (RUNNER~1) and the script
		// writes the long one: compare the directories, not the strings.
		_, entries := userPath(t, host)
		var found bool
		for _, e := range entries {
			if strings.EqualFold(e, `rel\bin`) {
				t.Errorf("user PATH got the relative dir: %q", entries)
			}
			if st, err := os.Stat(e); err == nil && filepath.IsAbs(e) && os.SameFile(st, want) {
				found = true
			}
		}
		if !found {
			t.Errorf("user PATH lacks %s: %q", abs, entries)
		}
	})
}

// Criterion 9.
func TestInstallPS1_Architecture(t *testing.T) {
	eachHost(t, func(t *testing.T, host string) {
		srv, hits := releaseServer(t, false)
		dir := t.TempDir()
		out, err := runPS1(t, host, "CERNE_BASE_URL="+srv.URL, "CERNE_VERSION=v1.2.3", "CERNE_INSTALL_DIR="+dir,
			"PROCESSOR_ARCHITECTURE=x86")
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
			"PROCESSOR_ARCHITECTURE=ARM64")
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
