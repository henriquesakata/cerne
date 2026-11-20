// Command knowncontents regenerates internal/refresh/known_contents.json: for
// every release tag it builds that release's binary, installs the agents into
// a throwaway project and records the hash of every managed item (F-0477).
// Run it from the repository root; a tag already in the file is not redone, and
// one that cannot be built or installed is recorded as skipped with the reason.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/henriquesakata/cerne/internal/refresh"
)

const listPath = "internal/refresh/known_contents.json"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "knowncontents:", err)
		os.Exit(1)
	}
}

func run() error {
	var list refresh.Contents
	if raw, err := os.ReadFile(listPath); err == nil {
		if err := json.Unmarshal(raw, &list); err != nil {
			return err
		}
	}
	if list.Tags == nil {
		list.Tags = map[string]string{}
	}
	if list.Known == nil {
		list.Known = map[string][]string{}
	}
	out, err := exec.Command("git", "tag", "-l", "v*").Output()
	if err != nil {
		return err
	}
	for _, tag := range strings.Fields(string(out)) {
		if _, done := list.Tags[tag]; done {
			continue
		}
		items, err := itemsOf(tag)
		if err != nil {
			list.Tags[tag] = err.Error()
			fmt.Fprintf(os.Stderr, "%s: skipped: %v\n", tag, err)
			continue
		}
		list.Tags[tag] = ""
		for slot, sha := range items {
			if !contains(list.Known[slot], sha) {
				list.Known[slot] = append(list.Known[slot], sha)
			}
		}
		fmt.Fprintf(os.Stderr, "%s: %d items\n", tag, len(items))
	}
	for _, shas := range list.Known {
		sort.Strings(shas)
	}
	raw, err := json.MarshalIndent(list, "", " ")
	if err != nil {
		return err
	}
	return os.WriteFile(listPath, append(raw, '\n'), 0o644)
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// itemsOf builds tag's binary and reads back what it installs.
func itemsOf(tag string) (map[string]string, error) {
	tmp, err := os.MkdirTemp("", "knowncontents-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	src, proj, home := filepath.Join(tmp, "src"), filepath.Join(tmp, "proj"), filepath.Join(tmp, "home")
	for _, d := range []string{src, proj, home} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, err
		}
	}
	// git archive: the tag's tree without touching this worktree.
	if out, err := sh("", nil, "sh", "-c", "git archive "+tag+" | tar -x -C "+src); err != nil {
		return nil, fmt.Errorf("cannot extract %s: %s", tag, out)
	}
	bin := filepath.Join(tmp, "cerne")
	if out, err := sh(src, nil, "go", "build", "-o", bin, "./cmd/cerne"); err != nil {
		return nil, fmt.Errorf("does not build: %s", tail(out))
	}
	env := []string{"HOME=" + home, "CERNE_CACHE_DIR=" + filepath.Join(tmp, "cache"), "CERNE_CONFIG_DIR=" + filepath.Join(tmp, "config"), "XDG_CONFIG_HOME=" + filepath.Join(tmp, "xdg")}
	for _, d := range []string{".cursor", ".codex", ".gemini", ".github/skills"} {
		if err := os.MkdirAll(filepath.Join(proj, d), 0o755); err != nil {
			return nil, err
		}
	}
	if out, err := sh(proj, env, "git", "init", "-q"); err != nil {
		return nil, fmt.Errorf("git init: %s", out)
	}
	// A project root the old installers accept: none of them reads the backend.
	if err := os.WriteFile(filepath.Join(proj, ".cernerc"), []byte("project_id = \"probe\"\nbackend = \""+filepath.Join(tmp, "backend.git")+"\"\n"), 0o644); err != nil {
		return nil, err
	}
	if out, err := sh(proj, env, bin, "hook", "install", "--dir", proj); err != nil {
		if strings.Contains(out, `unknown command "hook"`) {
			return map[string]string{}, nil // before v0.4.0 there were no hooks to hash
		}
		return nil, fmt.Errorf("hook install fails: %s", tail(out))
	}
	// An older release does not know every agent: its refusal means it wrote nothing for it.
	for _, p := range []string{"cursor", "codex", "gemini", "copilot"} {
		_, _ = sh(proj, env, bin, "hook", "install", "--dir", proj, "--provider", p)
	}
	_, _ = sh(proj, env, bin, "skill", "install", "--dir", proj) // no guide before the skill existed
	items := refresh.Collect(proj)
	if len(items) == 0 {
		return nil, fmt.Errorf("the install wrote no managed item")
	}
	return items, nil
}

func sh(dir string, env []string, name string, args ...string) (string, error) {
	c := exec.Command(name, args...)
	c.Dir = dir
	c.Env = append(os.Environ(), env...)
	out, err := c.CombinedOutput()
	return string(out), err
}

func tail(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 300 {
		s = s[len(s)-300:]
	}
	return s
}
