package project

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/BurntSushi/toml"
)

// Setting scopes, in precedence order, and the origin of a value no scope sets
// (F-0314). The project scope is the committed .cernerc; the host scope is
// settings.toml in ConfigDir, outside every repository.
const (
	ScopeProject  = "project"
	ScopeHost     = "host"
	OriginDefault = "default"
)

const hostSettingsFilename = "settings.toml"

// Refusals of a settings read or write (F-0314).
var (
	ErrUnknownSetting      = errors.New("unknown setting")
	ErrInvalidSettingValue = errors.New("invalid setting value")
	ErrSettingScope        = errors.New("unknown scope")
	// ErrSettingNotSet: an unset of a key the scope's file does not hold.
	ErrSettingNotSet = errors.New("nothing to unset")
	// ErrConfigBusy: another writer held the file's lock past
	// configLockTimeout. Nothing was written.
	ErrConfigBusy = errors.New("config file is locked by another writer")
)

// configLockTimeout bounds the wait for another writer's <file>.lock.
var configLockTimeout = 2 * time.Second

// Setting is one declared configuration key. ID is "table.key", the TOML
// address in both scopes (git config's section.key, R-0043).
type Setting struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Type        string   `json:"type"`
	Default     any      `json:"default"`
	Scopes      []string `json:"scopes"`
	Description string   `json:"description"`
}

// ResolvedSetting is a Setting with its effective value and the scope it came
// from: ScopeProject, ScopeHost or OriginDefault. Stored holds the valid
// value each scope that sets the key stores, keyed by scope, so a value a
// higher scope overrides stays visible; Invalid lists the stored values of
// this key the resolution skipped, in any scope.
type ResolvedSetting struct {
	Setting
	Value   any              `json:"value"`
	Origin  string           `json:"origin"`
	Stored  map[string]any   `json:"stored,omitempty"`
	Invalid []SettingProblem `json:"invalid,omitempty"`
	// Fallback is the value used when this one is empty (F-0333). Only the
	// /settings endpoint fills it, on every answer; ResolveSettings never does.
	Fallback string `json:"fallback,omitempty"`
}

// SettingProblem is what the resolution skipped instead of failing: a stored
// value outside the key's type (Raw is its TOML text) or a whole scope file it
// could not read.
type SettingProblem struct {
	Scope string `json:"scope"`
	File  string `json:"file"`
	Raw   string `json:"raw,omitempty"`
	Error string `json:"error"`
}

// SettingsView is every declared setting resolved for one project, plus the
// file behind each scope (keyed by scope; no project key outside a project)
// and the scope files that could not be read.
type SettingsView struct {
	Settings []ResolvedSetting `json:"settings"`
	Files    map[string]string `json:"files"`
	Errors   []SettingProblem  `json:"errors,omitempty"`
}

// settingsRegistry is the one place a setting is declared: the CLI, the
// endpoint and the UI all derive from it. Kept in id order.
var settingsRegistry = []Setting{
	{
		ID: "channel.human_name", Title: "Your name in the channel", Type: "string", Default: "",
		Scopes:      []string{ScopeHost},
		Description: "The name that signs the messages you send from the UI on a project's permanent channel. It applies to every project on this machine. It is not the project's name, which is set in the “Project name” field of the Settings screen. Empty uses your OS user name.",
	},
	{
		ID: "graph.enabled", Title: "Graph", Type: "bool", Default: true,
		Scopes:      []string{ScopeProject, ScopeHost},
		Description: "Shows the Graph tab in the UI. Off hides it.",
	},
	{
		ID: "mindmap.enabled", Title: "Mindmap", Type: "bool", Default: true,
		Scopes:      []string{ScopeProject, ScopeHost},
		Description: "Shows the Mindmap tab in the UI. Off hides it.",
	},
	{
		ID: "wiki.enabled", Title: "Wiki", Type: "bool", Default: true,
		Scopes:      []string{ScopeProject, ScopeHost},
		Description: "Off stops compile_on_push and turns wiki lint debt into advisory findings left out of 'cerne next'; the wiki stays readable, with a stale banner and an on-demand recompile.",
	},
}

// WikiEnabled is wiki.enabled for the project at (or above) projDir (F-0316):
// false only when it resolves to false. A value the resolution skipped — an
// invalid one, or a scope file it could not read — falls through as F-0314
// resolves it, but never silently: warning is then one line naming each
// problem and the value used instead ("" when there is none).
func WikiEnabled(projDir string) (on bool, warning string) {
	view := ResolveSettings(projDir)
	for _, s := range view.Settings {
		if s.ID != "wiki.enabled" {
			continue
		}
		var problems []string
		for _, p := range s.Invalid {
			problems = append(problems, fmt.Sprintf("invalid in %s scope (%s)", p.Scope, p.File))
		}
		for _, p := range view.Errors {
			problems = append(problems, fmt.Sprintf("unreadable in %s scope (%s)", p.Scope, p.File))
		}
		if len(problems) > 0 {
			warning = fmt.Sprintf("warning: wiki.enabled %s; using %s %v", strings.Join(problems, ", "), s.Origin, s.Value)
		}
		return s.Value != false, warning
	}
	return true, ""
}

// ResolveSettings resolves every declared setting for the project at (or
// above) projDir: project > host > default. It never fails as a whole: an
// unreadable scope file is reported once in Errors and leaves that scope
// empty; a stored value outside the key's type or in a scope the key does not
// declare (F-0333: a .cernerc naming a host-only key) is reported on that key,
// which falls through to the next valid scope. No .cernerc leaves the project
// scope empty; an absent host file leaves the host scope empty.
func ResolveSettings(projDir string) SettingsView {
	scopes := []string{ScopeProject, ScopeHost}
	view := SettingsView{Files: map[string]string{}}
	layers := map[string]map[string]any{}
	for _, scope := range scopes {
		path, err := settingsFile(projDir, scope)
		if scope == ScopeProject && errors.Is(err, ErrNoProject) {
			continue
		}
		if err == nil {
			view.Files[scope] = path
			layers[scope], err = readTOMLFile(path)
		}
		if err != nil {
			view.Errors = append(view.Errors, SettingProblem{Scope: scope, File: path, Error: err.Error()})
		}
	}
	for _, s := range settingsRegistry {
		r := ResolvedSetting{Setting: s, Value: s.Default, Origin: OriginDefault}
		table, key, _ := strings.Cut(s.ID, ".")
		for _, scope := range scopes {
			tbl, _ := layers[scope][table].(map[string]any)
			v, ok := tbl[key]
			if !ok {
				continue
			}
			problem := ""
			if err := checkDeclared(s.ID, scope); err != nil {
				problem = err.Error()
			} else if !hasSettingType(s, v) {
				problem = fmt.Sprintf("%v: want %s", ErrInvalidSettingValue, s.Type)
			}
			if problem != "" {
				raw := fmt.Sprint(v)
				if str, isStr := v.(string); isStr {
					raw = strconv.Quote(str)
				}
				r.Invalid = append(r.Invalid, SettingProblem{Scope: scope, File: view.Files[scope], Raw: raw, Error: problem})
				continue
			}
			if r.Stored == nil {
				r.Stored = map[string]any{}
			}
			r.Stored[scope] = v
			if r.Origin == OriginDefault {
				r.Value, r.Origin = v, scope
			}
		}
		view.Settings = append(view.Settings, r)
	}
	return view
}

// hasSettingType reports whether v is a value of s's declared type.
func hasSettingType(s Setting, v any) bool {
	switch v.(type) {
	case bool:
		return s.Type == "bool"
	case string:
		return s.Type == "string"
	}
	return false
}

// maxSettingString bounds a string setting (a display name, F-0325).
const maxSettingString = 64

// SetSetting writes the CLI literal raw as id's value in scope: "true"/"false"
// for a bool key, the text itself for a string key. See SetSettingValue.
func SetSetting(projDir, id, scope, raw string) error {
	if err := checkDeclared(id, scope); err != nil {
		return err
	}
	var v any = raw
	if declared(id).Type == "bool" {
		if raw != "true" && raw != "false" {
			return fmt.Errorf("%s: %w %q: want true or false", id, ErrInvalidSettingValue, raw)
		}
		v = raw == "true"
	}
	return SetSettingValue(projDir, id, scope, v)
}

// SetSettingValue writes v as id's value in scope: the project's .cernerc or
// the host settings.toml, editing only that key's line. Key, scope and value
// (its type; a string is one non-blank line of at most 64 runes) are
// validated before any file is read, so a refusal writes nothing.
func SetSettingValue(projDir, id, scope string, v any) error {
	if err := checkDeclared(id, scope); err != nil {
		return err
	}
	s := declared(id)
	if !hasSettingType(s, v) {
		return fmt.Errorf("%s: %w %v: want %s", id, ErrInvalidSettingValue, v, s.Type)
	}
	if str, ok := v.(string); ok && (strings.TrimSpace(str) == "" || strings.ContainsAny(str, "\r\n") || utf8.RuneCountInString(str) > maxSettingString) {
		return fmt.Errorf("%s: %w %q: want one non-blank line of at most %d characters", id, ErrInvalidSettingValue, str, maxSettingString)
	}
	path, err := settingsFile(projDir, scope)
	if err != nil {
		return err
	}
	table, key, _ := strings.Cut(id, ".")
	_, err = editTOMLFile(path, table, key, v)
	return err
}

// UnsetSetting removes id from scope's file, handing the key back to the next
// scope down (host, then the default). A key the file does not hold is
// ErrSettingNotSet, and nothing is written.
func UnsetSetting(projDir, id, scope string) error {
	if err := checkDeclared(id, scope); err != nil {
		return err
	}
	path, err := settingsFile(projDir, scope)
	if err != nil {
		return err
	}
	table, key, _ := strings.Cut(id, ".")
	removed, err := editTOMLFile(path, table, key, nil)
	if err == nil && !removed {
		return fmt.Errorf("%w: %s is not set in %s scope", ErrSettingNotSet, id, scope)
	}
	return err
}

// declared returns id's declaration; id must have passed checkDeclared.
func declared(id string) Setting {
	return settingsRegistry[slices.IndexFunc(settingsRegistry, func(s Setting) bool { return s.ID == id })]
}

// checkDeclared refuses an id the registry does not declare, or a scope the
// setting does not allow.
func checkDeclared(id, scope string) error {
	i := slices.IndexFunc(settingsRegistry, func(s Setting) bool { return s.ID == id })
	if i < 0 {
		ids := make([]string, len(settingsRegistry))
		for j, s := range settingsRegistry {
			ids[j] = s.ID
		}
		return fmt.Errorf("%w %q (known: %s)", ErrUnknownSetting, id, strings.Join(ids, ", "))
	}
	if scopes := settingsRegistry[i].Scopes; !slices.Contains(scopes, scope) {
		return fmt.Errorf("%w %q (allowed: %s)", ErrSettingScope, scope, strings.Join(scopes, ", "))
	}
	return nil
}

// editTOMLFile applies editTOML to the file at path (absent = empty) under
// git's lockfile discipline, and reports whether it wrote: <path>.lock, created O_EXCL, is the lock and
// receives the new content, then is renamed over the file. Writers in this
// process (the ui) and in others (the CLI) serialize instead of losing
// updates; one still blocked after configLockTimeout gets ErrConfigBusy and
// writes nothing. A foreign lock is never removed: a crashed writer's stale
// lock is the user's to delete, as with git. No change skips the write. Like
// writeFileAtomic, it writes through a symlink and keeps the file's mode.
func editTOMLFile(path, table, key string, value any) (bool, error) {
	if _, err := os.Stat(path); value == nil && errors.Is(err, fs.ErrNotExist) {
		return false, nil // removing from a missing file: nothing to lock or create
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	// Write to the symlink's target; messages keep the path the user gave.
	target, mode, err := writeTarget(path)
	if err != nil {
		return false, err
	}
	lock := target + ".lock"
	deadline := time.Now().Add(configLockTimeout)
	f, err := os.OpenFile(lock, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o666)
	for errors.Is(err, fs.ErrExist) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		f, err = os.OpenFile(lock, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o666)
	}
	if errors.Is(err, fs.ErrExist) {
		return false, fmt.Errorf("%s: %w; if no cerne is running, remove %s", path, ErrConfigBusy, lock)
	}
	if err != nil {
		return false, err
	}
	renamed := false
	defer func() {
		f.Close()
		if !renamed {
			os.Remove(lock)
		}
	}()
	if err := f.Chmod(mode); err != nil {
		return false, err
	}

	raw, err := os.ReadFile(target)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	out, err := editTOML(raw, table, key, value)
	if err != nil {
		return false, fmt.Errorf("%s: %w", path, err)
	}
	if bytes.Equal(out, raw) {
		return false, nil
	}
	if _, err := f.Write(out); err != nil {
		return false, err
	}
	if err := f.Close(); err != nil {
		return false, err
	}
	if err := os.Rename(lock, target); err != nil {
		return false, err
	}
	renamed = true
	return true, nil
}

// settingsFile returns the file behind scope: the .cernerc found from
// projDir (not decoded: settings read it generically), or
// <ConfigDir>/settings.toml.
func settingsFile(projDir, scope string) (string, error) {
	if scope == ScopeHost {
		dir, err := ConfigDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(dir, hostSettingsFilename), nil
	}
	dir, err := findConfigDir(projDir)
	if err != nil {
		return "", fmt.Errorf("project scope: %w", err)
	}
	return filepath.Join(dir, configFilename), nil
}

// readTOMLFile decodes path as a generic TOML document; absent reads as empty.
func readTOMLFile(path string) (map[string]any, error) {
	m := map[string]any{}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return m, nil
	}
	if err != nil {
		return nil, err
	}
	if err := toml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrConfigParse, err)
	}
	return m, nil
}
