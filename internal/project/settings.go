package project

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

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
)

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
// from: ScopeProject, ScopeHost or OriginDefault.
type ResolvedSetting struct {
	Setting
	Value  any    `json:"value"`
	Origin string `json:"origin"`
}

// SettingsView is every declared setting resolved for one project, plus the
// file behind each scope (keyed by scope; no project key outside a project).
type SettingsView struct {
	Settings []ResolvedSetting `json:"settings"`
	Files    map[string]string `json:"files"`
}

// settingsRegistry is the one place a setting is declared: the CLI, the
// endpoint and the UI all derive from it. Kept in id order.
var settingsRegistry = []Setting{
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
		Description: "Compiles the wiki automatically and counts its debt as pending work. Off stops both.",
	},
}

// ResolveSettings resolves every declared setting for the project at (or
// above) projDir: project > host > default. No .cernerc leaves the project
// scope empty; an absent host file leaves the host scope empty.
func ResolveSettings(projDir string) (SettingsView, error) {
	view := SettingsView{Files: map[string]string{}}
	layers := map[string]map[string]any{}
	for _, scope := range []string{ScopeProject, ScopeHost} {
		path, err := settingsFile(projDir, scope)
		if scope == ScopeProject && errors.Is(err, ErrNoProject) {
			continue
		}
		if err != nil {
			return SettingsView{}, err
		}
		m, err := readTOMLFile(path)
		if err != nil {
			return SettingsView{}, err
		}
		view.Files[scope], layers[scope] = path, m
	}
	for _, s := range settingsRegistry {
		r := ResolvedSetting{Setting: s, Value: s.Default, Origin: OriginDefault}
		for _, scope := range []string{ScopeProject, ScopeHost} {
			table, key, _ := strings.Cut(s.ID, ".")
			tbl, _ := layers[scope][table].(map[string]any)
			v, ok := tbl[key]
			if !ok {
				continue
			}
			if _, isBool := v.(bool); !isBool {
				return SettingsView{}, fmt.Errorf("%s: %s = %v: %w: want %s", view.Files[scope], s.ID, v, ErrInvalidSettingValue, s.Type)
			}
			r.Value, r.Origin = v, scope
			break
		}
		view.Settings = append(view.Settings, r)
	}
	return view, nil
}

// SetSetting writes raw ("true"/"false") as id's value in scope: the
// project's .cernerc or the host settings.toml, editing only that key's line.
// Key, scope and value are validated before any file is read, so a refusal
// writes nothing.
//
// ponytail: read-modify-write with no lock, the last writer wins; add a flock if CLI and UI really edit concurrently.
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

// editTOMLFile applies editTOML to the file at path (absent = empty),
// creating its directory, and skips the write when nothing changes.
func editTOMLFile(path, table, key string, value any) error {
	raw, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	out, err := editTOML(raw, table, key, value)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if bytes.Equal(out, raw) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return writeFileAtomic(filepath.Dir(path), filepath.Base(path), out)
}

// settingsFile returns the file behind scope: the .cernerc LoadConfig finds
// from projDir, or <ConfigDir>/settings.toml.
func settingsFile(projDir, scope string) (string, error) {
	if scope == ScopeHost {
		dir, err := ConfigDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(dir, hostSettingsFilename), nil
	}
	_, dir, err := LoadConfig(projDir)
	if err != nil {
		return "", err
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
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return m, nil
}
