package app

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/store"
)

// ErrContentConflict is a write whose base — the hash of the content the
// writer loaded — no longer matches the backend's (F-0464): someone else
// changed the document since, and the write is refused rather than lost.
var ErrContentConflict = errors.New("the document changed in the backend since it was loaded")

// ContentConflictError is ErrContentConflict carrying the content the core
// compared against, so the caller can show the difference without a re-read.
type ContentConflictError struct{ Current string }

func (e *ContentConflictError) Error() string { return ErrContentConflict.Error() }

// Is matches ErrContentConflict.
func (e *ContentConflictError) Is(target error) bool { return target == ErrContentConflict }

// ContentHash is the sha256 of the exact stored bytes, lowercase hex — the
// value a read returns as content_hash and a write sends back as If-Match.
// A missing file counts as empty bytes.
func ContentHash(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// checkBase refuses cur when a base was given and cur's hash is not it.
func checkBase(cur []byte, base string) error {
	if base != "" && ContentHash(cur) != base {
		return &ContentConflictError{Current: string(cur)}
	}
	return nil
}

// SaveEntityContentBy writes the markdown content of a decision, note or
// runbook (kind ∈ decision|note|runbook) to content/<kind>s/<id>.md by by,
// then commits, pushes and refreshes the snapshot (F-0464). store.ErrNotFound
// (wrapped) for an id that is not of kind's shape or does not exist;
// ErrEntityNotLive (wrapped) past active; *ContentConflictError when base is
// not empty and the backend's content hashes otherwise. A byte-identical write
// is a successful no-op. The clone is brought to origin first, so state and
// base are judged against the backend.
func SaveEntityContentBy(repoDir, projDir, kind, id, content, base string, by DocAuthor) error {
	path, state, ok := entityContent(repoDir, kind, id)
	if !ok {
		return fmt.Errorf("%w: %s %q", store.ErrNotFound, kind, id)
	}
	// Always on origin (audit A1): the state, not only the base, decides the
	// write, and a clone may trail a supersede pushed from elsewhere. These
	// routes have no legacy caller to keep on the cheaper path.
	err := mutateAndSnapshotLatest(repoDir, projDir, func() (string, error) {
		st, live, err := state()
		if err != nil {
			return "", err
		}
		if !live {
			return "", fmt.Errorf("%w: %s (%s)", ErrEntityNotLive, id, st)
		}
		cur, readErr := os.ReadFile(path)
		if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
			return "", readErr
		}
		if err := checkBase(cur, base); err != nil {
			return "", err
		}
		// Detected here, not by captureChanges: the event below is itself a change.
		if readErr == nil && string(cur) == content {
			return "", fmt.Errorf("%w: %s", gitback.ErrNothingToCommit, path)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return "", err
		}
		payload := map[string]string{"state": st, "provider": by.Provider, "model": by.Model, "source": by.Source}
		if err := store.AppendEvent(repoDir, domain.NewEvent(id, kind+".content_edited", payload, EventActor())); err != nil {
			return "", err
		}
		return fmt.Sprintf("cerne: update %s content", id), nil
	})
	if errors.Is(err, gitback.ErrNothingToCommit) {
		return nil // backend already had exactly this content
	}
	return err
}

// contentIDShape is the id of a decision, note or runbook — nothing else
// reaches a path: an id decoded from a URL may carry "../" (B-0034).
var contentIDShape = regexp.MustCompile(`^(D|N|RB)-\d+$`)

// entityContent is the content path of kind's id and the loader of its state
// and whether that state is live (active), read inside the lock; ok is false
// for an unknown kind or an id that is not of kind's shape.
func entityContent(repoDir, kind, id string) (path string, state func() (string, bool, error), ok bool) {
	m := contentIDShape.FindStringSubmatch(id)
	if m == nil {
		return "", nil, false
	}
	switch {
	case kind == "decision" && m[1] == "D":
		return DecisionDocPath(repoDir, id), func() (string, bool, error) {
			d, err := store.LoadDecision(repoDir, id)
			return string(d.State), d.State == domain.DecisionActive, err
		}, true
	case kind == "note" && m[1] == "N":
		return NoteDocPath(repoDir, id), func() (string, bool, error) {
			n, err := store.LoadNote(repoDir, id)
			return string(n.State), n.State == domain.NoteActive, err
		}, true
	case kind == "runbook" && m[1] == "RB":
		return RunbookDocPath(repoDir, id), func() (string, bool, error) {
			rb, err := store.LoadRunbook(repoDir, id)
			return string(rb.State), rb.State == domain.RunbookActive, err
		}, true
	}
	return "", nil, false
}
