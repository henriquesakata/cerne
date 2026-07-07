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

// Feature is the persisted form of a feature entity. Parent, when set, is the
// ID of the feature this one decomposes: a feature referenced as a Parent is an
// epic (the role is inferred from the link, never a separate flag).
type Feature struct {
	ID           string              `json:"id"`
	Title        string              `json:"title"`
	Module       string              `json:"module"`
	Parent       string              `json:"parent,omitempty"`
	State        domain.FeatureState `json:"state"`
	SupersededBy string              `json:"superseded_by,omitempty"`
	Reason       string              `json:"reason,omitempty"`
	Facets       []string            `json:"facets,omitempty"`     // closed-set axis (D-0002/D-0003): infra/code/data/process/auth
	Components   []string            `json:"components,omitempty"` // curated subsystem/tool axis (D-0002/D-0003): gitback, materialize, …
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
	Facets       []string             `json:"facets,omitempty"`     // closed-set axis (D-0002): infra/code/data/process/auth
	Components   []string             `json:"components,omitempty"` // curated subsystem/tool axis (D-0002): gitea, kpack, …
	CreatedAt    time.Time            `json:"created_at"`
	UpdatedAt    time.Time            `json:"updated_at"`
}

// Runbook is the persisted form of a runbook entity: an actionable operational
// procedure (deploy, smoke, recovery, migration, setup).
type Runbook struct {
	ID           string              `json:"id"`
	Title        string              `json:"title"`
	State        domain.RunbookState `json:"state"`
	SupersededBy string              `json:"superseded_by,omitempty"`
	Reason       string              `json:"reason,omitempty"`
	Facets       []string            `json:"facets,omitempty"`     // closed-set axis (D-0002): infra/code/data/process/auth
	Components   []string            `json:"components,omitempty"` // curated subsystem/tool axis (D-0002): gitback, materialize, …
	CreatedAt    time.Time           `json:"created_at"`
	UpdatedAt    time.Time           `json:"updated_at"`
}

// Note is the persisted form of a note entity: durable, team-relevant project
// knowledge an agent acquired (gotchas, invariants, tacit conventions). The
// body is freeform; the lifecycle (active/superseded) is the leanest one that
// preserves searchable history.
type Note struct {
	ID           string           `json:"id"`
	Title        string           `json:"title"`
	State        domain.NoteState `json:"state"`
	SupersededBy string           `json:"superseded_by,omitempty"`
	Reason       string           `json:"reason,omitempty"`
	Informs      []string         `json:"informs,omitempty"`    // F-/D- ids this knowledge informs
	Facets       []string         `json:"facets,omitempty"`     // closed-set axis (D-0002): infra/code/data/process/auth
	Components   []string         `json:"components,omitempty"` // curated subsystem/tool axis (D-0002): gitea, kpack, …
	Durable      bool             `json:"durable,omitempty"`    // reviewed as durable: lint's stale-note skips it
	CreatedAt    time.Time        `json:"created_at"`
	UpdatedAt    time.Time        `json:"updated_at"`
}

// Reference is the persisted form of a reference entity. External references
// (registered, not imported) carry the source URL, the External marker, and an
// Informs backlink to the features/decisions the source motivated.
type Reference struct {
	ID         string                `json:"id"`
	Title      string                `json:"title"`
	State      domain.ReferenceState `json:"state"`
	URL        string                `json:"url,omitempty"`        // external source link
	External   bool                  `json:"external,omitempty"`   // true for a registered external source
	Informs    []string              `json:"informs,omitempty"`    // F-/D- ids this source motivated
	Facets     []string              `json:"facets,omitempty"`     // closed-set axis (D-0002): infra/code/data/process/auth
	Components []string              `json:"components,omitempty"` // curated subsystem/tool axis (D-0002): gitea, kpack, …
	CreatedAt  time.Time             `json:"created_at"`
	UpdatedAt  time.Time             `json:"updated_at"`
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

// Work is the persisted form of a work entity: a transient agent activity
// (F-0130) — "investigate error X", "review MR Y" — that is not a feature (no
// gates/docs) but still needs to be visible so a second agent doesn't
// duplicate it. It has no content document; Note is a short closing remark
// captured at done/abandon time, not a body.
type Work struct {
	ID        string           `json:"id"`
	Title     string           `json:"title"` // the activity description
	State     domain.WorkState `json:"state"`
	Actor     string           `json:"actor"`          // who is doing it: "<agent>" or "<user>@<hostname>"
	Refs      []string         `json:"refs,omitempty"` // F-/D- ids this activity relates to
	Note      string           `json:"note,omitempty"` // closing note (done) or reason (abandoned)
	Auto      bool             `json:"auto,omitempty"` // opened by a feature transition (F-0146), not by `cerne work start`
	CreatedAt time.Time        `json:"created_at"`
	UpdatedAt time.Time        `json:"updated_at"`
}

const (
	featuresSubdir    = "entities/features"
	decisionsSubdir   = "entities/decisions"
	referencesSubdir  = "entities/references"
	runbooksSubdir    = "entities/runbooks"
	notesSubdir       = "entities/notes"
	historicalsSubdir = "entities/historicals"
	worksSubdir       = "entities/works"
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

// SaveRunbook writes the runbook to entities/runbooks/<ID>.json.
func SaveRunbook(backendDir string, rb Runbook) error {
	return saveEntity(backendDir, runbooksSubdir, rb.ID, rb)
}

// LoadRunbook reads one runbook by id; ErrNotFound when the file is missing.
func LoadRunbook(backendDir, id string) (Runbook, error) {
	return loadEntity[Runbook](backendDir, runbooksSubdir, id)
}

// LoadAllRunbooks returns all runbooks sorted by ID ascending.
func LoadAllRunbooks(backendDir string) ([]Runbook, error) {
	return loadAllEntities[Runbook](backendDir, runbooksSubdir)
}

// SaveNote writes the note to entities/notes/<ID>.json.
func SaveNote(backendDir string, n Note) error {
	return saveEntity(backendDir, notesSubdir, n.ID, n)
}

// LoadNote reads one note by id; ErrNotFound when the file is missing.
func LoadNote(backendDir, id string) (Note, error) {
	return loadEntity[Note](backendDir, notesSubdir, id)
}

// LoadAllNotes returns all notes sorted by ID ascending.
func LoadAllNotes(backendDir string) ([]Note, error) {
	return loadAllEntities[Note](backendDir, notesSubdir)
}

// LoadNoteContent reads a note's markdown body (content/notes/<id>.md). The body
// lives outside the entity JSON, so a reader (e.g. 'cerne note show') fetches it
// straight from the backend store rather than from the .docs/ projection.
func LoadNoteContent(backendDir, id string) (string, error) {
	data, err := os.ReadFile(filepath.Join(backendDir, "content", "notes", id+".md"))
	if err != nil {
		return "", err
	}
	return string(data), nil
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

// DeleteReference removes the reference entity at entities/references/<id>.json.
// Used by the runbook migration, which reclassifies a reference imported from
// docs/runbooks into a runbook. ErrNotFound when the file is missing.
func DeleteReference(backendDir, id string) error {
	return deleteEntity(backendDir, referencesSubdir, id)
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

// SaveWork writes the work to entities/works/<ID>.json. saveEntity/
// writeJSONFile create the subdir on demand, so a legacy backend (initialized
// before F-0130, without entities/works/) works unchanged.
func SaveWork(backendDir string, w Work) error {
	return saveEntity(backendDir, worksSubdir, w.ID, w)
}

// LoadWork reads one work by id; ErrNotFound when the file is missing.
func LoadWork(backendDir, id string) (Work, error) {
	return loadEntity[Work](backendDir, worksSubdir, id)
}

// LoadAllWorks returns all works sorted by ID ascending; an empty slice (not
// an error) when entities/works/ is absent — a legacy backend that predates
// F-0130.
func LoadAllWorks(backendDir string) ([]Work, error) {
	return loadAllEntities[Work](backendDir, worksSubdir)
}

// saveEntity writes the entity to <backendDir>/<subdir>/<id>.json.
func saveEntity(backendDir, subdir, id string, e any) error {
	return writeJSONFile(filepath.Join(backendDir, filepath.FromSlash(subdir)), id+".json", e)
}

// deleteEntity removes <backendDir>/<subdir>/<id>.json; ErrNotFound when absent.
func deleteEntity(backendDir, subdir, id string) error {
	err := os.Remove(filepath.Join(backendDir, filepath.FromSlash(subdir), id+".json"))
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	return err
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
