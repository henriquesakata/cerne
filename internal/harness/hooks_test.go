package harness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/hooks"
)

// findItem returns the first item of class whose name matches.
func findItem(inv Inventory, class, name string) (Item, bool) {
	for _, c := range inv.Classes {
		if c.Name != class {
			continue
		}
		for _, it := range c.Items {
			if it.Name == name {
				return it, true
			}
		}
	}
	return Item{}, false
}

func TestManagedHookCapabilityStates(t *testing.T) {
	proj := t.TempDir()
	if _, err := hooks.Install(proj); err != nil {
		t.Fatal(err)
	}
	first := hooks.Assets()[0].Script

	inv := Inspect(Options{ProjDir: proj})
	it, ok := findItem(inv, "hooks", first)
	if !ok {
		t.Fatalf("managed capability %s absent from inventory", first)
	}
	if it.State != StateOK {
		t.Errorf("installed capability = %s (%s), want ok", it.State, it.Detail)
	}
	if it.OutsideRepo {
		t.Error("project hook marked outside repo")
	}

	// One flipped byte → divergent, with the repair command.
	path := filepath.Join(proj, ".claude", "hooks", first)
	raw, _ := os.ReadFile(path)
	raw[0] ^= 1
	os.WriteFile(path, raw, 0o644)
	inv = Inspect(Options{ProjDir: proj})
	it, _ = findItem(inv, "hooks", first)
	if it.State != StateDivergent || it.Fix != "cerne hook install" {
		t.Errorf("divergent capability = %s fix=%q, want divergent + cerne hook install", it.State, it.Fix)
	}
	// F-0360 crit. 7: the detail says the install drops local lines and names
	// the switch that turns the hook off without editing it.
	const wantDetail = "script differs from the canonical asset of this cerne build — cerne hook install replaces it and drops local lines; to turn the hook off for a process, set CERNE_NO_AGENT_HOOKS=1"
	if it.Detail != wantDetail {
		t.Errorf("divergent detail = %q, want %q", it.Detail, wantDetail)
	}

	// Script gone → capability missing even though wiring stays.
	os.Remove(path)
	inv = Inspect(Options{ProjDir: proj})
	it, _ = findItem(inv, "hooks", first)
	if it.State != StateMissing {
		t.Errorf("script removed = %s, want missing", it.State)
	}
}

func TestWiringHalfMissing(t *testing.T) {
	proj := t.TempDir()
	if _, err := hooks.Install(proj); err != nil {
		t.Fatal(err)
	}
	// Script on disk, wiring gone: capability is missing, detail names wiring.
	if err := os.Remove(filepath.Join(proj, ".claude", "settings.json")); err != nil {
		t.Fatal(err)
	}
	inv := Inspect(Options{ProjDir: proj})
	it, ok := findItem(inv, "hooks", hooks.Assets()[0].Script)
	if !ok {
		t.Fatal("capability absent")
	}
	if it.State != StateMissing {
		t.Errorf("wiring removed = %s, want missing", it.State)
	}
	if it.Detail == "" {
		t.Error("missing wiring reported without detail naming the half")
	}
}

func TestExternalBinaryBrokenAndConfigured(t *testing.T) {
	proj := t.TempDir()
	writeFile(t, filepath.Join(proj, ".claude", "settings.json"),
		`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"rtk hook-rewrite"}]}]}}`)

	empty := t.TempDir()
	t.Setenv("PATH", empty)
	inv := Inspect(Options{ProjDir: proj})
	it, ok := findItem(inv, "hooks", "rtk")
	if !ok {
		t.Fatal("external hook binary absent from inventory")
	}
	if it.State != StateBroken {
		t.Errorf("unresolvable binary = %s, want broken", it.State)
	}
	if !strings.Contains(it.Detail, "rtk") {
		t.Errorf("detail %q does not name the missing binary", it.Detail)
	}

	// Stub on PATH → configured.
	stub := filepath.Join(empty, "rtk")
	os.WriteFile(stub, []byte("#!/bin/sh\n"), 0o755)
	inv = Inspect(Options{ProjDir: proj})
	it, _ = findItem(inv, "hooks", "rtk")
	if it.State != StateConfigured {
		t.Errorf("resolvable binary = %s, want configured", it.State)
	}
}

func TestProjectScriptWiringNotDuplicated(t *testing.T) {
	proj := t.TempDir()
	writeFile(t, filepath.Join(proj, ".claude", "hooks", "meu-hook.sh"), "#!/bin/sh\nexit 0\n")
	writeFile(t, filepath.Join(proj, ".claude", "settings.json"),
		`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"bash \"$CLAUDE_PROJECT_DIR/.claude/hooks/meu-hook.sh\""}]}]}}`)

	inv := Inspect(Options{ProjDir: proj})
	var count int
	for _, c := range inv.Classes {
		if c.Name != "hooks" {
			continue
		}
		for _, it := range c.Items {
			if it.Name == "meu-hook.sh" {
				count++
			}
		}
	}
	if count != 1 {
		t.Errorf("project script wired + on disk listed %d times, want 1", count)
	}

	// Wiring to a project script NOT on disk must still surface, as broken.
	writeFile(t, filepath.Join(proj, ".claude", "settings.json"),
		`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"bash \"$CLAUDE_PROJECT_DIR/.claude/hooks/sumiu.sh\""}]}]}}`)
	inv = Inspect(Options{ProjDir: proj})
	it, ok := findItem(inv, "hooks", "sumiu.sh")
	if !ok || it.State != StateBroken {
		t.Errorf("wired but absent project script = %+v, want broken", it)
	}
}

func TestUnmanagedHookScript(t *testing.T) {
	proj := t.TempDir()
	writeFile(t, filepath.Join(proj, ".claude", "hooks", "no-new-case-dir.sh"), "#!/bin/sh\nexit 0\n")
	inv := Inspect(Options{ProjDir: proj})
	it, ok := findItem(inv, "hooks", "no-new-case-dir.sh")
	if !ok {
		t.Fatal("unmanaged hook script absent from inventory")
	}
	if it.State != StateUnmanaged {
		t.Errorf("manual hook = %s, want unmanaged", it.State)
	}
	if it.Fix != "" {
		t.Errorf("unmanaged item suggests fix %q, want none", it.Fix)
	}
}

func TestUserOriginHookOutsideRepo(t *testing.T) {
	proj := t.TempDir()
	home := t.TempDir()
	writeFile(t, filepath.Join(home, ".claude", "settings.json"),
		`{"hooks":{"PostToolUse":[{"hooks":[{"type":"command","command":"mytool observe"}]}]}}`)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "mytool"), []byte("#!/bin/sh\n"), 0o755)
	t.Setenv("PATH", dir)

	inv := Inspect(Options{ProjDir: proj, Home: home})
	it, ok := findItem(inv, "hooks", "mytool")
	if !ok {
		t.Fatal("user-layer hook absent")
	}
	if it.Origin != OriginUser || !it.OutsideRepo {
		t.Errorf("user hook origin=%s outside=%v, want user/true", it.Origin, it.OutsideRepo)
	}
}

// TestGitSlotsFollowHooksPath (F-0360 crit. 9/10/11): with core.hooksPath in
// [core], each git slot is read from the hook manager's directory (and, for
// husky 9's `.husky/_`, from `.husky/<slot>`), ok when the cerne invocation is
// there, missing with the snippet and no Fix otherwise. Without it, or with it
// only in a comment, the slots stay in .git/hooks.
func TestGitSlotsFollowHooksPath(t *testing.T) {
	const (
		guardInv   = "cerne guard commit-msg \"$1\"\n"
		trailerInv = "cerne hook commit-trailer \"$1\"\n"
	)
	type want struct{ state, path, detail, fix string }
	cases := []struct {
		name   string
		config func(proj string) string
		files  map[string]string
		want   map[string]want
	}{
		{
			name:   "relative dir with invocations",
			config: func(string) string { return "[core]\n\thooksPath = githooks\n" },
			files:  map[string]string{"githooks/commit-msg": guardInv, "githooks/prepare-commit-msg": trailerInv},
			want: map[string]want{
				"commit-msg":         {StateOK, filepath.Join("githooks", "commit-msg"), "", ""},
				"prepare-commit-msg": {StateOK, filepath.Join("githooks", "prepare-commit-msg"), "", ""},
			},
		},
		{
			name:   "husky layout, absolute path",
			config: func(proj string) string { return "[Core]\n\tHooksPath = " + filepath.Join(proj, ".husky", "_") + "\n" },
			files: map[string]string{
				".husky/_/commit-msg": ". \"$(dirname \"$0\")/h\"\n", ".husky/_/prepare-commit-msg": ". \"$(dirname \"$0\")/h\"\n",
				".husky/commit-msg": "npx commitlint --edit \"$1\"\n" + guardInv, ".husky/prepare-commit-msg": trailerInv,
			},
			want: map[string]want{
				"commit-msg":         {StateOK, filepath.Join(".husky", "commit-msg"), "", ""},
				"prepare-commit-msg": {StateOK, filepath.Join(".husky", "prepare-commit-msg"), "", ""},
			},
		},
		{
			name:   "husky layout without the invocation, or absent",
			config: func(string) string { return "[core]\n\thooksPath = .husky/_\n" },
			files:  map[string]string{".husky/commit-msg": "npx commitlint --edit \"$1\"\n"},
			want: map[string]want{
				"commit-msg":         {StateMissing, filepath.Join(".husky", "commit-msg"), "core.hooksPath=.husky/_ — add to your hook manager: " + strings.TrimSpace(guardInv), ""},
				"prepare-commit-msg": {StateMissing, filepath.Join(".husky", "_", "prepare-commit-msg"), "core.hooksPath=.husky/_ — add to your hook manager: " + strings.TrimSpace(trailerInv), ""},
			},
		},
		{
			name: "absolute path through a symlink of the project dir",
			config: func(proj string) string {
				real, err := filepath.EvalSymlinks(proj)
				if err != nil {
					real = proj
				}
				return "[core]\n\thooksPath = " + filepath.Join(real, ".husky", "_") + "\n"
			},
			files: map[string]string{".husky/commit-msg": guardInv, ".husky/prepare-commit-msg": trailerInv},
			want: map[string]want{
				"commit-msg":         {StateOK, filepath.Join(".husky", "commit-msg"), "", ""},
				"prepare-commit-msg": {StateOK, filepath.Join(".husky", "prepare-commit-msg"), "", ""},
			},
		},
		{
			name:   "a dir merely ending in .husky/_ is not husky",
			config: func(string) string { return "[core]\n\thooksPath = foo.husky/_\n" },
			files:  map[string]string{"foo.husky/commit-msg": guardInv},
			want: map[string]want{
				"commit-msg": {StateMissing, filepath.Join("foo.husky", "_", "commit-msg"), "core.hooksPath=foo.husky/_ — add to your hook manager: " + strings.TrimSpace(guardInv), ""},
			},
		},
		{
			name:   "invocation only in a comment",
			config: func(string) string { return "[core]\n\thooksPath = githooks\n" },
			files:  map[string]string{"githooks/commit-msg": "# " + guardInv, "githooks/prepare-commit-msg": "  #" + trailerInv},
			want: map[string]want{
				"commit-msg":         {StateMissing, filepath.Join("githooks", "commit-msg"), "core.hooksPath=githooks — add to your hook manager: " + strings.TrimSpace(guardInv), ""},
				"prepare-commit-msg": {StateMissing, filepath.Join("githooks", "prepare-commit-msg"), "core.hooksPath=githooks — add to your hook manager: " + strings.TrimSpace(trailerInv), ""},
			},
		},
		{
			name:   "hooksPath only in a comment",
			config: func(string) string { return "[core]\n\tbare = false\n# hooksPath = .husky/_\n" },
			files:  map[string]string{".husky/commit-msg": guardInv},
			want: map[string]want{
				"commit-msg":         {StateMissing, filepath.Join(".git", "hooks", "commit-msg"), "", "cerne hook install"},
				"prepare-commit-msg": {StateMissing, filepath.Join(".git", "hooks", "prepare-commit-msg"), "", "cerne hook install"},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			proj := t.TempDir()
			t.Setenv("HOME", t.TempDir())
			if err := os.MkdirAll(filepath.Join(proj, ".git", "hooks"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(proj, ".git", "config"), []byte(tc.config(proj)), 0o644); err != nil {
				t.Fatal(err)
			}
			for rel, body := range tc.files {
				p := filepath.Join(proj, filepath.FromSlash(rel))
				if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			inv := Inspect(Options{ProjDir: proj})
			for slot, w := range tc.want {
				it, ok := findItem(inv, "hooks", slot)
				if !ok {
					t.Fatalf("%s absent from inventory", slot)
				}
				if it.State != w.state || it.Path != w.path || it.Detail != w.detail || it.Fix != w.fix {
					t.Errorf("%s = {state %q path %q detail %q fix %q}, want %+v", slot, it.State, it.Path, it.Detail, it.Fix, w)
				}
			}
		})
	}
}
