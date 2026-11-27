// Package refresh decides whether the hooks and the guide a project holds can
// be renewed without asking (F-0477): every managed item is hashed, and an
// agent whose items all match a release cerne once wrote is safe to renew.
package refresh

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/henriquesakata/cerne/internal/hooks"
)

// wholeFileDirs are the folders whose files cerne writes whole.
var wholeFileDirs = []string{
	".claude/hooks",
	".claude/skills/cerne-authoring",
	".claude/skills/cerne-self-learning",
	".cursor/rules",
	".cursor/skills/cerne-authoring",
	".cursor/skills/cerne-self-learning",
	".agents/skills/cerne-authoring",
	".gemini/skills/cerne-authoring",
	".github/hooks",
	".github/skills/cerne-authoring",
	".github/instructions",
}

// IsWholeFileDir reports whether rel, a slash path relative to the project, is
// one of those folders or lies inside one.
func IsWholeFileDir(rel string) bool {
	for _, d := range wholeFileDirs {
		if rel == d || strings.HasPrefix(rel, d+"/") {
			return true
		}
	}
	return false
}

// blockFiles are the JSON files that belong to the person, with cerne entries inside.
var blockFiles = []string{".claude/settings.json", ".codex/hooks.json", ".gemini/settings.json", ".cursor/hooks.json", ".mcp.json"}

// markerFiles are project docs carrying cerne's marked block.
var markerFiles = []string{"AGENTS.md", "GEMINI.md"}

// IsMarkerFile reports whether rel, a slash path relative to the project, is a
// doc carrying cerne's marked block.
func IsMarkerFile(rel string) bool { return slices.Contains(markerFiles, rel) }

const (
	markerBegin = "<!-- cerne:authoring-guide:begin"
	markerEnd   = "<!-- cerne:authoring-guide:end -->"
)

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// agentOf names the provider a slot belongs to.
func agentOf(slot string) hooks.Provider {
	path, _, _ := strings.Cut(slot, "#")
	switch {
	case strings.HasPrefix(path, ".cursor/"):
		return hooks.ProviderCursor
	case strings.HasPrefix(path, ".codex/"), strings.HasPrefix(path, ".agents/"), path == "AGENTS.md":
		return hooks.ProviderCodex
	case strings.HasPrefix(path, ".gemini/"), path == "GEMINI.md":
		return hooks.ProviderGemini
	case strings.HasPrefix(path, ".github/"), path == ".mcp.json":
		return hooks.ProviderCopilot
	}
	return hooks.ProviderClaude
}

// managedCommand says whether a hook command is one cerne wrote.
func managedCommand(c string) bool {
	return strings.Contains(c, "cerne hook") || strings.Contains(c, ".claude/hooks/cerne-") || strings.Contains(c, "register-reference-reminder.sh")
}

// canonicalTuples is the cerne part of a hook configuration: one tuple (event,
// matcher, command, timeout) per managed hook, plus the CERNE_ACTOR marker. It
// is the form the generator and the rule both read.
func canonicalTuples(raw []byte) []string {
	var root map[string]any
	if json.Unmarshal(raw, &root) != nil {
		return nil
	}
	var out []string
	add := func(parts ...any) {
		b, _ := json.Marshal(parts)
		out = append(out, string(b))
	}
	events, _ := root["hooks"].(map[string]any)
	for event, v := range events {
		groups, _ := v.([]any)
		for _, g := range groups {
			gm, _ := g.(map[string]any)
			matcher, _ := gm["matcher"].(string)
			leaves, nested := gm["hooks"].([]any)
			if !nested {
				leaves = []any{gm}
			}
			for _, l := range leaves {
				lm, _ := l.(map[string]any)
				cmd, _ := lm["command"].(string)
				if cmd == "" {
					cmd, _ = lm["bash"].(string)
				}
				if managedCommand(cmd) {
					add(event, matcher, cmd, lm["timeout"])
				}
			}
		}
	}
	if env, _ := root["env"].(map[string]any); env != nil {
		if a, ok := env["CERNE_ACTOR"]; ok {
			add("env", "CERNE_ACTOR", a)
		}
	}
	if servers, _ := root["mcpServers"].(map[string]any); servers != nil {
		if c, ok := servers["cerne"]; ok {
			add("mcp", "cerne", c)
		}
	}
	sort.Strings(out)
	return out
}

// pointerBlock is the managed block of a project doc, "" when there is none.
func pointerBlock(raw []byte) string {
	s := string(raw)
	i := strings.Index(s, markerBegin)
	j := strings.Index(s, markerEnd)
	if i < 0 || j < i {
		return ""
	}
	return strings.ReplaceAll(s[i:j+len(markerEnd)], "\r\n", "\n")
}

// Collect reads the managed items of the project at dir: a slot (a path, or
// path#hash for one hook of a configuration) mapped to its SHA-256. It only
// reads, and what is absent is simply not there.
func Collect(dir string) map[string]string {
	out := map[string]string{}
	for _, d := range wholeFileDirs {
		_ = filepath.WalkDir(filepath.Join(dir, filepath.FromSlash(d)), func(p string, e fs.DirEntry, err error) error {
			if err != nil || e.IsDir() {
				return nil
			}
			if b, rErr := os.ReadFile(p); rErr == nil {
				rel, _ := filepath.Rel(dir, p)
				out[filepath.ToSlash(rel)] = sum(b)
			}
			return nil
		})
	}
	for _, f := range blockFiles {
		if b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(f))); err == nil {
			for _, t := range canonicalTuples(b) {
				h := sum([]byte(t))
				out[f+"#"+h] = h
			}
		}
	}
	for _, f := range markerFiles {
		if b, err := os.ReadFile(filepath.Join(dir, f)); err == nil {
			if blk := pointerBlock(b); blk != "" {
				out[f] = sum([]byte(blk))
			}
		}
	}
	return out
}

// cutHash splits a slot of one hook of a configuration; ok is false for a file.
func cutHash(slot string) (path string, ok bool) {
	path, _, ok = strings.Cut(slot, "#")
	return path, ok
}

func pathOf(slot string) string { p, _ := cutHash(slot); return p }
