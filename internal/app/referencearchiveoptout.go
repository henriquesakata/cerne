package app

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/dupmatch"
	"github.com/henriquesakata/cerne/internal/store"
)

// ErrOptOutNeedsIDs refuses `reference archive --opt-out` with no reference named.
var ErrOptOutNeedsIDs = errors.New("--opt-out needs the ids of the references to keep out of the archive")

// optOutKey is the identity of a URL for the opt-out: the duplicate guard's
// normalization without the utm_* campaign parameters, so a tagged copy of the
// link is the same page.
func optOutKey(raw string) string {
	n := dupmatch.NormalizeURL(raw)
	u, err := url.Parse(n)
	if err != nil {
		return n
	}
	q := u.Query()
	for k := range q {
		if lk := strings.ToLower(k); strings.HasPrefix(lk, "utm_") || lk == "fbclid" || lk == "gclid" || lk == "mc_cid" {
			q.Del(k)
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}

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
