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

	"github.com/henriquesakata/cerne/internal/domain"
)

// ErrNotFound is the sentinel for an entity file missing from the backend.
var ErrNotFound = errors.New("entity not found")

// errInvalidID is an id that is not PREFIX-NNNN (ValidEntityID), refused before
// it reaches a path: an id decoded from a URL may carry "../" (B-0034). It
// wraps ErrNotFound — no entity has that id — so every caller that answers a
// missing entity answers this one the same way.
var errInvalidID = fmt.Errorf("%w: invalid id", ErrNotFound)

// Feature is the persisted form of a feature entity. Parent, when set, is the
// ID of the feature this one decomposes: a feature referenced as a Parent is an
// epic (the role is inferred from the link, never a separate flag).
type Feature struct {
	ID           string              `json:"id"`
	Title        string              `json:"title"`
	Module       string              `json:"module"`
	Parent       string              `json:"parent,omitempty"`
	Fixes        string              `json:"fixes,omitempty"` // the feature whose delivered defect this one corrects (F-0321): an escaped defect for the process panel
	State        domain.FeatureState `json:"state"`
	SupersededBy string              `json:"superseded_by,omitempty"`
	Reason       string              `json:"reason,omitempty"`
	Facets       []string            `json:"facets,omitempty"`     // closed-set axis (D-0002/D-0003): infra/code/data/process/auth
	Components   []string            `json:"components,omitempty"` // curated subsystem/tool axis (D-0002/D-0003): gitback, materialize, …
	// Owner and Priority (F-0204): who is responsible and how urgent the demand
	// is. Owner is a free-text handle (no roster/ACL — cerne has no user
	// registry); Priority is the closed p0-p3 set (domain.ValidPriorities).
	// Both omitempty — existing entities stay valid with no migration.
	Owner    string `json:"owner,omitempty"`
	Priority string `json:"priority,omitempty"`
	// Delivery links (F-0179): the resolved URLs of the commit/MR that delivered
	// this feature — output traceability, distinct from Reference's input
	// provenance. Always stored as full URLs (a bare SHA is resolved at capture).
	DeliveryCommitURL string `json:"delivery_commit_url,omitempty"`
	DeliveryMRURL     string `json:"delivery_mr_url,omitempty"`
	// AcceptedVia (F-0488) is "merge" when the human's merge of the delivery PR
	// was the acceptance, "auto" when complete reached done by [gates] accept =
	// "auto" (F-0515); empty for an accept typed or dragged by hand.
	AcceptedVia string `json:"accepted_via,omitempty"`
	// AcceptedRisk (F-0516) is, on a card accepted_via "risk", the summary of
	// the three measurements that let complete accept it; empty otherwise.
	AcceptedRisk string `json:"accepted_risk,omitempty"`
	// HeldRisk (F-0521) is, on a card the risk gate held at complete, the lines
	// of what held it, the same ones complete prints; kept through the accept,
	// cleared by a revert.
	HeldRisk []string `json:"held_risk,omitempty"`
	// ApprovedVia (F-0515) is "auto" when an agent approved under [gates]
	// approve = "auto"; empty for a person's approval. Never back-filled.
	ApprovedVia string `json:"approved_via,omitempty"`
	// AcceptMode (F-0518) is the owner's mark for the risk gate: "human" holds
	// the card for a person, "auto" waives its sensitive-area condition; empty
	// lets the gate decide.
	AcceptMode string `json:"accept_mode,omitempty"`
	// Capabilities lists the product-capability nodes (C-…) this feature
	// contributes to (F-0188) — the functional axis, N:N, validated on link.
	Capabilities []string `json:"capabilities,omitempty"`
	// FromIdea is the idea (I-, F-0312) this feature was promoted from — the
	// structured back-link; the spec's "**Origem:**" line is prose only.
	FromIdea string `json:"from_idea,omitempty"`
	// Origin is who asked for the demand (T-0005): "human" | "agent", empty for
	// a card born before the field (unknown, never migrated).
	Origin string `json:"origin,omitempty"`
	// Mirror is the issue a human published this card to (F-0449); nil = never mirrored.
	Mirror    *MirrorRef `json:"mirror,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// MirrorRef links a card or an idea to one issue on a configured tracker
// (F-0449). Title and Summary are the public text the opt-in gave; both are
// empty for an issue that came from outside, whose text cerne never rewrites.
type MirrorRef struct {
	Tracker string `json:"tracker"`
	Kind    string `json:"kind"`
	ID      string `json:"id"`
	URL     string `json:"url"`
	Title   string `json:"title,omitempty"`
	Summary string `json:"summary,omitempty"`
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
	Informs      []string             `json:"informs,omitempty"`    // F-/E-/T-/B-/D-/RB-/N-/I- ids this decision informs (E-0005)
	// ReviewedAt: the last 'cerne decision keep' (F-0385) — lint's
	// stale-decision counts its signals from here instead of CreatedAt.
	ReviewedAt *time.Time `json:"reviewed_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
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
	When         []string         `json:"when,omitempty"`       // declared triggers (F-0457): edit:<path>, bash:<words>
	Gist         string           `json:"gist,omitempty"`       // the one line an agent needs when the note shows up (F-0459)
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
	Informs    []string              `json:"informs,omitempty"`    // F-/D-/RB-/N-/I- ids this source motivated
	Facets     []string              `json:"facets,omitempty"`     // closed-set axis (D-0002): infra/code/data/process/auth
	Components []string              `json:"components,omitempty"` // curated subsystem/tool axis (D-0002): gitea, kpack, …
	Check      *ReferenceCheck       `json:"check,omitempty"`      // last valid source check (F-0501)
	Archive    *ReferenceArchive     `json:"archive,omitempty"`    // last archive attempt (F-0503)
	NoArchive  bool                  `json:"no_archive,omitempty"` // never sent to an archive service (F-0503)
	CreatedAt  time.Time             `json:"created_at"`
	UpdatedAt  time.Time             `json:"updated_at"`
}

// ReferenceArchive is the last outcome of archiving a reference's page
// (F-0503). Status is archived, pending, not_public or skipped; an archived
// one has URL (the dated copy), CapturedAt and Origin: saved (a new copy) or
// existing (the closest copy already there). Reason says why for the others.
type ReferenceArchive struct {
	URL        string    `json:"url,omitempty"`
	CapturedAt time.Time `json:"captured_at,omitzero"`
	Origin     string    `json:"origin,omitempty"`
	Status     string    `json:"status"`
	Reason     string    `json:"reason,omitempty"`
}

// ReferenceCheck is the last check of an external reference's URL that counted
// (F-0501): a transient failure never overwrites it. Status is ok, changed or
// gone; BaselineHash is the accepted text hash `changed` is measured against.
type ReferenceCheck struct {
	CheckedAt    time.Time `json:"checked_at"`
	HTTPStatus   int       `json:"http_status,omitempty"`
	TextHash     string    `json:"text_hash,omitempty"`
	BaselineHash string    `json:"baseline_hash,omitempty"`
	Status       string    `json:"status"`
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
	Actor     string           `json:"actor"`                // who is doing it: "<agent>" or "<user>@<hostname>"
	Refs      []string         `json:"refs,omitempty"`       // ids (F-/E-/T-/B-/D-/W-/N-/RB-) this activity relates to
	Note      string           `json:"note,omitempty"`       // closing note (done) or reason (abandoned)
	Auto      bool             `json:"auto,omitempty"`       // opened by a feature transition (F-0146), not by `cerne work start`
	Paths     []string         `json:"paths,omitempty"`      // path globs this activity intends to touch (F-0151)
	HandoffAt *time.Time       `json:"handoff_at,omitempty"` // last session handoff (F-0339); the text is content/works/<id>/handoff.md
	CreatedAt time.Time        `json:"created_at"`
	UpdatedAt time.Time        `json:"updated_at"`
}

const (
	featuresSubdir     = "entities/features"
	decisionsSubdir    = "entities/decisions"
	referencesSubdir   = "entities/references"
	runbooksSubdir     = "entities/runbooks"
	notesSubdir        = "entities/notes"
	historicalsSubdir  = "entities/historicals"
	worksSubdir        = "entities/works"
	pendingsSubdir     = "entities/pendings"
	capabilitiesSubdir = "entities/capabilities"
	ideasSubdir        = "entities/ideas"
	handoffsSubdir     = "entities/handoffs"
)

// SaveFeature writes the feature to entities/features/<ID>.json.
func SaveFeature(backendDir string, f Feature) error {
	return saveEntity(backendDir, featuresSubdir, f.ID, f)
}

// LoadFeature reads one feature by id; ErrNotFound when the file is missing.
// The parent comes back canonical (domain.CanonicalID), so a legacy parent
// stored in another case reads as its epic everywhere (F-0326).
func LoadFeature(backendDir, id string) (Feature, error) {
	f, err := loadEntity[Feature](backendDir, featuresSubdir, id)
	f.Parent = domain.CanonicalID(f.Parent)
	return f, err
}

// LoadAllFeatures returns all features sorted by ID ascending, each parent
// canonical like LoadFeature's.
func LoadAllFeatures(backendDir string) ([]Feature, error) {
	fs, err := loadAllEntities[Feature](backendDir, featuresSubdir)
	for i := range fs {
		fs[i].Parent = domain.CanonicalID(fs[i].Parent)
	}
	return fs, err
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

// LoadRunbookContent reads a runbook's markdown body (content/runbooks/<id>.md),
// mirroring LoadNoteContent: straight from the backend store, not the .docs/
// projection.
func LoadRunbookContent(backendDir, id string) (string, error) {
	data, err := os.ReadFile(filepath.Join(backendDir, "content", "runbooks", id+".md"))
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
	w, err := loadEntity[Work](backendDir, worksSubdir, id)
	w.Actor = domain.ActorLabel(w.Actor) // F-0442: read only, the file is not rewritten
	return w, err
}

// LoadAllWorks returns all works sorted by ID ascending; an empty slice (not
// an error) when entities/works/ is absent — a legacy backend that predates
// F-0130.
func LoadAllWorks(backendDir string) ([]Work, error) {
	ws, err := loadAllEntities[Work](backendDir, worksSubdir)
	for i := range ws {
		ws[i].Actor = domain.ActorLabel(ws[i].Actor) // F-0442
	}
	return ws, err
}

// Pending is the persisted form of a pending (F-0322): a known delivery debt
// of a concrete feature, task or epic (From, mandatory). Body is short context
// kept in the JSON, not a content document. ClosedAt is stamped by resolve,
// drop and promote; Reason is mandatory for dropped; PromotedTo holds the
// feature/task id a promote created.
type Pending struct {
	ID         string              `json:"id"`
	Title      string              `json:"title"`
	Body       string              `json:"body,omitempty"`
	From       string              `json:"from"`
	State      domain.PendingState `json:"state"`
	Facets     []string            `json:"facets,omitempty"`
	Components []string            `json:"components,omitempty"`
	CreatedAt  time.Time           `json:"created_at"`
	UpdatedAt  time.Time           `json:"updated_at"`
	ClosedAt   *time.Time          `json:"closed_at,omitempty"`
	Reason     string              `json:"reason,omitempty"`
	PromotedTo string              `json:"promoted_to,omitempty"`
}

// SavePending writes the pending to entities/pendings/<ID>.json, creating the
// subdir on demand (a backend initialized before F-0322 works unchanged).
func SavePending(backendDir string, p Pending) error {
	return saveEntity(backendDir, pendingsSubdir, p.ID, p)
}

// LoadPending reads one pending by id; ErrNotFound when the file is missing.
func LoadPending(backendDir, id string) (Pending, error) {
	return loadEntity[Pending](backendDir, pendingsSubdir, id)
}

// LoadAllPendings returns all pendings sorted by ID ascending; empty (not an
// error) when entities/pendings/ is absent.
func LoadAllPendings(backendDir string) ([]Pending, error) {
	return loadAllEntities[Pending](backendDir, pendingsSubdir)
}

// Idea is the persisted form of an idea (F-0312): raw material nobody has
// evaluated yet — text and optional tags, no spec, no gates. ClosedAt is
// stamped by drop and promote; Reason (optional) by drop; PromotedTo holds
// the candidate a promote created.
type Idea struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Body  string `json:"body,omitempty"`
	// Text is the pre-F-0419 single field, read only: UnmarshalJSON folds it
	// into Title/Body and clears it, so no write ever emits it.
	Text       string           `json:"text,omitempty"`
	State      domain.IdeaState `json:"state"`
	Facets     []string         `json:"facets,omitempty"`
	Components []string         `json:"components,omitempty"`
	CreatedAt  time.Time        `json:"created_at"`
	UpdatedAt  time.Time        `json:"updated_at"`
	ClosedAt   *time.Time       `json:"closed_at,omitempty"`
	Reason     string           `json:"reason,omitempty"`
	PromotedTo string           `json:"promoted_to,omitempty"`
	// Token is the capture's idempotency key (C2): a retry of the same
	// submission returns this idea instead of minting a twin.
	Token string `json:"token,omitempty"`
	// Mirror is the outside issue mirror sync imported this idea from (F-0449);
	// promotion hands it to the card.
	Mirror *MirrorRef `json:"mirror,omitempty"`

	legacy bool // read from the text-only format (see Legacy)
}

// Legacy reports that the idea was read from the pre-F-0419 text-only format
// and still needs `cerne idea migrate` to be stored as title + body.
func (i Idea) Legacy() bool { return i.legacy }

// UnmarshalJSON reads the legacy text-only format as title (first line) +
// body (the rest, trimmed): the safety net for a backend not yet migrated
// (F-0419).
func (i *Idea) UnmarshalJSON(data []byte) error {
	type plain Idea
	var p plain
	if err := json.Unmarshal(data, &p); err != nil {
		return err
	}
	if p.Text != "" && p.Title == "" {
		title, body, _ := strings.Cut(strings.TrimSpace(p.Text), "\n")
		p.Title, p.Body = strings.TrimSpace(title), strings.TrimSpace(body)
		p.legacy = true
	}
	p.Text = ""
	*i = Idea(p)
	return nil
}

// SaveIdea writes the idea to entities/ideas/<ID>.json, creating the subdir
// on demand.
func SaveIdea(backendDir string, i Idea) error {
	return saveEntity(backendDir, ideasSubdir, i.ID, i)
}

// LoadIdea reads one idea by id; ErrNotFound when the file is missing.
func LoadIdea(backendDir, id string) (Idea, error) {
	return loadEntity[Idea](backendDir, ideasSubdir, id)
}

// LoadAllIdeas returns all ideas sorted by ID ascending; empty (not an error)
// when entities/ideas/ is absent.
func LoadAllIdeas(backendDir string) ([]Idea, error) {
	return loadAllEntities[Idea](backendDir, ideasSubdir)
}

// entityFile is <backendDir>/<subdir>/<id>.json, or errInvalidID for an id
// that is not PREFIX-NNNN: the one place an entity id becomes a path (B-0034).
func entityFile(backendDir, subdir, id string) (string, error) {
	if !ValidEntityID(id) {
		return "", fmt.Errorf("%w: %q", errInvalidID, id)
	}
	return filepath.Join(backendDir, filepath.FromSlash(subdir), id+".json"), nil
}

// saveEntity writes the entity to <backendDir>/<subdir>/<id>.json.
func saveEntity(backendDir, subdir, id string, e any) error {
	path, err := entityFile(backendDir, subdir, id)
	if err != nil {
		return err
	}
	return writeJSONFile(filepath.Dir(path), filepath.Base(path), e)
}

// deleteEntity removes <backendDir>/<subdir>/<id>.json; ErrNotFound when absent.
func deleteEntity(backendDir, subdir, id string) error {
	path, err := entityFile(backendDir, subdir, id)
	if err != nil {
		return err
	}
	err = os.Remove(path)
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
	path, err := entityFile(backendDir, subdir, id)
	if err != nil {
		var zero T
		return zero, err
	}
	return readEntity[T](path, id)
}

// readEntity decodes the entity file at path; id names it in ErrNotFound.
func readEntity[T any](path, id string) (T, error) {
	var e T
	data, err := os.ReadFile(path)
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
		e, err := readEntity[T](filepath.Join(dir, id+".json"), id) // a listed name has no separator
		if err != nil {
			return nil, err
		}
		entities = append(entities, e)
	}
	return entities, nil
}
