package cli

import (
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/hooks"
	"github.com/henriquesakata/cerne/internal/refresh"
	"github.com/henriquesakata/cerne/internal/skills"
)

const v0143 = "../refresh/testdata/v0.14.3"

// oldProject is an initialized project whose agents are the ones cerne v0.14.3
// wrote (taken from that release, see internal/refresh/testdata): the current
// install is replaced by the fixture's folders listed in dirs.
func oldProject(t *testing.T, dirs ...string) string {
	t.Helper()
	projDir, _, _ := setupProject(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CERNE_CONFIG_DIR", t.TempDir())
	for _, d := range []string{".claude/hooks", ".claude/skills", ".claude/settings.json"} {
		if err := os.RemoveAll(filepath.Join(projDir, d)); err != nil {
			t.Fatal(err)
		}
	}
	for _, d := range dirs {
		copyFixtureTree(t, filepath.Join(v0143, d), filepath.Join(projDir, d))
	}
	return projDir
}

func copyFixtureTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(p string, e os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if e.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dst, rel)), 0o755); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func claudeOnly() []string {
	return []string{".claude/hooks", ".claude/skills", ".claude/settings.json"}
}

func countLines(out, sub string) int {
	n := 0
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, sub) {
			n++
		}
	}
	return n
}

// Crits. 1, 3 and 4: after a cerne next, an untouched v0.14.3 project is
// current, the line comes once and the next call is silent.
func TestAutoRefreshNextRenewsOnce(t *testing.T) {
	projDir := oldProject(t, append(claudeOnly(), ".cursor")...)
	out, err := execCerne(t, "next", "--dir", projDir)
	if err != nil {
		t.Fatalf("next: %v\n%s", err, out)
	}
	if h, g := hooks.Stale(projDir), skills.Stale(projDir); len(h)+len(g) > 0 {
		t.Fatalf("still stale after the refresh: hooks %v, guide %v", h, g)
	}
	if n := countLines(out, "cerne: renewed"); n != 1 {
		t.Fatalf("%d renewed lines, want one:\n%s", n, out)
	}
	for _, want := range []string{"claude", "cursor", "from the next session"} {
		if !strings.Contains(out, want) {
			t.Errorf("line misses %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "run 'cerne update'") {
		t.Errorf("the update line stays with nothing left:\n%s", out)
	}
	for _, p := range refresh.Connected(projDir) {
		if v := refresh.Classify(projDir, p); v.State != refresh.UpToDate {
			t.Errorf("%s not up to date after the refresh: %+v", p, v)
		}
	}
	out, _ = execCerne(t, "next", "--dir", projDir)
	if strings.Contains(out, "renewed") || strings.Contains(out, "cerne update") {
		t.Errorf("the second next says something:\n%s", out)
	}
}

// Crits. 2 and 3: an edited hook leaves the whole agent as it was, and the
// session-start notice names the item and why.
func TestAutoRefreshEditedHookLeavesTheAgentUntouched(t *testing.T) {
	projDir := oldProject(t, claudeOnly()...)
	edited := filepath.Join(projDir, ".claude", "hooks", "cerne-work-gate.sh")
	appendTo(t, edited, "\n# mine\n")
	before := refresh.Collect(projDir)

	notice := staleManagedNotice(projDir, false)
	after := refresh.Collect(projDir)
	for slot, sha := range before {
		if after[slot] != sha {
			t.Errorf("%s was rewritten although the agent has an edit", slot)
		}
	}
	for _, want := range []string{".claude/hooks/cerne-work-gate.sh", "edited in the project or written by a newer cerne", "cerne update"} {
		if !strings.Contains(notice, want) {
			t.Errorf("notice misses %q:\n%s", want, notice)
		}
	}
	if strings.Contains(notice, "renewed") {
		t.Errorf("notice claims a renewal:\n%s", notice)
	}
}

// The session notice of a renewable project says what it did, once, and no
// stale warning remains.
func TestAutoRefreshSessionNoticeShowsOnlyWhatIsLeft(t *testing.T) {
	projDir := oldProject(t, claudeOnly()...)
	notice := staleManagedNotice(projDir, false)
	if countLines(notice, "cerne: renewed claude") != 1 || strings.Contains(notice, "out of date") {
		t.Fatalf("notice = %q, want the renewal line alone", notice)
	}
	if again := staleManagedNotice(projDir, false); again != "" {
		t.Fatalf("second notice = %q, want nothing", again)
	}
}

// Crit. 5: Codex asks to review changed hooks again, and the line says so.
func TestAutoRefreshCodexLine(t *testing.T) {
	projDir := oldProject(t, append(claudeOnly(), ".codex", ".agents", "AGENTS.md")...)
	out, err := execCerne(t, "next", "--dir", projDir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Codex asks to review changed hooks again (/hooks)") {
		t.Fatalf("no Codex review line:\n%s", out)
	}
	claude := oldProject(t, claudeOnly()...)
	if out, _ := execCerne(t, "next", "--dir", claude); strings.Contains(out, "Codex") {
		t.Fatalf("Codex line without Codex:\n%s", out)
	}
}

type noNetwork struct{ hits *int }

func (n noNetwork) RoundTrip(*http.Request) (*http.Response, error) {
	*n.hits++
	return nil, errors.New("no network in the automatic refresh")
}

// Crit. 7: no connection is opened, no binary is swapped and no agent that was
// not connected gets connected.
func TestAutoRefreshUsesNoNetworkAndConnectsNothing(t *testing.T) {
	projDir := oldProject(t, claudeOnly()...)
	hits := 0
	prev := http.DefaultTransport
	http.DefaultTransport = noNetwork{&hits}
	t.Cleanup(func() { http.DefaultTransport = prev })
	exe := installedBinary(t, "v1.0.0")
	if _, err := execCerne(t, "next", "--dir", projDir); err != nil {
		t.Fatal(err)
	}
	staleManagedNotice(projDir, false)
	if hits != 0 {
		t.Fatalf("%d network attempts", hits)
	}
	if b, _ := os.ReadFile(exe); string(b) != "old binary" {
		t.Fatal("the binary was touched")
	}
	for _, d := range []string{".cursor", ".codex", ".gemini", ".github"} {
		if _, err := os.Stat(filepath.Join(projDir, d)); err == nil {
			t.Errorf("the refresh connected an agent: %s exists", d)
		}
	}
}

// Crit. 6: hooks.auto_refresh = false, in either scope, leaves only the warning.
func TestAutoRefreshOffInEitherScope(t *testing.T) {
	for _, scope := range []string{"project", "host"} {
		t.Run(scope, func(t *testing.T) {
			projDir := oldProject(t, claudeOnly()...)
			if out, err := execCerne(t, "settings", "set", "hooks.auto_refresh", "false", "--scope", scope, "--dir", projDir); err != nil {
				t.Fatalf("settings set: %v\n%s", err, out)
			}
			out, _ := execCerne(t, "next", "--dir", projDir)
			if strings.Contains(out, "renewed") || len(hooks.Stale(projDir)) == 0 {
				t.Fatalf("refresh ran with the key off:\n%s", out)
			}
			if !strings.Contains(out, "run 'cerne update'") {
				t.Errorf("the warning is gone with the key off:\n%s", out)
			}
			if n := staleManagedNotice(projDir, false); !strings.Contains(n, "cerne update") || strings.Contains(n, "renewed") {
				t.Errorf("session notice = %q", n)
			}
		})
	}
}

// Crit. 9: what the automatic refresh leaves, cerne update replaces on command.
func TestUpdateReplacesWhatTheAutomaticRefreshLeft(t *testing.T) {
	projDir := oldProject(t, claudeOnly()...)
	edited := filepath.Join(projDir, ".claude", "hooks", "cerne-work-gate.sh")
	appendTo(t, edited, "\n# mine\n")
	if _, err := execCerne(t, "next", "--dir", projDir); err != nil {
		t.Fatal(err)
	}
	if len(hooks.Stale(projDir)) == 0 {
		t.Fatal("the refresh replaced an edited hook")
	}
	if out, err := execCerne(t, "update", "--files-only", "--dir", projDir); err != nil {
		t.Fatalf("update: %v\n%s", err, out)
	}
	if s := hooks.Stale(projDir); len(s) > 0 {
		t.Fatalf("update left %v", s)
	}
}

// Crit. 10: sync_skills = true still rewrites the guide, edited or not.
func TestAutoRefreshKeepsSyncSkills(t *testing.T) {
	projDir := oldProject(t, claudeOnly()...)
	appendTo(t, filepath.Join(projDir, ".claude", "hooks", "cerne-work-gate.sh"), "\n# mine\n") // agent unrecognized
	appendTo(t, filepath.Join(projDir, ".claude", "skills", "cerne-authoring", "SKILL.md"), "\nedited\n")
	out := staleManagedNotice(projDir, true)
	if !strings.Contains(out, "updated canonical skill") {
		t.Fatalf("sync_skills no longer rewrites the guide:\n%s", out)
	}
	if s := skills.Stale(projDir); len(s) > 0 {
		t.Fatalf("guide still stale: %v", s)
	}
}

// Blocker: the refresh at session start rewrites the very script the shell is
// running. Through the real reminder script and a real binary, the session
// start must exit 0 and say what it renewed.
func TestAutoRefreshWhileTheReminderScriptRuns(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary")
	}
	bin := filepath.Join(t.TempDir(), "cerne")
	if out, err := exec.Command("go", "build", "-o", bin, "../../cmd/cerne").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	projDir := oldProject(t, claudeOnly()...)
	script := filepath.Join(projDir, ".claude", "hooks", "cerne-register-reminder.sh")
	cmd := exec.Command("bash", script)
	cmd.Stdin = strings.NewReader(`{"cwd":"` + projDir + `","session_id":"s1"}`)
	cmd.Env = append(os.Environ(), "PATH="+filepath.Dir(bin)+string(os.PathListSeparator)+os.Getenv("PATH"), "CLAUDE_PROJECT_DIR="+projDir, "HOME="+t.TempDir())
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the session-start script failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "cerne: renewed claude") {
		t.Fatalf("no renewal line:\n%s", out)
	}
}
