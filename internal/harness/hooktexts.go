package harness

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/henriquesakata/cerne/internal/lint"
)

// ProjectHookTexts returns the text of every hook that belongs to the project
// (F-0340 crit. 6), for lint rules that read what the agent reads: the scripts
// of .claude/hooks/, each hook command of the project and local settings
// layers, and every project file those commands execute, with any interpreter.
// The user layer is not the project's and stays out.
func ProjectHookTexts(projDir string) ([]lint.HookText, error) {
	var out []lint.HookText
	seen := map[string]bool{}
	addFile := func(rel string) {
		rel = filepath.ToSlash(rel)
		if seen[rel] {
			return
		}
		seen[rel] = true
		if b, err := os.ReadFile(filepath.Join(projDir, filepath.FromSlash(rel))); err == nil {
			out = append(out, lint.HookText{Path: rel, Text: string(b)})
		}
	}

	if entries, err := os.ReadDir(filepath.Join(projDir, ".claude", "hooks")); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				addFile(".claude/hooks/" + e.Name())
			}
		}
	}

	ls := readLayers(projDir, "") // home "" = no user layer
	layerFile := map[string]string{OriginProject: ".claude/settings.json", OriginLocal: ".claude/settings.local.json"}
	idx := map[string]int{}
	var files []string
	for _, w := range ls.hooks {
		key := w.Origin + "#" + w.Event
		path := layerFile[w.Origin] + "#hooks." + w.Event + "[" + strconv.Itoa(idx[key]) + "]"
		idx[key]++
		out = append(out, lint.HookText{Path: path, Text: w.Command})
		files = append(files, commandProjectFiles(w.Command, projDir)...)
	}
	sort.Strings(files)
	for _, f := range files {
		addFile(f)
	}
	return out, nil
}

// commandProjectFiles lists the regular files inside projDir that a hook
// command executes, with any interpreter: each argument that, unquoted and
// with $CLAUDE_PROJECT_DIR replaced by the root (relative paths resolve
// against it), is a regular file inside the project. Paths come back relative
// to the root, slash-separated. It does not replace projectScript/resolveCommand.
func commandProjectFiles(cmd, projDir string) []string {
	var out []string
	for _, f := range strings.Fields(cmd) {
		f = strings.Trim(f, `"'`)
		f = strings.ReplaceAll(f, "${CLAUDE_PROJECT_DIR}", projDir)
		f = strings.ReplaceAll(f, "$CLAUDE_PROJECT_DIR", projDir)
		if f == "" {
			continue
		}
		if !filepath.IsAbs(f) {
			f = filepath.Join(projDir, f)
		}
		rel, err := filepath.Rel(projDir, filepath.Clean(f))
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			continue
		}
		if fi, err := os.Stat(f); err == nil && fi.Mode().IsRegular() {
			out = append(out, filepath.ToSlash(rel))
		}
	}
	return out
}
