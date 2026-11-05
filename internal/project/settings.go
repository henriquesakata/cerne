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
	ErrSettingScope        = errors.New("scope not allowed for setting")
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
// from: ScopeProject, ScopeHost or OriginDefault. Invalid lists the stored
// values of this key the resolution skipped, in any scope.
type ResolvedSetting struct {
	Setting
	Value   any              `json:"value"`
	Origin  string           `json:"origin"`
	Invalid []SettingProblem `json:"invalid,omitempty"`
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
		ID: "graph.enabled", Title: "Graph", Type: "bool", Default: true,
		Scopes:      []string{ScopeProject, ScopeHost},
		Description: "Not wired yet (F-0315): off will hide the Graph tab in the UI. Today it changes nothing.",
	},
	{
		ID: "mindmap.enabled", Title: "Mindmap", Type: "bool", Default: true,
		Scopes:      []string{ScopeProject, ScopeHost},
		Description: "Shows the Mindmap tab in the UI. Off hides it.",
	},
	{
		ID: "wiki.enabled", Title: "Wiki", Type: "bool", Default: true,
		Scopes:      []string{ScopeProject, ScopeHost},
		Description: "Not wired yet (F-0316): off will stop compiling the wiki on push and counting its debt as pending work. Today it changes nothing.",
	},
}

// ResolveSettings resolves every declared setting for the project at (or
// above) projDir: project > host > default. It never fails as a whole: an
// unreadable scope file is reported once in Errors and leaves that scope
// empty; a stored value outside the key's type is reported on that key, which
// falls through to the next valid scope. No .cernerc leaves the project
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
			_, isBool := v.(bool)
			switch {
			case !ok:
			case !isBool:
				raw := fmt.Sprint(v)
				if str, isStr := v.(string); isStr {
					raw = strconv.Quote(str)
				}
				r.Invalid = append(r.Invalid, SettingProblem{Scope: scope, File: view.Files[scope], Raw: raw,
					Error: fmt.Sprintf("%v: want %s", ErrInvalidSettingValue, s.Type)})
			case r.Origin == OriginDefault:
				r.Value, r.Origin = v, scope
			}
		}
		view.Settings = append(view.Settings, r)
	}
	return view
}

// SetSetting writes raw ("true"/"false") as id's value in scope: the
// project's .cernerc or the host settings.toml, editing only that key's line.
// Key, scope and value are validated before any file is read, so a refusal
// writes nothing.
func SetSetting(projDir, id, scope, raw string) error {
	if err := checkDeclared(id, scope); err != nil {
		return err
	}
	// Every declared setting is a bool; a new type adds its parse here.
	if raw != "true" && raw != "false" {
		return fmt.Errorf("%s: %w %q: want true or false", id, ErrInvalidSettingValue, raw)
	}
	path, err := settingsFile(projDir, scope)
	if err != nil {
		return err
	}
	table, key, _ := strings.Cut(id, ".")
	return editTOMLFile(path, table, key, raw == "true")
}

// UnsetSetting removes id from scope's file, handing the key back to the next
// scope down (host, then the default). The table header stays; a key the file
// does not hold is a no-op that writes nothing.
func UnsetSetting(projDir, id, scope string) error {
	if err := checkDeclared(id, scope); err != nil {
		return err
	}
	path, err := settingsFile(projDir, scope)
	if err != nil {
		return err
	}
	table, key, _ := strings.Cut(id, ".")
	return editTOMLFile(path, table, key, nil)
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
		return fmt.Errorf("%s: %w: %q (allowed: %s)", id, ErrSettingScope, scope, strings.Join(scopes, ", "))
	}
	return nil
}

// editTOMLFile applies editTOML to the file at path (absent = empty) under
// git's lockfile discipline: <path>.lock, created O_EXCL, is the lock and
// receives the new content, then is renamed over the file. Writers in this
// process (the ui) and in others (the CLI) serialize instead of losing
// updates; one still blocked after configLockTimeout gets ErrConfigBusy and
// writes nothing. A foreign lock is never removed: a crashed writer's stale
// lock is the user's to delete, as with git. No change skips the write. Like
// writeFileAtomic, it writes through a symlink and keeps the file's mode.
func editTOMLFile(path, table, key string, value any) error {
	if _, err := os.Stat(path); value == nil && errors.Is(err, fs.ErrNotExist) {
		return nil // removing from a missing file: nothing to lock or create
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	path, mode, err := writeTarget(path)
	if err != nil {
		return err
	}
	lock := path + ".lock"
	deadline := time.Now().Add(configLockTimeout)
	f, err := os.OpenFile(lock, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o666)
	for errors.Is(err, fs.ErrExist) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		f, err = os.OpenFile(lock, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o666)
	}
	if errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("%s: %w; if no cerne is running, remove %s", path, ErrConfigBusy, lock)
	}
	if err != nil {
		return err
	}
	renamed := false
	defer func() {
		f.Close()
		if !renamed {
			os.Remove(lock)
		}
	}()
	if err := f.Chmod(mode); err != nil {
		return err
	}

	raw, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	out, err := editTOML(raw, table, key, value)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if bytes.Equal(out, raw) {
		return nil
	}
	if _, err := f.Write(out); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(lock, path); err != nil {
		return err
	}
	renamed = true
	return nil
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
