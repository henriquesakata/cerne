package hooks

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/henriquesakata/cerne/internal/mcp"
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
	return "sh -c 'command -v cerne >/dev/null 2>&1 || PATH=\"${PATH:+$PATH:}$HOME/go/bin:$HOME/.local/bin:/opt/homebrew/bin:/usr/local/bin\"; command -v cerne >/dev/null 2>&1 || exit 0; " + invocation + " 2>/dev/null || true'"
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
	if err := writeFileAtomic(path, append(out, '\n'), 0o644); err != nil {
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

func installCursor(projDir string, explicit, network bool) []Result {
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
	if explicit { // never on a renewal: see InstallNetwork
		results = append(results, installCursorSandbox(projDir, network), cursorMCP(projDir))
	}

	// Not installed natively (R-0037) — reported, with what covers them, never silent.
	results = append(results,
		Result{Asset: "cursor: work-gate", Status: StatusSkipped, Note: "not wired natively: Cursor runs the installed .claude hooks (work gate, session binding, provenance, tool notes, stop closeout) through its third-party import, the \"Include Third-Party Plugins, Skills, and Other Configs\" setting (on by default); with it off, only the rules and the live board reach Cursor (F-0428)"},
		Result{Asset: "cursor: CERNE_ACTOR", Status: StatusSkipped, Note: "nothing to install: Cursor marks its agent shells with CURSOR_AGENT=1 (seen in Cursor 3.25.6, undocumented), which cerne's human gates read as an agent (F-0428)"},
		Result{Asset: "cursor: subagent-context", Status: StatusSkipped, Note: "subagentStart output has no additional_context — subagents start without the live board"},
		Result{Asset: "cursor: prompt-notes", Status: StatusSkipped, Note: "notes matching the prompt are only injected on Claude Code (UserPromptSubmit); no verified equivalent prompt-submit event that adds context is wired here (B-0020)"},
		Result{Asset: "cursor: wake", Status: StatusSkipped, Note: "an idle session is not woken by a channel message addressed to it (F-0491): no documented way to open a turn in it — the message waits for the next contact point"},
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

func installCodex(projDir string, network bool) []Result {
	var results []Result

	r := mergeProviderJSON(
		filepath.Join(projDir, ".codex", "hooks.json"),
		map[string]any{},
		map[string][]map[string]any{
			"SessionStart": {eventGroup("", "cerne hook session-context --format codex")},
			// F-0491: Codex documents additionalContext on UserPromptSubmit, in
			// Claude Code's envelope (R-0035): the channel and the prompt notes.
			"UserPromptSubmit": {eventGroup("", "cerne hook prompt-notes")},
			"PreToolUse": {
				eventGroup("Edit|Write", "cerne hook work-gate"),
				eventGroup("Bash", "cerne hook tool-notes --format codex"), // F-0457
			},
			"PostToolUse": {
				eventGroup("Bash", "cerne hook bind-work"),
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

	results = append(results, installCodexSandbox(projDir, network)...)

	results = append(results, Result{Asset: "codex: subagent-context", Status: StatusSkipped, Note: "SubagentStart wiring pending live validation of its context envelope"})
	results = append(results, Result{Asset: "codex: wake", Status: StatusSkipped, Note: "an idle session is not woken by a channel message addressed to it (F-0491): only the app-server connection's owner starts a turn, and cerne does not own the TUI's — the message waits for the next contact point"})
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
			"AfterTool": {
				eventGroup("run_shell_command", "cerne hook bind-work --format gemini"),
				eventGroup("run_shell_command|write_file|replace", "cerne hook tool-notes --after --format gemini"), // F-0457
			},
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
		Result{Asset: "gemini: prompt-notes", Status: StatusSkipped, Note: "notes matching the prompt are only injected on Claude Code (UserPromptSubmit); no verified equivalent prompt-submit event that adds context is wired here (B-0020)"},
		Result{Asset: "gemini: wake", Status: StatusSkipped, Note: "an idle session is not woken by a channel message addressed to it (F-0491): no documented way to open a turn in it — the message waits for the next contact point"},
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
		if wErr := writeFileAtomic(path, []byte(out+content), 0o644); wErr != nil {
			return Result{Asset: asset, Status: StatusSkipped, Note: "cannot write: " + wErr.Error()}
		}
		return Result{Asset: asset, Status: StatusUpdated}
	case err == nil:
		return Result{Asset: asset, Status: StatusSkipped, Note: "file exists — add manually:\n" + content}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return Result{Asset: asset, Status: StatusSkipped, Note: "cannot create directory: " + err.Error()}
	}
	if err := writeFileAtomic(path, []byte(content), 0o644); err != nil {
		return Result{Asset: asset, Status: StatusSkipped, Note: "cannot write: " + err.Error()}
	}
	return Result{Asset: asset, Status: StatusCreated}
}

// ─────────────────────────── Copilot (F-0444) ───────────────────────────

// copilotHooksFile is cerne's own hook file; other files under .github/hooks/
// belong to the team and are never read or touched.
const copilotHooksFile = ".github/hooks/cerne.json"

// copilotEntry builds one flat Copilot hook entry: bash and powershell are both
// fail-open (the cloud agent honors bash only), and the env block declares the
// agent actor and the document provider.
func copilotEntry(matcher, invocation string) map[string]any {
	e := map[string]any{
		"type":       "command",
		"bash":       failOpenCmd(invocation),
		"powershell": "if (Get-Command cerne -ErrorAction SilentlyContinue) { " + invocation + " 2>$null }; exit 0",
		"env":        map[string]any{"CERNE_ACTOR": "agent", "CERNE_PROVIDER": "copilot"},
	}
	if matcher != "" {
		e["matcher"] = matcher
	}
	return e
}

// copilotHooksBytes is the exact content of .github/hooks/cerne.json.
func copilotHooksBytes() ([]byte, error) {
	doc := map[string]any{
		"version": 1,
		"hooks": map[string]any{
			"SessionStart": []any{copilotEntry("", "cerne hook session-context --format copilot")},
			"PreToolUse":   []any{copilotEntry("Edit|Write", "cerne hook work-gate --format copilot")},
			"PostToolUse": []any{
				copilotEntry("Bash", "cerne hook bind-work --format copilot"),
				copilotEntry("Bash", "cerne hook provenance --format copilot"),
				copilotEntry("Bash", "cerne hook tool-notes --after --format copilot"),       // F-0457
				copilotEntry("Edit|Write", "cerne hook tool-notes --after --format copilot"), // F-0457
			},
			"Stop": []any{copilotEntry("", "cerne hook stop-check")},
		},
	}
	out, err := marshalIndentNoHTML(doc, "", "  ")
	return append(out, '\n'), err
}

func installCopilot(projDir string) []Result {
	asset := "copilot: hooks/cerne.json"
	dst := filepath.Join(projDir, filepath.FromSlash(copilotHooksFile))
	out, err := copilotHooksBytes()
	if err != nil {
		return []Result{{Asset: asset, Status: StatusSkipped, Note: "cannot render config: " + err.Error()}}
	}
	// The file is cerne's, rewritten whole — unless an existing one is not JSON,
	// which the user may be mid-edit on: leave it (fail-closed, F-0174).
	if raw, rErr := os.ReadFile(dst); rErr == nil && !json.Valid(raw) {
		return []Result{{Asset: asset, Status: StatusSkipped, Note: "existing file is not valid JSON — fix or delete it, then rerun"}}
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return []Result{{Asset: asset, Status: StatusSkipped, Note: "cannot create directory: " + err.Error()}}
	}
	st, err := writeIfChanged(dst, out)
	r := Result{Asset: asset, Status: st}
	if st == StatusUpdated {
		r.Note = "cerne-managed file rewritten whole — local edits are not kept; put team hooks in their own file under .github/hooks/"
	}
	if err != nil {
		r = Result{Asset: asset, Status: StatusSkipped, Note: "cannot write: " + err.Error()}
	}
	mcpRes, block := copilotMCP(projDir)
	return []Result{r, mcpRes,
		{Asset: "copilot: cloud agent MCP", Status: StatusSkipped, Note: "the cloud agent does not read .mcp.json — paste this block in the repository settings (Copilot, coding agent, MCP configuration), and install the cerne binary in its environment (.github/workflows/copilot-setup-steps.yml):\n" + block},
		{Asset: "copilot: double execution", Status: StatusSkipped, Note: "Copilot CLI also runs the cerne hooks of .claude/settings.json when both are installed; it ignores their Claude-format output, so only side effects repeat (session binding, sign of life)"},
		{Asset: "copilot: stop closeout", Status: StatusSkipped, Note: "the Stop block decision was never exercised live (only the payload was captured) — treat the closeout reminder as unverified"},
		{Asset: "copilot: precompact", Status: StatusSkipped, Note: "Copilot's preCompact hook is notification-only — the handoff code is not kept through compaction"},
		{Asset: "copilot: CERNE_PROVIDER", Status: StatusSkipped, Note: "CERNE_PROVIDER=copilot reaches the hook processes only, not the bash tool shell — documents written from the shell record their author as unknown unless it is exported there"},
		{Asset: "copilot: prompt-notes", Status: StatusSkipped, Note: "notes matching the prompt are only injected on Claude Code (UserPromptSubmit); no verified equivalent prompt-submit event that adds context is wired here (B-0020)"},
		{Asset: "copilot: wake", Status: StatusSkipped, Note: "an idle session is not woken by a channel message addressed to it (F-0491): no documented way to open a turn in it — the message waits for the next contact point"},
		{Asset: "copilot: subagent-context", Status: StatusSkipped, Note: "no subagent hook is wired — subagents start without the live board"},
	}
}

// copilotMCPServer is the read-only cerne server as Copilot's .mcp.json wants
// it: the CLI requires the explicit tools list (F-0444).
func copilotMCPServer() map[string]any {
	tools := make([]any, len(mcp.ToolNames))
	for i, n := range mcp.ToolNames {
		tools[i] = n
	}
	return map[string]any{"type": "stdio", "command": "cerne", "args": []any{"mcp"}, "tools": tools}
}

// copilotMCP merges the cerne server into the root .mcp.json; it returns the
// result and the JSON block for the repository settings (the cloud agent's only
// MCP source).
func copilotMCP(projDir string) (Result, string) {
	server := copilotMCPServer()
	blockRaw, _ := marshalIndentNoHTML(map[string]any{"mcpServers": map[string]any{"cerne": server}}, "", "  ")
	r := mergeMCPServer("copilot: .mcp.json", filepath.Join(projDir, ".mcp.json"), server, "add the read-only cerne server by hand (block below)")
	if r.Status == StatusCreated || r.Status == StatusUpdated {
		r.Note = "read-only server, also read by Claude Code from this file"
	}
	return r, string(blockRaw)
}

// decodeObject reads a JSON object keeping each number as written
// (json.Number): a float64 round trip changes a large integer on rewrite.
// Anything but exactly one object is not ok.
func decodeObject(raw []byte) (map[string]any, bool) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var root map[string]any
	if dec.Decode(&root) != nil || root == nil || dec.Decode(&struct{}{}) != io.EOF {
		return nil, false
	}
	return root, true
}

// mergeMCPServer merges server as mcpServers.cerne into the JSON file at path,
// keeping every other server and key (Copilot F-0444, Cursor F-0512). A
// different cerne entry or an unreadable file stays untouched, reported
// skipped with manual, the step to do by hand.
func mergeMCPServer(asset, path string, server map[string]any, manual string) Result {
	skip := func(note string) Result { return Result{Asset: asset, Status: StatusSkipped, Note: note} }
	root := map[string]any{}
	raw, err := os.ReadFile(path)
	exists := err == nil
	if exists {
		var ok bool
		if root, ok = decodeObject(raw); !ok {
			return skip("existing file is not valid JSON — " + manual)
		}
	} else if !os.IsNotExist(err) {
		return skip("cannot read: " + err.Error())
	}
	servers, _ := root["mcpServers"].(map[string]any)
	if servers == nil {
		if _, has := root["mcpServers"]; has {
			return skip(`existing "mcpServers" key has an unexpected type — ` + manual)
		}
		servers = map[string]any{}
		root["mcpServers"] = servers
	}
	if cur, has := servers["cerne"]; has {
		// Compare through JSON so both sides carry the same dynamic types.
		wantRaw, _ := json.Marshal(server)
		want, _ := decodeObject(wantRaw)
		if reflect.DeepEqual(cur, any(want)) {
			return Result{Asset: asset, Status: StatusUnchanged}
		}
		return skip(`a different "cerne" server is already declared — left as is; ` + manual)
	}
	servers["cerne"] = server
	out, err := marshalIndentNoHTML(root, "", "  ")
	if err != nil {
		return skip("cannot render config: " + err.Error())
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return skip("cannot create directory: " + err.Error())
	}
	if err := writeFileAtomic(path, append(out, '\n'), 0o644); err != nil {
		return skip("cannot write: " + err.Error())
	}
	if exists {
		return Result{Asset: asset, Status: StatusUpdated}
	}
	return Result{Asset: asset, Status: StatusCreated}
}
