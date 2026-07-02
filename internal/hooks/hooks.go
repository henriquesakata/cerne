// Package hooks embeds cerne's canonical Claude Code hooks and installs them
// into a project's .claude/ tree — the hook script under .claude/hooks/ plus its
// wiring in .claude/settings.json. cerne owns these; clients consume them via
// `cerne hook install` instead of maintaining divergent copies. The provenance
// reminder (F-0103) fires on decision gates and nudges the agent to register the
// external sources that shaped a decision.
package hooks

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

//go:embed assets/register-reference-reminder.sh
var files embed.FS

const (
	// hookScript is the installed file name under .claude/hooks/.
	hookScript = "register-reference-reminder.sh"
	// hookCommand is the settings.json command that runs the installed script.
	hookCommand = `bash "$CLAUDE_PROJECT_DIR/.claude/hooks/register-reference-reminder.sh"`
)

// Status is the outcome of installing one asset.
type Status string

const (
	StatusCreated   Status = "created"
	StatusUpdated   Status = "updated"
	StatusUnchanged Status = "unchanged"
)

// Result pairs an installed asset with what Install did to it.
type Result struct {
	Asset  string
	Status Status
}

// Install writes the hook script under <projDir>/.claude/hooks/ and merges its
// PostToolUse/Bash wiring into <projDir>/.claude/settings.json — idempotently and
// without disturbing existing hooks or other settings. It reports, per asset,
// whether it was created, updated, or already current. A corrupt settings.json
// is a fail-closed error: nothing is rewritten.
func Install(projDir string) ([]Result, error) {
	var results []Result

	scriptDst := filepath.Join(projDir, ".claude", "hooks", hookScript)
	st, err := writeIfChanged(scriptDst, []byte(mustRead("assets/"+hookScript)))
	if err != nil {
		return nil, err
	}
	results = append(results, Result{Asset: hookScript, Status: st})

	settingsDst := filepath.Join(projDir, ".claude", "settings.json")
	st, err = mergeSettingsFile(settingsDst)
	if err != nil {
		return nil, err
	}
	results = append(results, Result{Asset: "settings.json", Status: st})

	return results, nil
}

func mustRead(name string) string {
	b, err := files.ReadFile(name)
	if err != nil {
		panic("hooks: embedded file missing: " + name)
	}
	return string(b)
}

// writeIfChanged writes content to dst (creating parent dirs), reporting whether
// it was created, updated, or left unchanged (byte-identical).
func writeIfChanged(dst string, content []byte) (Status, error) {
	existing, err := os.ReadFile(dst)
	if err == nil {
		if bytes.Equal(existing, content) {
			return StatusUnchanged, nil
		}
		if err := os.WriteFile(dst, content, 0o644); err != nil {
			return "", err
		}
		return StatusUpdated, nil
	}
	if !os.IsNotExist(err) {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(dst, content, 0o644); err != nil {
		return "", err
	}
	return StatusCreated, nil
}

// mergeSettingsFile folds the hook wiring into the settings.json at dst,
// preserving every other key and hook. Absent → created; already wired →
// unchanged; corrupt JSON → fail-closed error (dst untouched).
func mergeSettingsFile(dst string) (Status, error) {
	existing, err := os.ReadFile(dst)
	switch {
	case err == nil:
		merged, changed, mErr := mergeSettings(existing)
		if mErr != nil {
			return "", mErr
		}
		if !changed {
			return StatusUnchanged, nil
		}
		if err := os.WriteFile(dst, merged, 0o644); err != nil {
			return "", err
		}
		return StatusUpdated, nil
	case os.IsNotExist(err):
		fresh, _, mErr := mergeSettings(nil)
		if mErr != nil {
			return "", mErr
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(dst, fresh, 0o644); err != nil {
			return "", err
		}
		return StatusCreated, nil
	default:
		return "", err
	}
}

// mergeSettings folds the PostToolUse/Bash hook entry into raw Claude Code
// settings JSON (nil/empty → a fresh document). It returns the new content, a
// changed flag (false when the entry is already present), and an error only when
// raw is present but not valid JSON (fail-closed — the caller must not write).
func mergeSettings(raw []byte) ([]byte, bool, error) {
	root := map[string]any{}
	if len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, &root); err != nil {
			return nil, false, fmt.Errorf("hooks: settings.json inválido, não será sobrescrito: %w", err)
		}
	}
	hooksVal, ok := root["hooks"]
	if !ok {
		hooksVal = map[string]any{}
	}
	hooksMap, ok := hooksVal.(map[string]any)
	if !ok {
		return nil, false, errors.New(`hooks: settings.json tem "hooks" com tipo inesperado`)
	}
	post, _ := hooksMap["PostToolUse"].([]any)
	if hookPresent(post) {
		return nil, false, nil
	}
	hooksMap["PostToolUse"] = append(post, map[string]any{
		"matcher": "Bash",
		"hooks": []any{
			map[string]any{"type": "command", "command": hookCommand},
		},
	})
	root["hooks"] = hooksMap

	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return nil, false, err
	}
	return append(out, '\n'), true, nil
}

// hookPresent reports whether any PostToolUse entry already wires our script (by
// command substring), which makes install idempotent.
func hookPresent(entries []any) bool {
	for _, e := range entries {
		m, ok := e.(map[string]any)
		if !ok {
			continue
		}
		hs, ok := m["hooks"].([]any)
		if !ok {
			continue
		}
		for _, h := range hs {
			hm, ok := h.(map[string]any)
			if !ok {
				continue
			}
			if cmd, ok := hm["command"].(string); ok && strings.Contains(cmd, hookScript) {
				return true
			}
		}
	}
	return false
}
