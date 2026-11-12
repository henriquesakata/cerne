package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/harness"
	"github.com/henriquesakata/cerne/internal/hooks"
)

// cerne harness --json returns the same inventory shape the endpoint serves
// (F-0251 crit. 14); the table form lists the classes with states.
func TestHarnessCommand(t *testing.T) {
	projDir, _, _ := setupProject(t)
	t.Setenv("HOME", t.TempDir()) // fixtures never read the developer's real ~
	if _, err := hooks.Install(projDir); err != nil {
		t.Fatal(err)
	}

	out, err := execCerne(t, "harness", "--json", "--dir", projDir)
	if err != nil {
		t.Fatalf("harness --json: %v\noutput: %s", err, out)
	}
	var inv harness.Inventory
	if err := json.Unmarshal([]byte(out), &inv); err != nil {
		t.Fatalf("output is not the inventory JSON: %v\n%s", err, out)
	}
	var sawHooks bool
	for _, c := range inv.Classes {
		if c.Name == "hooks" && len(c.Items) > 0 {
			sawHooks = true
		}
	}
	if !sawHooks {
		t.Errorf("inventory without hooks items:\n%s", out)
	}

	out, err = execCerne(t, "harness", "--dir", projDir)
	if err != nil {
		t.Fatalf("harness: %v\noutput: %s", err, out)
	}
	for _, want := range []string{"harness inventory", "hooks", "cerne-register-reminder.sh", "ok"} {
		if !strings.Contains(out, want) {
			t.Errorf("table output missing %q:\n%s", want, out)
		}
	}
}

// TestHarnessHuskyLayout (F-0360 crit. 12): in a repo with husky 9's layout
// (core.hooksPath=.husky/_, runner and stubs in .husky/_/, the project's hooks
// in .husky/<slot>), the harness gives ok to both git slots when those files
// call cerne, and missing without a Fix when they do not.
func TestHarnessHuskyLayout(t *testing.T) {
	projDir, _, _ := setupProject(t)
	t.Setenv("HOME", t.TempDir())
	initGitRepo(t, projDir, "main")
	if out, err := exec.Command("git", "-C", projDir, "config", "core.hooksPath", ".husky/_").CombinedOutput(); err != nil {
		t.Fatalf("git config: %v\n%s", err, out)
	}
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(projDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write(".husky/_/h", "#!/usr/bin/env sh\n[ \"${HUSKY-}\" = \"0\" ] && exit 0\nn=$(basename \"$0\")\ns=$(dirname \"$(dirname \"$0\")\")/$n\n[ ! -f \"$s\" ] && exit 0\nsh -e \"$s\" \"$@\"\n")
	write(".husky/_/commit-msg", "#!/usr/bin/env sh\n. \"$(dirname \"$0\")/h\"\n")
	write(".husky/_/prepare-commit-msg", "#!/usr/bin/env sh\n. \"$(dirname \"$0\")/h\"\n")
	write(".husky/commit-msg", "npx --no -- commitlint --edit \"$1\"\ncerne guard commit-msg \"$1\"\n")
	write(".husky/prepare-commit-msg", "cerne hook commit-trailer \"$1\"\n")

	slots := func() map[string]harness.Item {
		t.Helper()
		if out, err := execCerne(t, "hook", "install", "--dir", projDir); err != nil {
			t.Fatalf("hook install: %v\noutput: %s", err, out)
		}
		out, err := execCerne(t, "harness", "--json", "--dir", projDir)
		if err != nil {
			t.Fatalf("harness --json: %v\noutput: %s", err, out)
		}
		var inv harness.Inventory
		if err := json.Unmarshal([]byte(out), &inv); err != nil {
			t.Fatalf("output is not the inventory JSON: %v\n%s", err, out)
		}
		got := map[string]harness.Item{}
		for _, c := range inv.Classes {
			for _, it := range c.Items {
				if c.Name == "hooks" && (it.Name == "commit-msg" || it.Name == "prepare-commit-msg") {
					got[it.Name] = it
				}
			}
		}
		return got
	}

	for name, it := range slots() {
		if it.State != harness.StateOK || it.Path != filepath.Join(".husky", name) || it.Fix != "" {
			t.Errorf("%s with the invocation = %+v, want ok at .husky/%s", name, it, name)
		}
	}

	write(".husky/commit-msg", "npx --no -- commitlint --edit \"$1\"\n")
	if err := os.Remove(filepath.Join(projDir, ".husky", "prepare-commit-msg")); err != nil {
		t.Fatal(err)
	}
	got := slots()
	if len(got) != 2 {
		t.Fatalf("want both git slots, got %+v", got)
	}
	for name, it := range got {
		if it.State != harness.StateMissing || it.Fix != "" || !strings.Contains(it.Detail, "core.hooksPath=.husky/_") {
			t.Errorf("%s without the invocation = %+v, want missing, no Fix, the snippet", name, it)
		}
	}
}
