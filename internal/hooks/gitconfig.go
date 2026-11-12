package hooks

import (
	"os"
	"path/filepath"
	"strings"
)

// GitHooksPath reports core.hooksPath from the project's .git/config [core]
// section (key case-insensitive, last value wins, comments ignored), and
// whether it is set. Install and the harness both decide by it (F-0360).
// ponytail: only .git/config — no global config, include or worktree .git file.
func GitHooksPath(projDir string) (string, bool) {
	raw, err := os.ReadFile(filepath.Join(projDir, ".git", "config"))
	if err != nil {
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
			name := strings.TrimSpace(strings.Trim(line, "[]"))
			inCore = strings.EqualFold(name, "core")
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
