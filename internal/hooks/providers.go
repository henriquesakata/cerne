package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Provider adapters (F-0174): each install function writes the assets and
// wiring the provider's extension surface supports, per the researched
// capability matrix (R-0035 Codex, R-0036 Gemini, R-0037 Cursor). Every
// capability the surface cannot host (or that awaits live validation)
// becomes an explicit StatusSkipped result with the limitation or manual
// step as the note — degradation is reported, never silent. Unlike the
// Claude Code install, a corrupt provider config never fails the run: the
// asset degrades to skipped, because these files belong to the provider's
// user base first.

// failOpenCmd wraps a cerne hook invocation in the standard fail-open shell:
// a missing binary or a crashing hook must never break the host session.
func failOpenCmd(invocation string) string {
	return "sh -c 'command -v cerne >/dev/null 2>&1 || exit 0; " + invocation + " 2>/dev/null || true'"
}

// commandEntry is the {type, command} leaf shared by the Claude-style hook
// schemas (Claude, Codex, Gemini all nest it under {matcher?, hooks: [...]}).
func commandEntry(invocation string) map[string]any {
	return map[string]any{"type": "command", "command": failOpenCmd(invocation)}
}

// eventGroup builds one hooks.<Event> array member.
func eventGroup(matcher string, invocations ...string) map[string]any {
	var leaves []any
	for _, inv := range invocations {
		leaves = append(leaves, commandEntry(inv))
	}
	g := map[string]any{"hooks": leaves}
	if matcher != "" {
		g["matcher"] = matcher
	}
	return g
}

// mergeProviderJSON folds wanted event groups into the JSON file at path
// (creating it from base when absent). Presence is detected by invocation
// substring, so a re-run is idempotent and a hand-edited file keeps every
// byte the user wrote plus our appended entries (coarse re-marshal diff — a
// provider config is not expected to be as diff-sensitive as the tracked
// Claude settings.json). A corrupt file degrades to skipped.
func mergeProviderJSON(path string, base map[string]any, wanted map[string][]map[string]any) Result {
	asset := filepath.Base(filepath.Dir(path)) + "/" + filepath.Base(path)
	raw, err := os.ReadFile(path)
	root := map[string]any{}
	exists := err == nil
	if exists {
		if uErr := json.Unmarshal(raw, &root); uErr != nil {
			return Result{Asset: asset, Status: StatusSkipped, Note: "existing file is not valid JSON — add the cerne hook entries manually"}
		}
	} else {
		for k, v := range base {
			root[k] = v
		}
	}

	hooksVal, _ := root["hooks"].(map[string]any)
	if hooksVal == nil {
		if _, has := root["hooks"]; has {
			return Result{Asset: asset, Status: StatusSkipped, Note: `existing "hooks" key has an unexpected type — add the cerne hook entries manually`}
		}
		hooksVal = map[string]any{}
		root["hooks"] = hooksVal
	}

	changed := !exists
	for event, groups := range wanted {
		arr, _ := hooksVal[event].([]any)
		for _, g := range groups {
			if groupWired(arr, g) {
				continue
			}
			arr = append(arr, g)
			changed = true
		}
		hooksVal[event] = arr
	}
	if !changed {
		return Result{Asset: asset, Status: StatusUnchanged}
	}

	out, err := marshalIndentNoHTML(root, "", "  ")
	if err != nil {
		return Result{Asset: asset, Status: StatusSkipped, Note: "cannot render config: " + err.Error()}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return Result{Asset: asset, Status: StatusSkipped, Note: "cannot create directory: " + err.Error()}
	}
	if err := os.WriteFile(path, append(out, '\n'), 0o644); err != nil {
		return Result{Asset: asset, Status: StatusSkipped, Note: "cannot write: " + err.Error()}
	}
	if exists {
		return Result{Asset: asset, Status: StatusUpdated}
	}
	return Result{Asset: asset, Status: StatusCreated}
}

// groupWired reports whether any existing entry already carries the wanted
// group's invocation (substring match on the command). Handles both shapes:
// nested {matcher, hooks:[{command}]} (Claude/Codex/Gemini) and flat
// {command} (Cursor).
func groupWired(arr []any, wanted map[string]any) bool {
	needle, _ := wanted["command"].(string)
	if wantedLeaves, ok := wanted["hooks"].([]any); ok && len(wantedLeaves) > 0 {
		if first, ok := wantedLeaves[0].(map[string]any); ok {
			needle, _ = first["command"].(string)
		}
	}
	if needle == "" {
		return true
	}
	// The distinctive part is the cerne invocation, not the shell wrapper.
	if i := strings.Index(needle, "cerne hook"); i >= 0 {
		needle = strings.TrimSuffix(needle[i:], " 2>/dev/null || true'")
	}
	raw, _ := json.Marshal(arr)
	return strings.Contains(string(raw), needle)
}

// ─────────────────────────── Cursor (R-0037) ───────────────────────────

// cursorRules is the always-applied project rule carrying the registration
// floor (.cursor/rules/*.mdc, alwaysApply: true → injected at context start).
const cursorRules = `---
description: cerne project rules — work registration discipline
alwaysApply: true
---

` + RegisterRules + `
`

func installCursor(projDir string) []Result {
	var results []Result

	rulesDst := filepath.Join(projDir, ".cursor", "rules", "cerne.mdc")
	st, err := writeIfChanged(rulesDst, []byte(cursorRules))
	if err != nil {
		results = append(results, Result{Asset: "cursor: rules/cerne.mdc", Status: StatusSkipped, Note: err.Error()})
	} else {
		results = append(results, Result{Asset: "cursor: rules/cerne.mdc", Status: st})
	}

	results = append(results, mergeProviderJSON(
		filepath.Join(projDir, ".cursor", "hooks.json"),
		map[string]any{"version": 1},
		map[string][]map[string]any{
			// Cursor entries are flat {command,...}, not nested — build them here.
			"sessionStart": {flatCursorEntry("cerne hook session-context --format cursor")},
		},
	))

	// Explicit degradations (R-0037) — reported, never silent.
	results = append(results,
		Result{Asset: "cursor: work-gate", Status: StatusSkipped, Note: "native preToolUse lacks a documented session key for binding — enable Cursor's \"Include third-party Plugins, Skills, and other configs\" setting to reuse the installed .claude hooks (identical envelope) for the full gate"},
		Result{Asset: "cursor: CERNE_ACTOR", Status: StatusSkipped, Note: "no documented per-project session env — cerne's human gates cannot distinguish agent sessions on Cursor"},
		Result{Asset: "cursor: CERNE_PROVIDER", Status: StatusSkipped, Note: "no documented per-project session env — documents written from Cursor record their author as unknown unless CERNE_PROVIDER/CERNE_MODEL are exported in its shell"},
		Result{Asset: "cursor: subagent-context", Status: StatusSkipped, Note: "subagentStart output has no additional_context — subagents start without the live board"},
	)
	return results
}

// flatCursorEntry builds Cursor's flat hook entry ({command}, no nested
// hooks array — R-0037 schema).
func flatCursorEntry(invocation string) map[string]any {
	return map[string]any{"command": failOpenCmd(invocation)}
}

// ─────────────────────────── Codex (R-0035) ───────────────────────────

// codexEnvFragment opts the project's Codex sessions into CERNE_ACTOR=agent
// so cerne's human gates recognize them (shell_environment_policy, R-0035),
// and declares CERNE_PROVIDER for the documents they write (F-0375): openai is
// Codex's default provider; no CERNE_MODEL, which Codex does not expose.
const codexEnvFragment = `# cerne (F-0174): human gates recognize agent sessions; F-0375: documents record their provider.
[shell_environment_policy]
set = { CERNE_ACTOR = "agent", CERNE_PROVIDER = "openai" }
`

func installCodex(projDir string) []Result {
	var results []Result

	r := mergeProviderJSON(
		filepath.Join(projDir, ".codex", "hooks.json"),
		map[string]any{},
		map[string][]map[string]any{
			"SessionStart": {eventGroup("", "cerne hook session-context --format codex")},
			"PreToolUse":   {eventGroup("Edit|Write", "cerne hook work-gate")},
			"PostToolUse": {
				eventGroup("Bash", "cerne hook bind-work"),
				eventGroup("Bash", "cerne hook lifecycle-reminder"),
				eventGroup("Bash", "cerne hook provenance"),
			},
			"Stop": {eventGroup("", "cerne hook stop-check")},
		},
	)
	if r.Note == "" {
		r.Note = "requires a trusted project (projects.<path>.trust_level) and per-hook trust review (/hooks)"
	}
	results = append(results, r)

	envDst := filepath.Join(projDir, ".codex", "config.toml")
	results = append(results, ensureLineFile(envDst, "codex: config.toml (CERNE_ACTOR, CERNE_PROVIDER)", "CERNE_PROVIDER", codexEnvFragment, false))

	results = append(results, Result{Asset: "codex: subagent-context", Status: StatusSkipped, Note: "SubagentStart wiring pending live validation of its context envelope"})
	return results
}

// ─────────────────────────── Gemini (R-0036) ───────────────────────────

func installGemini(projDir string) []Result {
	var results []Result

	results = append(results, mergeProviderJSON(
		filepath.Join(projDir, ".gemini", "settings.json"),
		map[string]any{},
		map[string][]map[string]any{
			"SessionStart": {eventGroup("", "cerne hook session-context --format gemini")},
			"BeforeTool":   {eventGroup("write_file|replace", "cerne hook work-gate --format gemini")},
			"AfterTool":    {eventGroup("run_shell_command", "cerne hook bind-work --format gemini")},
		},
	))

	envDst := filepath.Join(projDir, ".gemini", ".env")
	results = append(results, ensureLineFile(envDst, "gemini: .env (CERNE_ACTOR)", "CERNE_ACTOR",
		"# cerne (F-0174): human gates recognize agent sessions.\nCERNE_ACTOR=agent\n", true))
	// F-0375: the provider of the documents Gemini writes; Gemini CLI exposes
	// no model to the shell, so no CERNE_MODEL.
	results = append(results, ensureLineFile(envDst, "gemini: .env (CERNE_PROVIDER)", "CERNE_PROVIDER",
		"# cerne (F-0375): documents record their provider.\nCERNE_PROVIDER=google\n", true))

	results = append(results,
		Result{Asset: "gemini: stop-closeout", Status: StatusSkipped, Note: "AfterAgent deny+reason forces a retry (stronger than advisory) — deferred pending live validation"},
		Result{Asset: "gemini: lifecycle/provenance reminders", Status: StatusSkipped, Note: "AfterTool context envelope pending live validation"},
		Result{Asset: "gemini: subagent-context", Status: StatusSkipped, Note: "subagents are a preview feature — hook reach unvalidated"},
	)
	return results
}

// ensureLineFile guarantees a config file carries a marker (e.g. CERNE_ACTOR):
// absent file → created with content; marker present → unchanged; marker
// absent → appended when appendable, otherwise skipped with the manual step.
func ensureLineFile(path, asset, marker, content string, appendable bool) Result {
	raw, err := os.ReadFile(path)
	switch {
	case err == nil && strings.Contains(string(raw), marker):
		return Result{Asset: asset, Status: StatusUnchanged}
	case err == nil && appendable:
		out := string(raw)
		if !strings.HasSuffix(out, "\n") {
			out += "\n"
		}
		if wErr := os.WriteFile(path, []byte(out+content), 0o644); wErr != nil {
			return Result{Asset: asset, Status: StatusSkipped, Note: "cannot write: " + wErr.Error()}
		}
		return Result{Asset: asset, Status: StatusUpdated}
	case err == nil:
		return Result{Asset: asset, Status: StatusSkipped, Note: "file exists — add manually:\n" + content}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return Result{Asset: asset, Status: StatusSkipped, Note: "cannot create directory: " + err.Error()}
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return Result{Asset: asset, Status: StatusSkipped, Note: "cannot write: " + err.Error()}
	}
	return Result{Asset: asset, Status: StatusCreated}
}
