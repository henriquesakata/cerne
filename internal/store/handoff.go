package store

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
)

var handoffHeaderRe = regexp.MustCompile(`^## handoff (\d{4}-\d{2}-\d{2}) — (.*)$`)

// HandoffEntry is one session handoff of a work (F-0339): what the session
// leaving wrote for the one arriving.
type HandoffEntry struct {
	Date   string `json:"date"` // YYYY-MM-DD (UTC)
	Author string `json:"author"`
	Text   string `json:"text"`
}

// String renders the entry as it is stored: header, blank line, text.
func (h HandoffEntry) String() string {
	return "## handoff " + h.Date + " — " + h.Author + "\n\n" + h.Text
}

// AppendHandoff appends one dated handoff entry to work id's
// content/works/<id>/handoff.md — append-only, like the errata.
func AppendHandoff(repoDir, id, text, author string, at time.Time) error {
	return appendEntry(handoffPath(repoDir, id), id, "handoff", text, author, at.UTC())
}

// LoadHandoffs reads work id's handoff entries, oldest first. A missing file is
// "no handoffs", not an error.
func LoadHandoffs(repoDir, id string) ([]HandoffEntry, error) {
	data, err := os.ReadFile(handoffPath(repoDir, id))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []HandoffEntry
	var body []string
	flush := func() {
		if n := len(out); n > 0 {
			out[n-1].Text = strings.TrimSpace(strings.Join(body, "\n"))
		}
		body = nil
	}
	for _, l := range strings.Split(string(data), "\n") {
		if m := handoffHeaderRe.FindStringSubmatch(strings.TrimRight(l, "\r")); m != nil {
			flush()
			out = append(out, HandoffEntry{Date: m[1], Author: strings.TrimSpace(m[2])})
			continue
		}
		body = append(body, l)
	}
	flush()
	return out, nil
}

// Handoff is the persisted form of a handoff (HO-, F-0368): the baton a work
// passes on, with its own code and the state that says whether it was received.
// The brief lives in content/handoffs/<id>.md.
type Handoff struct {
	ID           string              `json:"id"`
	State        domain.HandoffState `json:"state"`
	FromWork     string              `json:"from_work"`
	Refs         []string            `json:"refs,omitempty"`
	Actor        string              `json:"actor"`
	FromSession  string              `json:"from_session,omitempty"`
	Next         string              `json:"next"`
	SupersededBy string              `json:"superseded_by,omitempty"`
	TakenBy      string              `json:"taken_by,omitempty"`
	TakenWork    string              `json:"taken_work,omitempty"`
	TakenSession string              `json:"taken_session,omitempty"`
	TakenNote    string              `json:"taken_note,omitempty"`
	TakenAt      *time.Time          `json:"taken_at,omitempty"`
	CancelReason string              `json:"cancel_reason,omitempty"`
	CreatedAt    time.Time           `json:"created_at"`
	UpdatedAt    time.Time           `json:"updated_at"`
}

// SaveHandoff writes the handoff to entities/handoffs/<ID>.json.
func SaveHandoff(backendDir string, h Handoff) error {
	return saveEntity(backendDir, handoffsSubdir, h.ID, h)
}

// LoadHandoff reads one handoff by id; ErrNotFound when the file is missing.
func LoadHandoff(backendDir, id string) (Handoff, error) {
	return loadEntity[Handoff](backendDir, handoffsSubdir, id)
}

// LoadAllHandoffs returns all handoffs sorted by ID ascending; empty (not an
// error) on a backend that predates F-0368.
func LoadAllHandoffs(backendDir string) ([]Handoff, error) {
	return loadAllEntities[Handoff](backendDir, handoffsSubdir)
}

func handoffContentPath(backendDir, id string) string {
	return filepath.Join(backendDir, "content", "handoffs", id+".md")
}

// SaveHandoffContent writes the handoff's brief to content/handoffs/<id>.md.
func SaveHandoffContent(backendDir, id, body string) error {
	path := handoffContentPath(backendDir, id)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(body), 0o644)
}

// LoadHandoffContent reads the handoff's brief.
func LoadHandoffContent(backendDir, id string) (string, error) {
	data, err := os.ReadFile(handoffContentPath(backendDir, id))
	return string(data), err
}

// Line is the handoff's one-line row in `handoff list` and CONTEXT.md
// (F-0368 crit. 7); workTitle is the origin work's title, shown on a pending one.
func (h Handoff) Line(workTitle string, now time.Time) string {
	switch h.State {
	case domain.HandoffPending:
		return fmt.Sprintf("%s pending %s — %s %s — from %s", h.ID, ShortAge(now.Sub(h.CreatedAt)), h.FromWork, workTitle, h.Actor)
	case domain.HandoffTaken:
		var age string
		if h.TakenAt != nil {
			age = ShortAge(now.Sub(*h.TakenAt)) + " ago "
		}
		return fmt.Sprintf("%s taken %sby %s (%s) — from %s", h.ID, age, h.TakenBy, h.TakenWork, h.FromWork)
	case domain.HandoffSuperseded:
		return fmt.Sprintf("%s superseded by %s — from %s", h.ID, h.SupersededBy, h.FromWork)
	default:
		return fmt.Sprintf("%s %s %s ago — from %s: %s", h.ID, h.State, ShortAge(now.Sub(h.UpdatedAt)), h.FromWork, h.CancelReason)
	}
}

// ShortAge renders a duration compactly: "45m", "3h", "2d".
func ShortAge(d time.Duration) string {
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}
