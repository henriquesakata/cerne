package app

import (
	"errors"
	"fmt"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// ErrOptOutNeedsIDs refuses `reference archive --opt-out` with no reference named.
var ErrOptOutNeedsIDs = errors.New("--opt-out needs the ids of the references to keep out of the archive")

// OptOutReferences marks the named external references no-archive (F-0503
// crit. 7): the field and a reference.archive_opt_out event that carries the
// URL, so the opt-out bars every reference with the same normalized URL, now
// and later. It touches no network and does not need references.archive on.
// It returns the ids it marked.
func OptOutReferences(repoDir, projDir string, ids []string) ([]string, error) {
	if len(ids) == 0 {
		return nil, ErrOptOutNeedsIDs
	}
	var done []string
	err := MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		done = nil
		for _, raw := range ids {
			id := domain.CanonicalID(raw)
			r, err := store.LoadReference(repoDir, id)
			if err != nil {
				return "", err
			}
			if !r.External || r.URL == "" {
				return "", fmt.Errorf("reference %s is not an external source: nothing to keep out of the archive", id)
			}
			r.NoArchive = true
			if err := store.SaveReference(repoDir, r); err != nil {
				return "", err
			}
			if err := store.AppendEvent(repoDir, domain.NewEvent(id, archiveOptOutEvent, map[string]string{"url": r.URL}, EventActor())); err != nil {
				return "", err
			}
			done = append(done, id)
		}
		return fmt.Sprintf("cerne: opt %d reference(s) out of the archive", len(done)), nil
	})
	if err != nil {
		return nil, err
	}
	return done, nil
}
