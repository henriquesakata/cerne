package app

import (
	"errors"
	"slices"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// F-0503 crit. 7: --opt-out marks existing references: it needs ids, records
// the field and an event that carries the URL, uses no network and works with
// references.archive off.
func TestOptOutReferencesMarksExistingOnes(t *testing.T) {
	repoDir, projDir := archiveBackend(t, 2, false)
	if _, err := OptOutReferences(repoDir, projDir, nil); !errors.Is(err, ErrOptOutNeedsIDs) {
		t.Errorf("no ids: err = %v, want ErrOptOutNeedsIDs", err)
	}
	if _, err := OptOutReferences(repoDir, projDir, []string{"R-0099"}); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("unknown id: err = %v, want store.ErrNotFound", err)
	}
	got, err := OptOutReferences(repoDir, projDir, []string{"R-0001"})
	if err != nil || !slices.Equal(got, []string{"R-0001"}) {
		t.Fatalf("opt out = %v, %v", got, err)
	}
	if !loadRef(t, repoDir, "R-0001").NoArchive || loadRef(t, repoDir, "R-0002").NoArchive {
		t.Error("only R-0001 should be marked no-archive")
	}
	evs, _ := store.ReadEvents(repoDir)
	last := evs[len(evs)-1]
	if last.EntityID != "R-0001" || last.Type != "reference.archive_opt_out" || last.Payload["url"] != "https://example.com/1" {
		t.Errorf("event = %+v, want reference.archive_opt_out carrying the URL", last)
	}
}

// F-0503 crit. 7: the opt-out key drops utm_* parameters, so registering the
// URL again with a campaign tag, or with www., is still barred; another query
// is another page.
func TestArchiveOptOutKeyDropsUTM(t *testing.T) {
	repoDir, projDir := archiveBackend(t, 4, true)
	if _, err := OptOutReferences(repoDir, projDir, []string{"R-0001"}); err != nil {
		t.Fatal(err)
	}
	setRef(t, repoDir, projDir, "R-0001", func(r *store.Reference) { r.State = domain.ReferenceObsolete })
	setRef(t, repoDir, projDir, "R-0002", func(r *store.Reference) { r.URL = "https://example.com/1?utm_source=x&UTM_medium=mail" })
	setRef(t, repoDir, projDir, "R-0003", func(r *store.Reference) { r.URL = "https://www.example.com/1?utm_campaign=c" })
	setRef(t, repoDir, projDir, "R-0004", func(r *store.Reference) { r.URL = "https://example.com/1?page=2&utm_source=x" })
	c := &scriptClient{save: saves, captures: noCaptures}
	res, err := ArchiveReferences(repoDir, projDir, nil, publicNet(c), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].ID != "R-0004" {
		t.Errorf("batch took %+v, want only R-0004 (another query)", res)
	}
}

// F-0503 crit. 7: the opt-out bars every reference with the same normalized
// URL (case of scheme and host, fragment), a later one included, in a batch and
// when named; a different URL is not barred.
func TestArchiveOptOutIsPerURL(t *testing.T) {
	repoDir, projDir := archiveBackend(t, 3, true)
	if _, err := OptOutReferences(repoDir, projDir, []string{"R-0001"}); err != nil {
		t.Fatal(err)
	}
	setRef(t, repoDir, projDir, "R-0001", func(r *store.Reference) { r.State = domain.ReferenceObsolete })
	setRef(t, repoDir, projDir, "R-0002", func(r *store.Reference) { r.URL = "HTTPS://Example.COM/1#later-note" })
	c := &scriptClient{save: saves, captures: noCaptures}
	res, err := ArchiveReferences(repoDir, projDir, nil, publicNet(c), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].ID != "R-0003" {
		t.Errorf("batch took %+v, want only R-0003", res)
	}
	res, err = ArchiveReferences(repoDir, projDir, []string{"R-0002"}, offlineNet(t), false)
	if err != nil || len(res) != 1 || res[0].Status != "skipped" || res[0].Reason != "no-archive" {
		t.Errorf("named: %+v, %v; want skipped (no-archive)", res, err)
	}
}
