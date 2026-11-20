package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/hooks"
	"github.com/henriquesakata/cerne/internal/project"
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
		if v := refresh.Classify(projDir, p, true); v.State != refresh.UpToDate {
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

// snapshot reads every file under dir/rel (or the file itself) as bytes.
func snapshot(t *testing.T, dir string, rels ...string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, rel := range rels {
		_ = filepath.WalkDir(filepath.Join(dir, rel), func(p string, e os.DirEntry, err error) error {
			if err == nil && !e.IsDir() {
				b, _ := os.ReadFile(p)
				r, _ := filepath.Rel(dir, p)
				out[r] = string(b)
			}
			return nil
		})
	}
	return out
}

// C1: an edit in Claude's script leaves every Claude byte as it was, while the
// other agents of the same project are renewed.
func TestAutoRefreshEditedClaudeLeavesClaudeAloneAndRenewsTheOthers(t *testing.T) {
	projDir := oldProject(t, append(claudeOnly(), ".cursor", ".codex", ".agents", "AGENTS.md")...)
	appendTo(t, filepath.Join(projDir, ".claude", "hooks", "cerne-work-gate.sh"), "\n# mine\n")
	before := snapshot(t, projDir, ".claude")
	cursorBefore := snapshot(t, projDir, ".cursor", ".codex", ".agents")
	out, err := execCerne(t, "next", "--dir", projDir)
	if err != nil {
		t.Fatal(err)
	}
	for path, content := range snapshot(t, projDir, ".claude") {
		if before[path] != content {
			t.Errorf("%s changed although Claude has an edit", path)
		}
	}
	if len(snapshot(t, projDir, ".claude")) != len(before) {
		t.Error("a Claude file was created or removed")
	}
	if !strings.Contains(out, "renewed cursor, codex") {
		t.Errorf("Cursor and Codex were not renewed:\n%s", out)
	}
	for _, p := range []hooks.Provider{hooks.ProviderCursor, hooks.ProviderCodex} {
		if v := refresh.Classify(projDir, p, true); v.State == refresh.Unrecognized {
			t.Errorf("%s: %+v", p, v)
		}
	}
	after := snapshot(t, projDir, ".cursor", ".codex", ".agents")
	changed := false
	for k, v := range after {
		changed = changed || cursorBefore[k] != v
	}
	if !changed {
		t.Error("nothing of Cursor or Codex changed")
	}
}

// C6: a second run prints nothing even for the agents whose old cerne entries
// the append-only merge cannot replace (an old fail-open body, an old .mcp.json
// server), and no entry is doubled (C8).
func TestAutoRefreshSecondRunIsSilentAndNothingIsDoubled(t *testing.T) {
	projDir := oldProject(t, append(claudeOnly(), ".cursor", ".codex", ".gemini", ".github", ".mcp.json")...)
	if out, err := execCerne(t, "next", "--dir", projDir); err != nil || !strings.Contains(out, "cerne: renewed") {
		t.Fatalf("first run: %v\n%s", err, out)
	}
	for i := 0; i < 2; i++ {
		out, _ := execCerne(t, "next", "--dir", projDir)
		if strings.Contains(out, "renewed") {
			t.Fatalf("run %d prints a renewal again:\n%s", i+2, out)
		}
		if n := staleManagedNotice(projDir, false); strings.Contains(n, "renewed") {
			t.Fatalf("session notice %d renews again: %s", i+2, n)
		}
	}
	for _, f := range []string{".claude/settings.json", ".codex/hooks.json", ".gemini/settings.json", ".cursor/hooks.json"} {
		seen := map[string]int{}
		raw, err := os.ReadFile(filepath.Join(projDir, f))
		if err != nil {
			t.Fatal(err)
		}
		var doc struct {
			Hooks map[string][]map[string]any `json:"hooks"`
		}
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatal(err)
		}
		for event, groups := range doc.Hooks {
			for _, g := range groups {
				leaves, _ := g["hooks"].([]any)
				if leaves == nil {
					leaves = []any{g}
				}
				for _, l := range leaves {
					cmd, _ := l.(map[string]any)["command"].(string)
					core := cmd
					if i := strings.Index(cmd, "cerne hook"); i >= 0 {
						core = strings.SplitN(cmd[i:], " 2>", 2)[0]
					}
					seen[event+"|"+fmt.Sprint(g["matcher"])+"|"+core]++
				}
			}
		}
		for k, n := range seen {
			if n > 1 {
				t.Errorf("%s: %d copies of %s", f, n, k)
			}
		}
	}
}

// C2: a live lock held by another session skips the renewal silently; a dead
// one (older than 30 s) is taken over.
func TestAutoRefreshLock(t *testing.T) {
	projDir := oldProject(t, claudeOnly()...)
	cfg, _, err := project.LoadConfig(projDir)
	if err != nil {
		t.Fatal(err)
	}
	cache, err := project.CacheDir(cfg.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	lock := filepath.Join(cache, "refresh.lock")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, projDir, ".claude")
	if line, _ := autoRefresh(projDir); line != "" {
		t.Fatalf("renewed under another session's lock: %s", line)
	}
	for p, c := range snapshot(t, projDir, ".claude") {
		if before[p] != c {
			t.Fatalf("%s changed under another session's lock", p)
		}
	}
	old := time.Now().Add(-time.Minute)
	if err := os.Chtimes(lock, old, old); err != nil {
		t.Fatal(err)
	}
	if line, _ := autoRefresh(projDir); !strings.Contains(line, "renewed claude") {
		t.Fatalf("a dead lock was not taken over: %q", line)
	}
	if _, err := os.Stat(lock); err == nil {
		t.Error("the lock outlived the renewal")
	}
}

// Decision 2: a missing managed item is recreated, with its wiring.
func TestAutoRefreshRecreatesWhatIsMissing(t *testing.T) {
	projDir := oldProject(t, claudeOnly()...)
	script := filepath.Join(projDir, ".claude", "hooks", "cerne-tool-notes.sh")
	os.Remove(script) // not in v0.14.3 either way
	settings := filepath.Join(projDir, ".claude", "settings.json")
	raw, _ := os.ReadFile(settings)
	if strings.Contains(string(raw), "cerne-tool-notes.sh") {
		t.Fatal("the v0.14.3 fixture already wires the script")
	}
	if _, err := execCerne(t, "next", "--dir", projDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(script); err != nil {
		t.Fatalf("the missing script was not recreated: %v", err)
	}
	if raw, _ = os.ReadFile(settings); !strings.Contains(string(raw), "cerne-tool-notes.sh") {
		t.Fatal("the recreated script has no wiring in settings.json")
	}
}

// Decision 3: with sync_skills on, an edited guide neither blocks the hooks'
// renewal nor is looked at by the rule.
func TestAutoRefreshLeavesTheGuideToSyncSkills(t *testing.T) {
	projDir := oldProject(t, claudeOnly()...)
	setSyncSkills(t, projDir, true)
	appendTo(t, filepath.Join(projDir, ".claude", "skills", "cerne-authoring", "SKILL.md"), "\nedited\n")
	out := staleManagedNotice(projDir, true)
	if !strings.Contains(out, "updated canonical skill") || !strings.Contains(out, "cerne: renewed claude") {
		t.Fatalf("want the sync line and the renewal line:\n%s", out)
	}
	if h, g := hooks.Stale(projDir), skills.Stale(projDir); len(h)+len(g) > 0 {
		t.Fatalf("stale after both: %v %v", h, g)
	}
}
