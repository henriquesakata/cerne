package domain

import (
	"errors"
	"fmt"
	"path"
	"strings"
	"unicode"
)

// Declared note triggers (F-0457): the events a note asks to show up on.
// Two forms — edit:<path> (an exact repo-relative path or <dir>/**, the
// [component_paths] glob language) and bash:<words> (consecutive words of a
// shell command).

const (
	triggerEdit = "edit:"
	triggerBash = "bash:"
)

// triggerForms is the hint every refusal carries.
const triggerForms = "accepted forms: edit:<path> (an exact repo-relative path or <dir>/**) and bash:<words> (e.g. bash:git tag)"

var (
	// ErrTriggerKind is a trigger with neither the edit: nor the bash: prefix.
	ErrTriggerKind = errors.New("unknown trigger kind")
	// ErrTriggerPath is an edit: path that is absolute, escapes the repository
	// or uses a glob outside the trailing /**.
	ErrTriggerPath = errors.New("invalid trigger path")
	// ErrTriggerEmpty is a trigger with nothing after its prefix.
	ErrTriggerEmpty = errors.New("empty trigger")
)

// NormalizeTriggers validates each trigger and returns them in canonical form,
// blanks dropped and duplicates removed (order-preserving): edit: paths with
// forward slashes and no leading "./", bash: words joined by one space. Any
// invalid trigger yields an error naming it and no partial result.
func NormalizeTriggers(in []string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, raw := range in {
		t := strings.TrimSpace(raw)
		if t == "" {
			continue
		}
		var norm string
		switch {
		case strings.HasPrefix(t, triggerEdit):
			p, err := normalizeTriggerPath(strings.TrimSpace(strings.TrimPrefix(t, triggerEdit)))
			if err != nil {
				return nil, fmt.Errorf("%w: %q — %s", err, raw, triggerForms)
			}
			norm = triggerEdit + p
		case strings.HasPrefix(t, triggerBash):
			words := strings.Fields(strings.TrimPrefix(t, triggerBash))
			if len(words) == 0 {
				return nil, fmt.Errorf("%w: %q — %s", ErrTriggerEmpty, raw, triggerForms)
			}
			norm = triggerBash + strings.Join(words, " ")
		default:
			return nil, fmt.Errorf("%w: %q — %s", ErrTriggerKind, raw, triggerForms)
		}
		if !seen[norm] {
			seen[norm] = true
			out = append(out, norm)
		}
	}
	return out, nil
}

func normalizeTriggerPath(p string) (string, error) {
	p = strings.ReplaceAll(p, `\`, "/")
	for strings.HasPrefix(p, "./") {
		p = p[2:]
	}
	switch {
	case p == "":
		return "", ErrTriggerEmpty
	case strings.HasPrefix(p, "/"), len(p) > 1 && p[1] == ':':
		return "", ErrTriggerPath // absolute (POSIX or a Windows drive)
	case strings.HasSuffix(p, "/"):
		return "", ErrTriggerPath // a bare dir never matches a file: <dir>/**
	}
	base := strings.TrimSuffix(p, "/**")
	if base == "" || base == "**" || strings.Contains(base, "*") {
		return "", ErrTriggerPath
	}
	for _, seg := range strings.Split(base, "/") {
		if seg == ".." || seg == "." || seg == "" {
			return "", ErrTriggerPath
		}
	}
	return p, nil
}

// MatchGlob reports whether the slash path matches glob: an exact path, or a
// "dir/**" suffix that matches anything under dir. Broader glob semantics
// (intra-segment '*') are out of scope (F-0113, F-0457).
func MatchGlob(glob, path string) bool {
	if glob == path {
		return true
	}
	if strings.HasSuffix(glob, "/**") {
		prefix := strings.TrimSuffix(glob, "**") // keep the trailing slash: "dir/"
		return strings.HasPrefix(path, prefix)
	}
	return false
}

// PathTriggerScore rates how specifically trig matches the repo-relative
// slash path rel: 2 for an exact edit: path, 1 for an edit: <dir>/**, 0 when
// it does not match (or trig is not an edit: trigger).
func PathTriggerScore(trig, rel string) int {
	g, ok := strings.CutPrefix(trig, triggerEdit)
	if !ok || !MatchGlob(g, rel) {
		return 0
	}
	if g == rel {
		return 2
	}
	return 1
}

// CommandTriggerWords is the number of words of the bash: trigger trig when
// they occur as consecutive words of cmd, 0 otherwise (or when trig is not a
// bash: trigger). cmd is split on whitespace and on the shell operators
// ; & | ( ), quotes are not interpreted, and a word invoked by path
// (./bin/cerne) also answers to its base name.
func CommandTriggerWords(trig, cmd string) int {
	w, ok := strings.CutPrefix(trig, triggerBash)
	if !ok {
		return 0
	}
	want := strings.Fields(w)
	if len(want) == 0 {
		return 0
	}
	got := strings.FieldsFunc(cmd, func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune(";&|()", r)
	})
	for i := 0; i+len(want) <= len(got); i++ {
		match := true
		for j, ww := range want {
			g := got[i+j]
			if g != ww && (j != 0 || path.Base(g) != ww) {
				match = false
				break
			}
		}
		if match {
			return len(want)
		}
	}
	return 0
}
