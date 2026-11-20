package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/hooks"
	"github.com/henriquesakata/cerne/internal/selfupdate"
	"github.com/henriquesakata/cerne/internal/skills"
	"github.com/henriquesakata/cerne/internal/version"
)

// fakeRelease serves a GitHub-shaped release of tag whose asset for this
// platform is asset, and counts every request.
func fakeRelease(t *testing.T, tag string, asset []byte) *int {
	t.Helper()
	hits := new(int)
	name := selfupdate.AssetName(tag, runtime.GOOS, runtime.GOARCH)
	sum := sha256.Sum256(asset)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*hits++
		switch r.URL.Path {
		case "/releases/latest":
			http.Redirect(w, r, "/releases/tag/"+tag, http.StatusFound)
		case "/releases/tag/" + tag:
		case "/releases/download/" + tag + "/" + name:
			w.Write(asset)
		case "/releases/download/" + tag + "/SHA256SUMS":
			fmt.Fprintf(w, "%s  %s\n", hex.EncodeToString(sum[:]), name)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("CERNE_BASE_URL", srv.URL)
	return hits
}

// installedBinary makes the running binary a release ver installed by the
// script at a temp path, never inside a Go bin folder; returns that path.
func installedBinary(t *testing.T, ver string) string {
	t.Helper()
	exe := filepath.Join(t.TempDir(), "cerne")
	if err := os.WriteFile(exe, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	setRunningBinary(t, exe, ver)
	return exe
}

func setRunningBinary(t *testing.T, exe, ver string) {
	t.Helper()
	prevExe, prevVer := updateExecutable, version.Version
	updateExecutable = func() (string, error) { return exe, nil }
	version.Version = ver
	t.Setenv("GOBIN", "")
	t.Setenv("GOPATH", t.TempDir())
	t.Cleanup(func() { updateExecutable, version.Version = prevExe, prevVer })
}

// currentBinary is a script install already on the latest release.
func currentBinary(t *testing.T) *int {
	t.Helper()
	installedBinary(t, "v9.9.9")
	return fakeRelease(t, "v9.9.9", []byte("same"))
}

// staleProject is an initialized project whose managed files fell behind:
// one hook script edited, one deleted, the guide's SKILL.md edited.
func staleProject(t *testing.T) string {
	t.Helper()
	projDir, _, _ := setupProject(t)
	t.Setenv("HOME", t.TempDir())
	assets := hooks.Assets()
	hookDir := filepath.Join(projDir, ".claude", "hooks")
	appendTo(t, filepath.Join(hookDir, assets[0].Script), "\n# local edit\n")
	if err := os.Remove(filepath.Join(hookDir, assets[1].Script)); err != nil {
		t.Fatal(err)
	}
	appendTo(t, filepath.Join(projDir, ".claude", "skills", "cerne-authoring", "SKILL.md"), "\nold guide\n")
	if len(hooks.Stale(projDir)) != 2 || len(skills.Stale(projDir)) != 1 {
		t.Fatalf("fixture not stale: hooks %v, skills %v", hooks.Stale(projDir), skills.Stale(projDir))
	}
	return projDir
}

func appendTo(t *testing.T, path, s string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(s); err != nil {
		t.Fatal(err)
	}
}

// F-0476 crit. 1 and 2: the update renews the hooks and the guide like
// `hook install` + `skill install`, connects no new agent, and closes with
// one line per part; the hook change applies from the next session.
func TestUpdateRenewsHooksAndGuide(t *testing.T) {
	projDir := staleProject(t)
	currentBinary(t)

	out, err := execCerne(t, "update", "--dir", projDir)
	if err != nil {
		t.Fatalf("update: %v\n%s", err, out)
	}
	if s := hooks.Stale(projDir); len(s) > 0 {
		t.Errorf("hooks still stale: %v", s)
	}
	if s := skills.Stale(projDir); len(s) > 0 {
		t.Errorf("guide still stale: %v", s)
	}
	for _, d := range []string{".cursor", ".codex", ".gemini", ".github"} {
		if _, err := os.Stat(filepath.Join(projDir, d)); err == nil {
			t.Errorf("update connected an agent that was not connected: %s exists", d)
		}
	}
	for _, want := range []string{
		"binary: up to date",
		"hooks: updated — " + hooks.Assets()[0].Script,
		hooks.Assets()[1].Script,
		"guide: updated — .claude/skills/cerne-authoring",
		"from the agent's next session",
		"local edits to managed hooks are not kept",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

// F-0476 crit. 2: each part is updated, up to date or not done (with the
// reason and the command), and the exit is 0 only with nothing not done.
func TestUpdateClosingStates(t *testing.T) {
	t.Run("everything current exits 0", func(t *testing.T) {
		projDir, _, _ := setupProject(t)
		currentBinary(t)
		out, err := execCerne(t, "update", "--dir", projDir)
		if err != nil {
			t.Fatalf("update: %v\n%s", err, out)
		}
		for _, want := range []string{"binary: up to date", "hooks: up to date", "guide: up to date"} {
			if !strings.Contains(out, want) {
				t.Errorf("output missing %q:\n%s", want, out)
			}
		}
		if strings.Contains(out, "next session") {
			t.Errorf("no hook changed, yet the session note printed:\n%s", out)
		}
	})
	t.Run("hooks not done exits non-zero", func(t *testing.T) {
		projDir := staleProject(t)
		currentBinary(t)
		if err := os.WriteFile(filepath.Join(projDir, ".claude", "settings.json"), []byte("{not json"), 0o644); err != nil {
			t.Fatal(err)
		}
		out, err := execCerne(t, "update", "--dir", projDir)
		if err == nil || ExitCode(err) == 0 {
			t.Fatalf("update with a corrupt settings.json: err %v, want a non-zero exit\n%s", err, out)
		}
		if !strings.Contains(out, "hooks: not done — ") || !strings.Contains(out, "run: cerne hook install") {
			t.Errorf("hooks line is not a not-done with its command:\n%s", out)
		}
		if !strings.Contains(out, "guide: updated") {
			t.Errorf("the guide is renewed even when the hooks fail:\n%s", out)
		}
	})
	t.Run("binary not done exits non-zero", func(t *testing.T) {
		projDir := staleProject(t)
		installedBinary(t, "dev")
		out, err := execCerne(t, "update", "--dir", projDir)
		if err == nil {
			t.Fatalf("update on a dev build: nil error\n%s", out)
		}
		if !strings.Contains(out, "binary: not done — ") || !strings.Contains(out, "hooks: updated") {
			t.Errorf("want binary not done and the files still renewed:\n%s", out)
		}
	})
}

// F-0476 crit. 3: --check says what would be renewed, writes nothing, calls
// no network and exits non-zero while something is stale.
func TestUpdateCheck(t *testing.T) {
	projDir := staleProject(t)
	hits := currentBinary(t)
	before := treeDigest(t, projDir)

	out, err := execCerne(t, "update", "--check", "--dir", projDir)
	if err == nil {
		t.Fatalf("--check on a stale project: nil error\n%s", out)
	}
	for _, want := range []string{"hooks: would update — ", hooks.Assets()[1].Script, "guide: would update — cerne-authoring", "cerne update"} {
		if !strings.Contains(out, want) {
			t.Errorf("--check output missing %q:\n%s", want, out)
		}
	}
	if after := treeDigest(t, projDir); after != before {
		t.Error("--check wrote to the project")
	}
	if *hits != 0 {
		t.Errorf("--check made %d request(s); it must not use the network", *hits)
	}

	if out, err := execCerne(t, "update", "--dir", projDir); err != nil {
		t.Fatalf("update: %v\n%s", err, out)
	}
	*hits = 0
	out, err = execCerne(t, "update", "--check", "--dir", projDir)
	if err != nil || !strings.Contains(out, "hooks: up to date") || !strings.Contains(out, "guide: up to date") || *hits != 0 {
		t.Fatalf("--check on a current project: err %v, hits %d\n%s", err, *hits, out)
	}
}

// marker is a fake cerne binary: a shell script that records its arguments.
func marker(t *testing.T) (script []byte, recorded string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake binary is a shell script")
	}
	recorded = filepath.Join(t.TempDir(), "ran")
	return []byte("#!/bin/sh\necho \"$@\" > '" + recorded + "'\necho 'hooks: up to date (new binary)'\n"), recorded
}

// F-0476 crit. 7 and 8: a script install downloads the latest release,
// verifies it, swaps it in place, and the new binary renews the files.
func TestUpdateReplacesScriptInstall(t *testing.T) {
	projDir, _, _ := setupProject(t)
	newBin, ran := marker(t)
	exe := installedBinary(t, "v0.14.3")
	fakeRelease(t, "v0.15.0", newBin)

	out, err := execCerne(t, "update", "--dir", projDir)
	if err != nil {
		t.Fatalf("update: %v\n%s", err, out)
	}
	if got, _ := os.ReadFile(exe); string(got) != string(newBin) {
		t.Fatalf("executable not replaced; it holds %q", got)
	}
	if !strings.Contains(out, "binary: updated — v0.14.3 → v0.15.0") {
		t.Errorf("binary line missing:\n%s", out)
	}
	args, err := os.ReadFile(ran)
	if err != nil {
		t.Fatalf("the new binary never ran: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(args)); got != "update --files-only --dir "+projDir {
		t.Errorf("new binary ran with %q", got)
	}
	if !strings.Contains(out, "(new binary)") {
		t.Errorf("the new binary's output was not passed on:\n%s", out)
	}
}

// F-0476 crit. 6 and 8: under Homebrew, Homebrew replaces the binary, and the
// keg it links next renews the files.
func TestUpdateHomebrew(t *testing.T) {
	projDir, _, _ := setupProject(t)
	newBin, ran := marker(t)
	prefix := t.TempDir()
	oldKeg := filepath.Join(prefix, "Cellar", "cerne", "0.14.3", "bin", "cerne")
	os.MkdirAll(filepath.Dir(oldKeg), 0o755)
	mustWrite(t, oldKeg, "old")
	link := filepath.Join(prefix, "bin", "cerne")
	os.MkdirAll(filepath.Dir(link), 0o755)
	if err := os.Symlink(oldKeg, link); err != nil {
		t.Fatal(err)
	}
	newSrc := filepath.Join(t.TempDir(), "new")
	os.WriteFile(newSrc, newBin, 0o755)
	brewArgs := filepath.Join(t.TempDir(), "brew-args")
	brew := filepath.Join(t.TempDir(), "brew")
	os.WriteFile(brew, []byte(fmt.Sprintf(`#!/bin/sh
echo "$@" > '%s'
k='%s/Cellar/cerne/0.15.0/bin'
mkdir -p "$k" && cp '%s' "$k/cerne" && ln -sf "$k/cerne" '%s'
`, brewArgs, prefix, newSrc, link)), 0o755)
	prevBrew := brewCommand
	brewCommand = brew
	t.Cleanup(func() { brewCommand = prevBrew })
	setRunningBinary(t, link, "v0.14.3")
	hits := fakeRelease(t, "v0.15.0", nil)

	out, err := execCerne(t, "update", "--dir", projDir)
	if err != nil {
		t.Fatalf("update: %v\n%s", err, out)
	}
	if got, _ := os.ReadFile(brewArgs); strings.TrimSpace(string(got)) != "upgrade henriquesakata/tap/cerne" {
		t.Errorf("brew ran with %q", got)
	}
	if !strings.Contains(out, "binary: updated — by Homebrew") {
		t.Errorf("binary line missing:\n%s", out)
	}
	if _, err := os.Stat(ran); err != nil {
		t.Errorf("the new keg never ran: %v\n%s", err, out)
	}
	if *hits != 0 {
		t.Errorf("the Homebrew path asked GitHub %d time(s); Homebrew does that", *hits)
	}

	// Homebrew had nothing newer: the link still points at the same keg.
	os.WriteFile(brew, []byte("#!/bin/sh\n"), 0o755)
	setRunningBinary(t, link, "v0.15.0")
	out, err = execCerne(t, "update", "--dir", projDir)
	if err != nil || !strings.Contains(out, "binary: up to date") {
		t.Fatalf("brew with nothing newer: err %v\n%s", err, out)
	}
}

// F-0476 crit. 9: a dev build, a go install and an unwritable folder keep
// the binary, say why and the command, and the files are still renewed.
func TestUpdateKeepsBinaryItCannotReplace(t *testing.T) {
	cases := []struct {
		name, reason, fix string
		setup             func(t *testing.T)
	}{
		{"dev build", "local build", "go build", func(t *testing.T) { installedBinary(t, "dev") }},
		{"go install", "go install", "go install github.com/henriquesakata/cerne/cmd/cerne@latest", func(t *testing.T) {
			exe := installedBinary(t, "v0.14.3")
			t.Setenv("GOBIN", filepath.Dir(exe))
			fakeRelease(t, "v0.15.0", []byte("new"))
		}},
		{"unwritable folder", "cannot write", "install.sh", func(t *testing.T) {
			if runtime.GOOS == "windows" || os.Geteuid() == 0 {
				t.Skip("needs POSIX permissions and a non-root user")
			}
			exe := installedBinary(t, "v0.14.3")
			fakeRelease(t, "v0.15.0", []byte("new"))
			os.Chmod(filepath.Dir(exe), 0o555)
			t.Cleanup(func() { os.Chmod(filepath.Dir(exe), 0o755) })
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			projDir := staleProject(t)
			c.setup(t)
			out, err := execCerne(t, "update", "--dir", projDir)
			if err == nil {
				t.Fatalf("nil error with the binary not done\n%s", out)
			}
			line := lineWith(out, "binary:")
			if !strings.Contains(line, "not done — ") || !strings.Contains(line, c.reason) || !strings.Contains(line, "run: ") || !strings.Contains(line, c.fix) {
				t.Errorf("binary line = %q, want not done with %q and %q", line, c.reason, c.fix)
			}
			if !strings.Contains(out, "hooks: updated") || len(hooks.Stale(projDir)) > 0 {
				t.Errorf("files not renewed:\n%s", out)
			}
		})
	}
}

func lineWith(out, prefix string) string {
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, prefix) {
			return l
		}
	}
	return ""
}

// F-0476 crit. 10: an agent never replaces the binary, and asks nothing of
// the network; the files are renewed.
func TestUpdateAgentKeepsBinary(t *testing.T) {
	projDir := staleProject(t)
	exe := installedBinary(t, "v0.14.3")
	hits := fakeRelease(t, "v0.15.0", []byte("new"))
	t.Setenv("CERNE_ACTOR", "agent")

	out, err := execCerne(t, "update", "--dir", projDir)
	if err == nil {
		t.Fatalf("nil error with the binary not done\n%s", out)
	}
	if line := lineWith(out, "binary:"); !strings.Contains(line, "not done — ") || !strings.Contains(line, "human") || !strings.Contains(line, "own terminal") {
		t.Errorf("binary line = %q, want the human's part", line)
	}
	if got, _ := os.ReadFile(exe); string(got) != "old binary" || *hits != 0 {
		t.Errorf("agent touched the binary (%q) or the network (%d)", got, *hits)
	}
	if !strings.Contains(out, "hooks: updated") {
		t.Errorf("files not renewed:\n%s", out)
	}
}

// F-0476 crit. 12: outside a project only the binary is looked at.
func TestUpdateOutsideProject(t *testing.T) {
	t.Chdir(t.TempDir())
	currentBinary(t)
	out, err := execCerne(t, "update")
	if err != nil {
		t.Fatalf("update outside a project: %v\n%s", err, out)
	}
	if !strings.Contains(out, "binary: up to date") || !strings.Contains(out, "no project was touched") || strings.Contains(out, "hooks:") {
		t.Errorf("output:\n%s", out)
	}
}

// F-0476 crit. 11: cerne update is the only place that asks for a new
// version — no other code reaches the releases endpoint or selfupdate. F-0480
// adds the panel's release check (internal/server/uiupdate.go), behind
// ui.update_check; F-0479 adds the session-start notice (internal/cli/updatenotify.go),
// behind update.notify.
func TestOnlyUpdateChecksForVersions(t *testing.T) {
	root := filepath.Join("..", "..")
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == "node_modules" || d.Name() == ".git" || d.Name() == "dist") {
			return filepath.SkipDir
		}
		slash := filepath.ToSlash(p)
		// The binary's own code, its hook scripts and the UI; the install
		// scripts are the installer, not cerne.
		code := strings.HasSuffix(p, ".go") || (strings.Contains(slash, "/internal/") && strings.HasSuffix(p, ".sh")) ||
			(strings.Contains(slash, "/ui/src/") && (strings.HasSuffix(p, ".ts") || strings.HasSuffix(p, ".tsx")))
		if d.IsDir() || !code || strings.HasSuffix(p, "_test.go") || strings.Contains(p, ".test.") {
			return nil
		}
		if strings.Contains(slash, "internal/selfupdate/") || strings.HasSuffix(slash, "internal/cli/updatecmd.go") ||
			strings.HasSuffix(slash, "internal/server/uiupdate.go") || strings.HasSuffix(slash, "internal/cli/updatenotify.go") {
			return nil
		}
		b, _ := os.ReadFile(p)
		for _, s := range []string{"releases/latest", "releases.atom", "internal/selfupdate", "api.github.com/repos/henriquesakata/cerne", "github.com/henriquesakata/cerne/tags"} {
			if strings.Contains(string(b), s) {
				t.Errorf("%s references %q: only cerne update may check for a new version", p, s)
			}
		}
		return nil
	})
}

// F-0476 crit. 4: cerne next opens with the update line while the project's
// hooks or guide are older than the binary, and says nothing once they match.
func TestNextTellsToRunUpdate(t *testing.T) {
	projDir := staleProject(t)
	out, err := execCerne(t, "next", "--dir", projDir)
	if err != nil {
		t.Fatalf("next: %v\n%s", err, out)
	}
	if first := strings.SplitN(out, "\n", 2)[0]; !strings.Contains(first, "run 'cerne update'") {
		t.Errorf("first line = %q, want the update line on top\n%s", first, out)
	}
	if _, err := execCerne(t, "update", "--files-only", "--dir", projDir); err != nil {
		t.Fatal(err)
	}
	out, _ = execCerne(t, "next", "--dir", projDir)
	if strings.Contains(out, "cerne update") {
		t.Errorf("update line with everything current:\n%s", out)
	}
}

// F-0476 crit. 5: the Copilot session-start notice sends to cerne update too.
func TestCopilotStaleNoticeSendsToUpdate(t *testing.T) {
	proj := t.TempDir()
	if err := os.MkdirAll(filepath.Join(proj, ".github", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := skills.Install(proj, hooks.ProviderCopilot); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(proj, ".github", "skills", "cerne-authoring", "SKILL.md"), "old")
	out := copilotStaleNotice(proj)
	if !strings.Contains(out, "run `cerne update`") || strings.Contains(out, "install") {
		t.Fatalf("copilot notice = %q", out)
	}
}

// Critic finding (F-0476 crit. 2): a .cernerc that does not load still runs
// the binary part and closes with the project parts not done.
func TestUpdateBrokenCernercStillCloses(t *testing.T) {
	projDir, _, _ := setupProject(t)
	currentBinary(t)
	mustWrite(t, filepath.Join(projDir, ".cernerc"), "not = [valid toml")
	t.Chdir(projDir)
	out, err := execCerne(t, "update")
	if err == nil {
		t.Fatalf("nil error with a broken .cernerc\n%s", out)
	}
	for _, want := range []string{"binary: up to date", "hooks: not done — the project's .cernerc does not load", "guide: not done — "} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

// Critic finding (F-0476 crit. 8): a new binary that fails makes the update
// fail, naming it.
func TestUpdateFailingNewBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake binary is a shell script")
	}
	projDir, _, _ := setupProject(t)
	installedBinary(t, "v0.14.3")
	fakeRelease(t, "v0.15.0", []byte("#!/bin/sh\nexit 3\n"))
	out, err := execCerne(t, "update", "--dir", projDir)
	if err == nil || !strings.Contains(err.Error(), "the new binary") || !strings.Contains(err.Error(), "exit status 3") {
		t.Fatalf("err = %v, want the failing new binary named\n%s", err, out)
	}
}
