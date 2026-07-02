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
//
// To keep tracked settings.json diffs honest, the entry is spliced into the raw
// bytes surgically: existing key order, indentation, and escaping are preserved,
// and only the new entry (plus a comma on the previous last element) is added —
// not the whole-file reformat a map round-trip through json.MarshalIndent causes.
// The splice is verified to be semantically identical to the map-based merge; on
// any mismatch it falls back to a clean re-render (correct, just a coarser diff),
// so a splicing bug can never write invalid or wrong JSON.
func mergeSettings(raw []byte) ([]byte, bool, error) {
	root := map[string]any{}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) > 0 {
		if err := json.Unmarshal(raw, &root); err != nil {
			return nil, false, fmt.Errorf("hooks: settings.json inválido, não será sobrescrito: %w", err)
		}
	}

	// Detect structure + idempotency from the parsed map (this is also the
	// validity gate: an invalid document already failed above).
	hooksVal, hasHooks := root["hooks"]
	var hooksMap map[string]any
	if hasHooks {
		m, ok := hooksVal.(map[string]any)
		if !ok {
			return nil, false, errors.New(`hooks: settings.json tem "hooks" com tipo inesperado`)
		}
		hooksMap = m
	}
	var post []any
	hasPTU := false
	if hooksMap != nil {
		if pv, ok := hooksMap["PostToolUse"]; ok {
			arr, ok := pv.([]any)
			if !ok {
				return nil, false, errors.New(`hooks: settings.json tem "PostToolUse" com tipo inesperado`)
			}
			post = arr
			hasPTU = true
		}
	}
	if hookPresent(post) {
		return nil, false, nil
	}

	unit := detectIndentUnit(raw)

	// Fresh document: nothing to preserve, emit a clean (escape-safe) render.
	if len(trimmed) == 0 {
		out, err := marshalIndentNoHTML(withHookEntry(map[string]any{}), "", unit)
		if err != nil {
			return nil, false, err
		}
		return append(out, '\n'), true, nil
	}

	// Surgical splice on the raw bytes, verified against the canonical merge.
	expected := withHookEntry(root)
	if spliced, ok := spliceHook(raw, hasHooks, hasPTU, detectNewline(raw), unit); ok {
		var got map[string]any
		if json.Unmarshal(spliced, &got) == nil && reflect.DeepEqual(got, expected) {
			return spliced, true, nil
		}
	}
	// Fallback: correct but coarser diff (escaping still preserved).
	out, err := marshalIndentNoHTML(expected, "", unit)
	if err != nil {
		return nil, false, err
	}
	return append(out, '\n'), true, nil
}

// withHookEntry ensures root carries the PostToolUse/Bash wiring, creating the
// hooks object and PostToolUse array as needed. It is the canonical (key-order-
// agnostic) merge: used to seed a fresh document, to build the fallback render,
// and as the oracle the surgical splice is verified against.
func withHookEntry(root map[string]any) map[string]any {
	hooksMap, _ := root["hooks"].(map[string]any)
	if hooksMap == nil {
		hooksMap = map[string]any{}
	}
	post, _ := hooksMap["PostToolUse"].([]any)
	post = append(post, map[string]any{
		"matcher": "Bash",
		"hooks":   []any{map[string]any{"type": "command", "command": hookCommand}},
	})
	hooksMap["PostToolUse"] = post
	root["hooks"] = hooksMap
	return root
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

func newMatcherEntry() matcherEntry {
	return matcherEntry{
		Matcher: "Bash",
		Hooks:   []hookCmd{{Type: "command", Command: hookCommand}},
	}
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

// spliceHook returns raw with the new entry surgically inserted, choosing the
// insertion site from the existing structure: append to hooks.PostToolUse when it
// exists, else add PostToolUse to the hooks object, else add hooks to the root.
// ok=false if the target span cannot be located (caller falls back to a render).
func spliceHook(raw []byte, hasHooks, hasPTU bool, nl, unit string) ([]byte, bool) {
	entryText, err := marshalIndentNoHTML(newMatcherEntry(), "", unit)
	if err != nil {
		return nil, false
	}
	switch {
	case hasPTU:
		start, end, empty, ok := findSpan(raw, "hooks", "PostToolUse")
		if !ok {
			return nil, false
		}
		return spliceArrayAppend(raw, start, end, empty, string(entryText), nl, unit), true
	case hasHooks:
		start, end, empty, ok := findSpan(raw, "hooks")
		if !ok {
			return nil, false
		}
		arrText, err := marshalIndentNoHTML([]matcherEntry{newMatcherEntry()}, "", unit)
		if err != nil {
			return nil, false
		}
		return spliceObjectAppend(raw, start, end, empty, "PostToolUse", string(arrText), nl, unit), true
	default:
		start, end, empty, ok := findSpan(raw)
		if !ok {
			return nil, false
		}
		hooksVal := struct {
			PostToolUse []matcherEntry `json:"PostToolUse"`
		}{PostToolUse: []matcherEntry{newMatcherEntry()}}
		hooksText, err := marshalIndentNoHTML(hooksVal, "", unit)
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
