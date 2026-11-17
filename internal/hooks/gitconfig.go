package hooks

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// GitHooksPath reports core.hooksPath from the project's .git/config [core]
// section (key case-insensitive, last value wins, comments ignored), and
// whether it is set. Install and the harness both decide by it (F-0360).
// ponytail: only .git/config — no global config, include, worktree .git file,
// `~` expansion or escapes inside quotes; add them when a project needs one.
func GitHooksPath(projDir string) (string, bool) {
	raw, err := os.ReadFile(filepath.Join(projDir, ".git", "config"))
	if err != nil {
		// .git is a file (worktree/submodule): git reads the common config.
		if fi, serr := os.Stat(filepath.Join(projDir, ".git")); serr == nil && !fi.IsDir() {
			out, _ := exec.Command("git", "-C", projDir, "config", "--get", "core.hooksPath").Output()
			v := strings.TrimSpace(string(out))
			return v, v != ""
		}
		return "", false
	}
	var value string
	inCore := false
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line[0] == '#' || line[0] == ';' {
			continue
		}
		if line[0] == '[' {
			name, _, _ := strings.Cut(line[1:], "]") // `[core] # comment` is still [core]
			inCore = strings.EqualFold(strings.TrimSpace(name), "core")
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !inCore || !ok || !strings.EqualFold(strings.TrimSpace(key), "hooksPath") {
			continue
		}
		val = strings.TrimSpace(val)
		if strings.HasPrefix(val, `"`) {
			val, _, _ = strings.Cut(val[1:], `"`)
		} else if i := strings.IndexAny(val, "#;"); i >= 0 {
			val = strings.TrimSpace(val[:i])
		}
		value = val
	}
	return value, value != ""
}

// GitHooksDir returns the directory git runs hooks from for the repository at
// projDir (B-0013). A plain repo (.git is a directory) is answered without
// spawning git; a linked worktree or submodule (.git is a file) is resolved
// by `git rev-parse --git-path hooks`, which points at the common dir. The
// second result is false when projDir is not a git repository.
func GitHooksDir(projDir string) (string, bool) {
	fi, err := os.Stat(filepath.Join(projDir, ".git"))
	switch {
	case err != nil:
		return "", false
	case fi.IsDir():
		return filepath.Join(projDir, ".git", "hooks"), true
	}
	out, err := exec.Command("git", "-C", projDir, "rev-parse", "--git-path", "hooks").Output()
	dir := strings.TrimSpace(string(out))
	if err != nil || dir == "" {
		return "", false
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(projDir, dir)
	}
	return dir, true
}
