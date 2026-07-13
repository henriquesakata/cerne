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
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

//go:embed assets/register-reference-reminder.sh assets/cerne-register-reminder.sh assets/cerne-subagent-context.sh assets/cerne-work-bind.sh assets/cerne-work-gate.sh assets/cerne-lifecycle-reminder.sh
var files embed.FS

const (
	// hookScript is the installed file name under .claude/hooks/.
	hookScript = "register-reference-reminder.sh"
	// hookCommand is the settings.json command that runs the installed script.
	hookCommand = `bash "$CLAUDE_PROJECT_DIR/.claude/hooks/register-reference-reminder.sh"`
	// registerScript is the SessionStart registration reminder (F-0139): the
	// feature/work/human-gates rules injected at the start of every session.
	registerScript = "cerne-register-reminder.sh"
	// registerCommand is the settings.json command that runs registerScript.
	registerCommand = `bash "$CLAUDE_PROJECT_DIR/.claude/hooks/cerne-register-reminder.sh"`
	// subagentScript is the SubagentStart context injector (F-0150): SessionStart
	// does not fire inside subagents, so this script hands every spawned subagent
	// the condensed cerne rules plus the live work list.
	subagentScript = "cerne-subagent-context.sh"
	// subagentCommand is the settings.json command that runs subagentScript.
	subagentCommand = `bash "$CLAUDE_PROJECT_DIR/.claude/hooks/cerne-subagent-context.sh"`
	// bindScript is the PostToolUse session→work binder (F-0151): a successful
	// `cerne work start`/`feature start` seen in a Bash call binds the session
	// to the registered id, so the work gate recognizes it.
	bindScript = "cerne-work-bind.sh"
	// bindCommand is the settings.json command that runs bindScript.
	bindCommand = `bash "$CLAUDE_PROJECT_DIR/.claude/hooks/cerne-work-bind.sh"`
	// gateScript is the PreToolUse work gate (F-0151): denies Edit/Write in a
	// session with no live registration when [gates] work_gate is on (the
	// toggle is evaluated by the Go subcommand at run time).
	gateScript = "cerne-work-gate.sh"
	// gateCommand is the settings.json command that runs gateScript.
	gateCommand = `bash "$CLAUDE_PROJECT_DIR/.claude/hooks/cerne-work-gate.sh"`
	// gateMatcher limits the gate to the file-editing tools.
	gateMatcher = "Edit|Write"
	// lifecycleScript is the PostToolUse lifecycle reminder (F-0170): after a
	// successful `cerne feature new`/`feature start` it injects the guidance
	// for that exact moment (write the spec in this turn / consult the notes).
	lifecycleScript = "cerne-lifecycle-reminder.sh"
	// lifecycleCommand is the settings.json command that runs lifecycleScript.
	lifecycleCommand = `bash "$CLAUDE_PROJECT_DIR/.claude/hooks/cerne-lifecycle-reminder.sh"`
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

	registerDst := filepath.Join(projDir, ".claude", "hooks", registerScript)
	st, err = writeIfChanged(registerDst, []byte(mustRead("assets/"+registerScript)))
	if err != nil {
		return nil, err
	}
	results = append(results, Result{Asset: registerScript, Status: st})

	subagentDst := filepath.Join(projDir, ".claude", "hooks", subagentScript)
	st, err = writeIfChanged(subagentDst, []byte(mustRead("assets/"+subagentScript)))
	if err != nil {
		return nil, err
	}
	results = append(results, Result{Asset: subagentScript, Status: st})

	for _, script := range []string{bindScript, gateScript, lifecycleScript} {
		dst := filepath.Join(projDir, ".claude", "hooks", script)
		st, err = writeIfChanged(dst, []byte(mustRead("assets/"+script)))
		if err != nil {
			return nil, err
		}
		results = append(results, Result{Asset: script, Status: st})
	}

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

// mergeSettings folds the PostToolUse/Bash hook entry and the
// env.CERNE_ACTOR=agent marker (F-0131 crit. 11) into raw Claude Code
// settings JSON (nil/empty → a fresh document). It returns the new content, a
// changed flag (false when both are already present), and an error only when
// raw is present but not valid JSON (fail-closed — the caller must not write).
//
// To keep tracked settings.json diffs honest, each entry is spliced into the
// raw bytes surgically: existing key order, indentation, and escaping are
// preserved, and only the new entry (plus a comma on the previous last
// element) is added — not the whole-file reformat a map round-trip through
// json.MarshalIndent causes. Each splice is verified to be semantically
// identical to the map-based merge; on any mismatch the whole merge falls
// back to a clean re-render (correct, just a coarser diff), so a splicing bug
// can never write invalid or wrong JSON.
func mergeSettings(raw []byte) ([]byte, bool, error) {
	root := map[string]any{}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) > 0 {
		if err := json.Unmarshal(raw, &root); err != nil {
			return nil, false, fmt.Errorf("hooks: invalid settings.json, will not overwrite: %w", err)
		}
	}

	// Detect structure + idempotency from the parsed map (this is also the
	// validity gate: an invalid document already failed above).
	hooksVal, hasHooks := root["hooks"]
	var hooksMap map[string]any
	if hasHooks {
		m, ok := hooksVal.(map[string]any)
		if !ok {
			return nil, false, errors.New(`hooks: settings.json has "hooks" with an unexpected type`)
		}
		hooksMap = m
	}
	var post []any
	hasPTU := false
	if hooksMap != nil {
		if pv, ok := hooksMap["PostToolUse"]; ok {
			arr, ok := pv.([]any)
			if !ok {
				return nil, false, errors.New(`hooks: settings.json has "PostToolUse" with an unexpected type`)
			}
			post = arr
			hasPTU = true
		}
	}
	needHook := !hookPresent(post)
	needBind := !scriptWired(post, bindScript)
	needLifecycle := !scriptWired(post, lifecycleScript)

	var pre []any
	hasPre := false
	if hooksMap != nil {
		if pv, ok := hooksMap["PreToolUse"]; ok {
			arr, ok := pv.([]any)
			if !ok {
				return nil, false, errors.New(`hooks: settings.json has "PreToolUse" with an unexpected type`)
			}
			pre = arr
			hasPre = true
		}
	}
	needGate := !scriptWired(pre, gateScript)

	var sessionStart []any
	hasSS := false
	if hooksMap != nil {
		if sv, ok := hooksMap["SessionStart"]; ok {
			arr, ok := sv.([]any)
			if !ok {
				return nil, false, errors.New(`hooks: settings.json has "SessionStart" with an unexpected type`)
			}
			sessionStart = arr
			hasSS = true
		}
	}
	needRegister := !scriptWired(sessionStart, registerScript)

	var subagentStart []any
	hasSub := false
	if hooksMap != nil {
		if sv, ok := hooksMap["SubagentStart"]; ok {
			arr, ok := sv.([]any)
			if !ok {
				return nil, false, errors.New(`hooks: settings.json has "SubagentStart" with an unexpected type`)
			}
			subagentStart = arr
			hasSub = true
		}
	}
	needSubagent := !scriptWired(subagentStart, subagentScript)

	envVal, hasEnv := root["env"]
	var envMap map[string]any
	if hasEnv {
		m, ok := envVal.(map[string]any)
		if !ok {
			return nil, false, errors.New(`hooks: settings.json tem "env" com tipo inesperado`)
		}
		envMap = m
	}
	needActor := !actorPresent(envMap)

	if !needHook && !needBind && !needLifecycle && !needGate && !needRegister && !needSubagent && !needActor {
		return nil, false, nil
	}

	unit := detectIndentUnit(raw)

	// Fresh document: nothing to preserve, emit a clean (escape-safe) render.
	if len(trimmed) == 0 {
		fresh := map[string]any{}
		if needHook {
			fresh = withHookEntry(fresh)
		}
		if needBind {
			fresh = withMatcherEntry(fresh, "PostToolUse", "Bash", bindCommand)
		}
		if needLifecycle {
			fresh = withMatcherEntry(fresh, "PostToolUse", "Bash", lifecycleCommand)
		}
		if needRegister {
			fresh = withEventEntry(fresh, "SessionStart", registerCommand)
		}
		if needSubagent {
			fresh = withEventEntry(fresh, "SubagentStart", subagentCommand)
		}
		if needGate {
			fresh = withMatcherEntry(fresh, "PreToolUse", gateMatcher, gateCommand)
		}
		if needActor {
			fresh = withActorEntry(fresh)
		}
		out, err := marshalIndentNoHTML(fresh, "", unit)
		if err != nil {
			return nil, false, err
		}
		return append(out, '\n'), true, nil
	}

	// Surgical splice on the raw bytes, verified against the canonical merge,
	// one entry at a time (each re-parses the previous splice's output so the
	// second splice sees the first's result). Any failure at any step falls
	// back to a single clean re-render of the full expected document.
	expected := map[string]any{}
	for k, v := range root {
		expected[k] = v
	}
	if needHook {
		expected = withHookEntry(expected)
	}
	if needBind {
		expected = withMatcherEntry(expected, "PostToolUse", "Bash", bindCommand)
	}
	if needLifecycle {
		expected = withMatcherEntry(expected, "PostToolUse", "Bash", lifecycleCommand)
	}
	if needRegister {
		expected = withEventEntry(expected, "SessionStart", registerCommand)
	}
	if needSubagent {
		expected = withEventEntry(expected, "SubagentStart", subagentCommand)
	}
	if needGate {
		expected = withMatcherEntry(expected, "PreToolUse", gateMatcher, gateCommand)
	}
	if needActor {
		expected = withActorEntry(expected)
	}

	cur := raw
	ok := true
	if needHook {
		spliced, spliceOK := spliceMatcherEntry(cur, "PostToolUse", newMatcherEntry("Bash", hookCommand), hasHooks, hasPTU, detectNewline(cur), unit)
		if !spliceOK {
			ok = false
		} else {
			cur = spliced
		}
	}
	if ok && needBind {
		curHasHooks, curHasPTU := reDetectEvent(cur, "PostToolUse", hasHooks, hasPTU)
		spliced, spliceOK := spliceMatcherEntry(cur, "PostToolUse", newMatcherEntry("Bash", bindCommand), curHasHooks, curHasPTU, detectNewline(cur), unit)
		if !spliceOK {
			ok = false
		} else {
			cur = spliced
		}
	}
	if ok && needLifecycle {
		curHasHooks, curHasPTU := reDetectEvent(cur, "PostToolUse", hasHooks, hasPTU)
		spliced, spliceOK := spliceMatcherEntry(cur, "PostToolUse", newMatcherEntry("Bash", lifecycleCommand), curHasHooks, curHasPTU, detectNewline(cur), unit)
		if !spliceOK {
			ok = false
		} else {
			cur = spliced
		}
	}
	if ok && needRegister {
		// Re-detect structure: the hook splice above may have created "hooks".
		curHasHooks, curHasSS := reDetectEvent(cur, "SessionStart", hasHooks, hasSS)
		spliced, spliceOK := spliceEventEntry(cur, "SessionStart", newEventEntry(registerCommand), curHasHooks, curHasSS, detectNewline(cur), unit)
		if !spliceOK {
			ok = false
		} else {
			cur = spliced
		}
	}
	if ok && needSubagent {
		// Re-detect again: the splices above may have created "hooks".
		curHasHooks, curHasSub := reDetectEvent(cur, "SubagentStart", hasHooks, hasSub)
		spliced, spliceOK := spliceEventEntry(cur, "SubagentStart", newEventEntry(subagentCommand), curHasHooks, curHasSub, detectNewline(cur), unit)
		if !spliceOK {
			ok = false
		} else {
			cur = spliced
		}
	}
	if ok && needGate {
		curHasHooks, curHasPre := reDetectEvent(cur, "PreToolUse", hasHooks, hasPre)
		spliced, spliceOK := spliceMatcherEntry(cur, "PreToolUse", newMatcherEntry(gateMatcher, gateCommand), curHasHooks, curHasPre, detectNewline(cur), unit)
		if !spliceOK {
			ok = false
		} else {
			cur = spliced
		}
	}
	if ok && needActor {
		spliced, spliceOK := spliceActor(cur, hasEnv, detectNewline(cur), unit)
		if !spliceOK {
			ok = false
		} else {
			cur = spliced
		}
	}
	if ok {
		var got map[string]any
		if json.Unmarshal(cur, &got) == nil && reflect.DeepEqual(got, expected) {
			return cur, true, nil
		}
	}
	// Fallback: correct but coarser diff (escaping still preserved).
	out, err := marshalIndentNoHTML(expected, "", unit)
	if err != nil {
		return nil, false, err
	}
	return append(out, '\n'), true, nil
}

// withHookEntry ensures root carries the provenance PostToolUse/Bash wiring.
// It is the canonical (key-order-agnostic) merge: used to seed a fresh
// document, to build the fallback render, and as the oracle the surgical
// splice is verified against.
func withHookEntry(root map[string]any) map[string]any {
	return withMatcherEntry(root, "PostToolUse", "Bash", hookCommand)
}

// withMatcherEntry ensures root carries a matcher-ful hook entry under the
// given event, creating the hooks object and the event array as needed.
// Canonical merge — seed, fallback render and splice oracle.
func withMatcherEntry(root map[string]any, event, matcher, command string) map[string]any {
	hooksMap, _ := root["hooks"].(map[string]any)
	if hooksMap == nil {
		hooksMap = map[string]any{}
	}
	arr, _ := hooksMap[event].([]any)
	arr = append(arr, map[string]any{
		"matcher": matcher,
		"hooks":   []any{map[string]any{"type": "command", "command": command}},
	})
	hooksMap[event] = arr
	root["hooks"] = hooksMap
	return root
}

// withEventEntry ensures root carries the wiring for a matcher-less hook event
// (SessionStart for the registration reminder F-0139, SubagentStart for the
// subagent context injector F-0150), creating the hooks object and the event
// array as needed. Canonical merge — seed, fallback render and splice oracle.
func withEventEntry(root map[string]any, event, command string) map[string]any {
	hooksMap, _ := root["hooks"].(map[string]any)
	if hooksMap == nil {
		hooksMap = map[string]any{}
	}
	arr, _ := hooksMap[event].([]any)
	arr = append(arr, map[string]any{
		"hooks": []any{map[string]any{"type": "command", "command": command}},
	})
	hooksMap[event] = arr
	root["hooks"] = hooksMap
	return root
}

// sessionEntry renders a new matcher-less event entry (fires on every session
// or subagent start) with conventional field order.
type sessionEntry struct {
	Hooks []hookCmd `json:"hooks"`
}

func newEventEntry(command string) sessionEntry {
	return sessionEntry{Hooks: []hookCmd{{Type: "command", Command: command}}}
}

// reDetectEvent re-parses cur (the output of a previous splice) and reports
// whether "hooks" and hooks.<event> now exist, falling back to the caller's
// pre-splice knowledge when cur does not parse.
func reDetectEvent(cur []byte, event string, hadHooks, hadEvent bool) (hasHooks, hasEvent bool) {
	hasHooks, hasEvent = hadHooks, hadEvent
	var curRoot map[string]any
	if json.Unmarshal(cur, &curRoot) == nil {
		hm, _ := curRoot["hooks"].(map[string]any)
		hasHooks = hm != nil
		if hm != nil {
			_, hasEvent = hm[event]
		}
	}
	return hasHooks, hasEvent
}

// spliceEventEntry returns raw with a matcher-less event entry surgically
// inserted: appended to hooks.<event> when it exists, else <event> added to
// the hooks object, else hooks added to the root. ok=false when the target
// span cannot be located (caller falls back to a render).
func spliceEventEntry(raw []byte, event string, entry sessionEntry, hasHooks, hasEvent bool, nl, unit string) ([]byte, bool) {
	entryText, err := marshalIndentNoHTML(entry, "", unit)
	if err != nil {
		return nil, false
	}
	switch {
	case hasEvent:
		start, end, empty, ok := findSpan(raw, "hooks", event)
		if !ok {
			return nil, false
		}
		return spliceArrayAppend(raw, start, end, empty, string(entryText), nl, unit), true
	case hasHooks:
		start, end, empty, ok := findSpan(raw, "hooks")
		if !ok {
			return nil, false
		}
		arrText, err := marshalIndentNoHTML([]sessionEntry{entry}, "", unit)
		if err != nil {
			return nil, false
		}
		return spliceObjectAppend(raw, start, end, empty, event, string(arrText), nl, unit), true
	default:
		start, end, empty, ok := findSpan(raw)
		if !ok {
			return nil, false
		}
		hooksText, err := marshalIndentNoHTML(map[string][]sessionEntry{event: {entry}}, "", unit)
		if err != nil {
			return nil, false
		}
		return spliceObjectAppend(raw, start, end, empty, "hooks", string(hooksText), nl, unit), true
	}
}

// actorEnvKey/actorEnvValue are the env marker `cerne hook install` writes
// (F-0131 crit. 11): CERNE_ACTOR=agent, so app.EventActor/CheckHumanGate can
// tell an agent session apart from a human terminal (which never loads this
// settings.json).
const (
	actorEnvKey   = "CERNE_ACTOR"
	actorEnvValue = "agent"
)

// withActorEntry ensures root carries env.CERNE_ACTOR=agent, creating the env
// object as needed and preserving any other env key already present.
func withActorEntry(root map[string]any) map[string]any {
	envMap, _ := root["env"].(map[string]any)
	if envMap == nil {
		envMap = map[string]any{}
	} else {
		clone := make(map[string]any, len(envMap)+1)
		for k, v := range envMap {
			clone[k] = v
		}
		envMap = clone
	}
	envMap[actorEnvKey] = actorEnvValue
	root["env"] = envMap
	return root
}

// actorPresent reports whether env already carries CERNE_ACTOR=agent — the
// idempotency check for the actor merge.
func actorPresent(envMap map[string]any) bool {
	if envMap == nil {
		return false
	}
	v, ok := envMap[actorEnvKey]
	if !ok {
		return false
	}
	s, ok := v.(string)
	return ok && s == actorEnvValue
}

// spliceActor returns raw with env.CERNE_ACTOR=agent surgically inserted:
// appended to the existing env object when present, else a new top-level env
// object added to the root. ok=false if the target span cannot be located
// (caller falls back to a render).
func spliceActor(raw []byte, hasEnv bool, nl, unit string) ([]byte, bool) {
	if hasEnv {
		start, end, empty, ok := findSpan(raw, "env")
		if !ok {
			return nil, false
		}
		valText, err := marshalIndentNoHTML(actorEnvValue, "", unit)
		if err != nil {
			return nil, false
		}
		return spliceObjectAppendRaw(raw, start, end, empty, quoteKey(actorEnvKey)+": "+string(valText), nl, unit), true
	}
	start, end, empty, ok := findSpan(raw)
	if !ok {
		return nil, false
	}
	envVal := map[string]string{actorEnvKey: actorEnvValue}
	envText, err := marshalIndentNoHTML(envVal, "", unit)
	if err != nil {
		return nil, false
	}
	return spliceObjectAppend(raw, start, end, empty, "env", string(envText), nl, unit), true
}

// spliceObjectAppendRaw is spliceObjectAppend for a caller that already
// rendered the full "key": value member text (spliceObjectAppend renders the
// value only and prefixes the key itself).
func spliceObjectAppendRaw(raw []byte, start, end int, empty bool, member, nl, unit string) []byte {
	objIndent := lineIndent(raw, start)
	memberIndent := objIndent + unit
	member = indentBlock(member, memberIndent)
	var newObj string
	if empty {
		newObj = "{" + nlify("\n"+memberIndent+member+"\n"+objIndent, nl) + "}"
	} else {
		inner := string(raw[start+1 : end-1])
		kept := strings.TrimRight(inner, " \t\r\n")
		origTrail := inner[len(kept):]
		frag := nlify(","+"\n"+memberIndent+member, nl)
		newObj = "{" + kept + frag + origTrail + "}"
	}
	return concat(raw, start, end, newObj)
}

// hookCmd and matcherEntry render the new PostToolUse entry with conventional
// field order (matcher before hooks; type before command) — a map would sort the
// keys alphabetically. Field order affects only the emitted bytes, not the
// semantic verification (which compares parsed maps).
type hookCmd struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

type matcherEntry struct {
	Matcher string    `json:"matcher"`
	Hooks   []hookCmd `json:"hooks"`
}

func newMatcherEntry(matcher, command string) matcherEntry {
	return matcherEntry{
		Matcher: matcher,
		Hooks:   []hookCmd{{Type: "command", Command: command}},
	}
}

// hookPresent reports whether any PostToolUse entry already wires our script (by
// command substring), which makes install idempotent.
func hookPresent(entries []any) bool {
	return scriptWired(entries, hookScript)
}

// scriptWired reports whether any hook entry in entries already runs script
// (by command substring) — the shared idempotency check for hook wiring.
func scriptWired(entries []any, script string) bool {
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
			if cmd, ok := hm["command"].(string); ok && strings.Contains(cmd, script) {
				return true
			}
		}
	}
	return false
}

// spliceMatcherEntry returns raw with a matcher-ful event entry surgically
// inserted, choosing the insertion site from the existing structure: append to
// hooks.<event> when it exists, else add <event> to the hooks object, else add
// hooks to the root. ok=false if the target span cannot be located (caller
// falls back to a render).
func spliceMatcherEntry(raw []byte, event string, entry matcherEntry, hasHooks, hasEvent bool, nl, unit string) ([]byte, bool) {
	entryText, err := marshalIndentNoHTML(entry, "", unit)
	if err != nil {
		return nil, false
	}
	switch {
	case hasEvent:
		start, end, empty, ok := findSpan(raw, "hooks", event)
		if !ok {
			return nil, false
		}
		return spliceArrayAppend(raw, start, end, empty, string(entryText), nl, unit), true
	case hasHooks:
		start, end, empty, ok := findSpan(raw, "hooks")
		if !ok {
			return nil, false
		}
		arrText, err := marshalIndentNoHTML([]matcherEntry{entry}, "", unit)
		if err != nil {
			return nil, false
		}
		return spliceObjectAppend(raw, start, end, empty, event, string(arrText), nl, unit), true
	default:
		start, end, empty, ok := findSpan(raw)
		if !ok {
			return nil, false
		}
		hooksText, err := marshalIndentNoHTML(map[string][]matcherEntry{event: {entry}}, "", unit)
		if err != nil {
			return nil, false
		}
		return spliceObjectAppend(raw, start, end, empty, "hooks", string(hooksText), nl, unit), true
	}
}

// findSpan locates the JSON value at the given object-key path (empty path = the
// root value) and returns the byte offset of its opening delimiter (start), the
// offset just past its closing delimiter (end), and whether the container is
// empty. It walks tokens with a json.Decoder, so arbitrary whitespace is handled
// correctly. ok=false if the path is absent. raw must be valid JSON (the caller
// validates it before calling).
func findSpan(raw []byte, path ...string) (start, end int, empty, ok bool) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	type frame struct {
		isObj     bool
		expectKey bool // object: the next scalar token is a key, not a value
		childSeen bool // container held at least one child token
		openOff   int
		pushedKey bool // this frame appended to keyPath
	}
	var stack []frame
	var keyPath []string
	var pendingKey string // key most recently read in the innermost object
	haveTarget := false
	targetDepth := 0
	targetOpen := 0
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, 0, false, false
		}
		off := int(dec.InputOffset())
		switch t := tok.(type) {
		case json.Delim:
			switch t {
			case '{', '[':
				hasKey := false
				var key string
				if len(stack) > 0 {
					top := &stack[len(stack)-1]
					top.childSeen = true
					if top.isObj {
						key = pendingKey
						hasKey = true
					}
				}
				if hasKey {
					keyPath = append(keyPath, key)
				}
				if !haveTarget && pathEqual(keyPath, path) {
					haveTarget = true
					targetDepth = len(stack) + 1
					targetOpen = off - 1
				}
				stack = append(stack, frame{isObj: t == '{', expectKey: t == '{', openOff: off - 1, pushedKey: hasKey})
			case '}', ']':
				top := stack[len(stack)-1]
				if haveTarget && len(stack) == targetDepth {
					return targetOpen, off, !top.childSeen, true
				}
				stack = stack[:len(stack)-1]
				if top.pushedKey {
					keyPath = keyPath[:len(keyPath)-1]
				}
				if len(stack) > 0 && stack[len(stack)-1].isObj {
					stack[len(stack)-1].expectKey = true
				}
			}
		default:
			if len(stack) > 0 {
				top := &stack[len(stack)-1]
				top.childSeen = true
				if top.isObj {
					if top.expectKey {
						pendingKey, _ = tok.(string)
						top.expectKey = false
					} else {
						top.expectKey = true
					}
				}
			}
		}
	}
	return 0, 0, false, false
}

func pathEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// spliceArrayAppend appends elemText as a new last element of the array spanning
// [start,end), reusing the existing element bytes verbatim (only the previous
// last element gains a trailing comma). elemText is rendered at base indentation;
// it is re-indented to sit one unit deeper than the array's own line.
func spliceArrayAppend(raw []byte, start, end int, empty bool, elemText, nl, unit string) []byte {
	keyIndent := lineIndent(raw, start)
	elementIndent := keyIndent + unit
	block := indentBlock(elemText, elementIndent)
	var newArr string
	if empty {
		newArr = "[" + nlify("\n"+elementIndent+block+"\n"+keyIndent, nl) + "]"
	} else {
		inner := string(raw[start+1 : end-1])
		kept := strings.TrimRight(inner, " \t\r\n")
		origTrail := inner[len(kept):]
		frag := nlify(","+"\n"+elementIndent+block, nl)
		newArr = "[" + kept + frag + origTrail + "]"
	}
	return concat(raw, start, end, newArr)
}

// spliceObjectAppend inserts a "key": valueText member as the last entry of the
// object spanning [start,end), reusing existing member bytes verbatim (only the
// previous last member gains a trailing comma). valueText is rendered at base
// indentation and re-indented to the object's member level.
func spliceObjectAppend(raw []byte, start, end int, empty bool, key, valueText, nl, unit string) []byte {
	objIndent := lineIndent(raw, start)
	memberIndent := objIndent + unit
	member := quoteKey(key) + ": " + indentBlock(valueText, memberIndent)
	var newObj string
	if empty {
		newObj = "{" + nlify("\n"+memberIndent+member+"\n"+objIndent, nl) + "}"
	} else {
		inner := string(raw[start+1 : end-1])
		kept := strings.TrimRight(inner, " \t\r\n")
		origTrail := inner[len(kept):]
		frag := nlify(","+"\n"+memberIndent+member, nl)
		newObj = "{" + kept + frag + origTrail + "}"
	}
	return concat(raw, start, end, newObj)
}

// concat replaces raw[start:end) with mid.
func concat(raw []byte, start, end int, mid string) []byte {
	out := make([]byte, 0, len(raw)-(end-start)+len(mid))
	out = append(out, raw[:start]...)
	out = append(out, mid...)
	out = append(out, raw[end:]...)
	return out
}

// quoteKey renders a JSON object key (no HTML escaping).
func quoteKey(k string) string {
	b, _ := marshalIndentNoHTML(k, "", "")
	return string(b)
}

// marshalIndentNoHTML is json.MarshalIndent with HTML escaping disabled (so &, <,
// > survive as literals) and without the trailing newline json.Encoder appends.
func marshalIndentNoHTML(v any, prefix, indent string) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent(prefix, indent)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// detectNewline reports the newline the file uses ("\r\n" or "\n").
func detectNewline(raw []byte) string {
	if i := bytes.IndexByte(raw, '\n'); i > 0 && raw[i-1] == '\r' {
		return "\r\n"
	}
	return "\n"
}

// detectIndentUnit infers one indentation level from the first indented line
// (default two spaces).
func detectIndentUnit(raw []byte) string {
	for _, ln := range bytes.Split(raw, []byte("\n")) {
		ln = bytes.TrimRight(ln, "\r")
		body := bytes.TrimLeft(ln, " \t")
		if len(body) > 0 && len(body) < len(ln) {
			return string(ln[:len(ln)-len(body)])
		}
	}
	return "  "
}

// lineIndent returns the leading whitespace of the line containing byte off.
func lineIndent(raw []byte, off int) string {
	ls := bytes.LastIndexByte(raw[:off], '\n') + 1
	i := ls
	for i < len(raw) && (raw[i] == ' ' || raw[i] == '\t') {
		i++
	}
	return string(raw[ls:i])
}

// indentBlock prefixes every line after the first with indent.
func indentBlock(s, indent string) string {
	return strings.ReplaceAll(s, "\n", "\n"+indent)
}

// nlify rewrites "\n" to the file's newline (no-op for "\n").
func nlify(s, nl string) string {
	if nl == "\n" {
		return s
	}
	return strings.ReplaceAll(s, "\n", nl)
}
