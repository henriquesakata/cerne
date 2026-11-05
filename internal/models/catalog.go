// Package models is the user-level model catalog (F-0311): the single source of
// truth for which models exist, how they read, which effort levels each accepts
// and how each is offered. Two layers resolve it — a baseline embedded in the
// binary (works offline and on first boot) and a global file in the user's
// profile (hand-editable, fed by the refresh). Pure: no network, no project.
package models

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// FileName is the global layer's file inside the user-level config dir.
const FileName = "models.json"

// Visibility values: current entries lead the menu, legacy ones live in a
// submenu (still selectable), delisted ones are never offered but still resolve
// a label for history (a result stamped years ago must keep reading).
const (
	Current  = "current"
	Legacy   = "legacy"
	Delisted = "delisted"
)

var (
	// ErrEmpty: the global file exists but holds nothing.
	ErrEmpty = errors.New("model catalog file is empty")
	// ErrMalformed: the global file is not a valid catalog.
	ErrMalformed = errors.New("model catalog file is malformed")
)

// Entry is one model. Efforts is the closed set the model accepts — the menu
// offers exactly these, never a combination the provider would refuse.
type Entry struct {
	ID            string   `json:"id"`
	Label         string   `json:"label,omitempty"`
	Provider      string   `json:"provider,omitempty"`
	Efforts       []string `json:"efforts,omitempty"`
	DefaultEffort string   `json:"default_effort,omitempty"`
	Visibility    string   `json:"visibility,omitempty"`
}

// Catalog is a whole layer or the resolved catalog. UpdatedAt and Listed are
// written by the refresh (the 24h window reads UpdatedAt; Listed marks that a
// provider listed successfully at least once). PreferredProvider is the chat's
// provider choice, global like the catalog itself.
type Catalog struct {
	UpdatedAt         time.Time       `json:"updated_at,omitzero"`
	Listed            map[string]bool `json:"listed,omitempty"`
	PreferredProvider string          `json:"preferred_provider,omitempty"`
	DefaultModel      string          `json:"default_model,omitempty"`
	Entries           []Entry         `json:"entries"`
}

//go:embed baseline.json
var baselineJSON []byte

// Baseline returns a fresh copy of the embedded catalog (callers may mutate it).
func Baseline() Catalog {
	var c Catalog
	if err := json.Unmarshal(baselineJSON, &c); err != nil {
		panic("models: embedded baseline is invalid: " + err.Error())
	}
	return c
}

// Find returns the entry for id.
func (c Catalog) Find(id string) (Entry, bool) {
	for _, e := range c.Entries {
		if e.ID == id {
			return e, true
		}
	}
	return Entry{}, false
}

// Load resolves the catalog from the global file in dir over the baseline. A
// non-nil warn means the global layer was unusable (absent, empty, malformed)
// and the baseline alone is served — the caller logs it, nothing breaks.
func Load(dir string) (cat Catalog, warn error) {
	base := Baseline()
	layer, err := readLayer(dir)
	if err != nil {
		return base, err
	}
	return merge(base, layer), nil
}

// readLayer reads the raw global layer (no baseline merge). An absent file
// yields an error wrapping fs.ErrNotExist.
func readLayer(dir string) (Catalog, error) {
	path := filepath.Join(dir, FileName)
	data, err := os.ReadFile(path)
	if err != nil {
		return Catalog{}, err
	}
	if strings.TrimSpace(string(data)) == "" {
		return Catalog{}, fmt.Errorf("%w: %s", ErrEmpty, path)
	}
	var c Catalog
	if err := json.Unmarshal(data, &c); err != nil {
		return Catalog{}, fmt.Errorf("%w: %s: %v", ErrMalformed, path, err)
	}
	for _, e := range c.Entries {
		switch {
		case e.ID == "":
			return Catalog{}, fmt.Errorf("%w: %s: entry without id", ErrMalformed, path)
		case e.Visibility != "" && e.Visibility != Current && e.Visibility != Legacy && e.Visibility != Delisted:
			return Catalog{}, fmt.Errorf("%w: %s: %s has visibility %q", ErrMalformed, path, e.ID, e.Visibility)
		}
	}
	return c, nil
}

// merge resolves layer over base: an id present in both takes the layer's
// non-empty fields (a hand edit carries only what it changes); ids only in the
// layer are appended. Document fields come from the layer when set.
func merge(base, layer Catalog) Catalog {
	out := base
	out.Entries = append([]Entry(nil), base.Entries...)
	at := make(map[string]int, len(out.Entries))
	for i, e := range out.Entries {
		at[e.ID] = i
	}
	for _, e := range layer.Entries {
		i, ok := at[e.ID]
		if !ok {
			at[e.ID] = len(out.Entries)
			out.Entries = append(out.Entries, e)
			continue
		}
		cur := &out.Entries[i]
		if e.Label != "" {
			cur.Label = e.Label
		}
		if e.Provider != "" {
			cur.Provider = e.Provider
		}
		if e.Efforts != nil {
			cur.Efforts = e.Efforts
		}
		if e.DefaultEffort != "" {
			cur.DefaultEffort = e.DefaultEffort
		}
		if e.Visibility != "" {
			cur.Visibility = e.Visibility
		}
	}
	for i := range out.Entries {
		if out.Entries[i].Visibility == "" {
			out.Entries[i].Visibility = Current
		}
		if out.Entries[i].Label == "" {
			out.Entries[i].Label = out.Entries[i].ID
		}
	}
	out.UpdatedAt = layer.UpdatedAt
	out.Listed = layer.Listed
	out.PreferredProvider = layer.PreferredProvider
	if layer.DefaultModel != "" {
		out.DefaultModel = layer.DefaultModel
	}
	return out
}
