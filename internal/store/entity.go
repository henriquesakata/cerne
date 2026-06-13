// Package store persists cerne entities as pretty-printed JSON files
// inside a backend directory tree.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
)

// ErrNotFound is the sentinel for an entity file missing from the backend.
var ErrNotFound = errors.New("entity not found")

// Feature is the persisted form of a feature entity.
type Feature struct {
	ID           string              `json:"id"`
	Title        string              `json:"title"`
	Module       string              `json:"module"`
	State        domain.FeatureState `json:"state"`
	SupersededBy string              `json:"superseded_by,omitempty"`
	Reason       string              `json:"reason,omitempty"`
	CreatedAt    time.Time           `json:"created_at"`
	UpdatedAt    time.Time           `json:"updated_at"`
}

// Decision is the persisted form of a decision entity.
type Decision struct {
	ID           string               `json:"id"`
	Title        string               `json:"title"`
	State        domain.DecisionState `json:"state"`
	SupersededBy string               `json:"superseded_by,omitempty"`
	Reason       string               `json:"reason,omitempty"`
	CreatedAt    time.Time            `json:"created_at"`
	UpdatedAt    time.Time            `json:"updated_at"`
}

// Reference is the persisted form of a reference entity.
type Reference struct {
	ID        string                `json:"id"`
	Title     string                `json:"title"`
	State     domain.ReferenceState `json:"state"`
	CreatedAt time.Time             `json:"created_at"`
	UpdatedAt time.Time             `json:"updated_at"`
}

// Historical is the persisted form of a historical entity: something that
// existed and was superseded/absorbed before it ever had its own spec/case.
// It is born terminal (State is always domain.StateSuperseded) and carries the
// original identifiers as Aliases so the lint can flag live mentions. SupersededBy
// is polymorphic: a cerne ID (e.g. F-0123) or a free label (e.g. "Finance").
type Historical struct {
	ID           string              `json:"id"`
	Name         string              `json:"name"`
	Aliases      []string            `json:"aliases,omitempty"`
	SupersededBy string              `json:"superseded_by"`
	Reason       string              `json:"reason"`
	State        domain.FeatureState `json:"state"`
	ImportedFrom string              `json:"imported_from,omitempty"`
	CreatedAt    time.Time           `json:"created_at"`
}

const (
	featuresSubdir    = "entities/features"
	decisionsSubdir   = "entities/decisions"
	referencesSubdir  = "entities/references"
	historicalsSubdir = "entities/historicals"
)

// SaveFeature writes the feature to entities/features/<ID>.json.
func SaveFeature(backendDir string, f Feature) error {
	return saveEntity(backendDir, featuresSubdir, f.ID, f)
}

// LoadFeature reads one feature by id; ErrNotFound when the file is missing.
func LoadFeature(backendDir, id string) (Feature, error) {
	return loadEntity[Feature](backendDir, featuresSubdir, id)
}

// LoadAllFeatures returns all features sorted by ID ascending.
func LoadAllFeatures(backendDir string) ([]Feature, error) {
	return loadAllEntities[Feature](backendDir, featuresSubdir)
}

// SaveDecision writes the decision to entities/decisions/<ID>.json.
func SaveDecision(backendDir string, d Decision) error {
	return saveEntity(backendDir, decisionsSubdir, d.ID, d)
}

// LoadDecision reads one decision by id; ErrNotFound when the file is missing.
func LoadDecision(backendDir, id string) (Decision, error) {
	return loadEntity[Decision](backendDir, decisionsSubdir, id)
}

// LoadAllDecisions returns all decisions sorted by ID ascending.
func LoadAllDecisions(backendDir string) ([]Decision, error) {
	return loadAllEntities[Decision](backendDir, decisionsSubdir)
}

// SaveReference writes the reference to entities/references/<ID>.json.
func SaveReference(backendDir string, r Reference) error {
	return saveEntity(backendDir, referencesSubdir, r.ID, r)
}

// LoadReference reads one reference by id; ErrNotFound when the file is missing.
func LoadReference(backendDir, id string) (Reference, error) {
	return loadEntity[Reference](backendDir, referencesSubdir, id)
}

// LoadAllReferences returns all references sorted by ID ascending.
func LoadAllReferences(backendDir string) ([]Reference, error) {
	return loadAllEntities[Reference](backendDir, referencesSubdir)
}

// SaveHistorical writes the historical entity to entities/historicals/<ID>.json.
func SaveHistorical(backendDir string, h Historical) error {
	return saveEntity(backendDir, historicalsSubdir, h.ID, h)
}

// LoadHistorical reads one historical entity by id; ErrNotFound when missing.
func LoadHistorical(backendDir, id string) (Historical, error) {
	return loadEntity[Historical](backendDir, historicalsSubdir, id)
}

// LoadAllHistoricals returns all historical entities sorted by ID ascending.
func LoadAllHistoricals(backendDir string) ([]Historical, error) {
	return loadAllEntities[Historical](backendDir, historicalsSubdir)
}

// saveEntity writes the entity to <backendDir>/<subdir>/<id>.json.
func saveEntity(backendDir, subdir, id string, e any) error {
	return writeJSONFile(filepath.Join(backendDir, filepath.FromSlash(subdir)), id+".json", e)
}

// writeJSONFile writes e as pretty-printed JSON (2-space indent, trailing
// newline) atomically: temp file in the same dir, then rename.
func writeJSONFile(dir, filename string, e any) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(dir, filename+".*.tmp")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), filepath.Join(dir, filename)); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}

func loadEntity[T any](backendDir, subdir, id string) (T, error) {
	var e T
	data, err := os.ReadFile(filepath.Join(backendDir, filepath.FromSlash(subdir), id+".json"))
	if errors.Is(err, os.ErrNotExist) {
		return e, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	if err != nil {
		return e, err
	}
	if err := json.Unmarshal(data, &e); err != nil {
		return e, err
	}
	return e, nil
}

func loadAllEntities[T any](backendDir, subdir string) ([]T, error) {
	dir := filepath.Join(backendDir, filepath.FromSlash(subdir))
	dirEntries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return []T{}, nil
	}
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(dirEntries))
	for _, de := range dirEntries {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".json") {
			continue
		}
		ids = append(ids, strings.TrimSuffix(de.Name(), ".json"))
	}
	sort.Strings(ids)
	entities := make([]T, 0, len(ids))
	for _, id := range ids {
		e, err := loadEntity[T](backendDir, subdir, id)
		if err != nil {
			return nil, err
		}
		entities = append(entities, e)
	}
	return entities, nil
}
