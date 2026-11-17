package index

import (
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

func hits(t *testing.T, db, q string) []string {
	t.Helper()
	res, _, err := SearchEntities(db, q, 10)
	if err != nil {
		t.Fatalf("SearchEntities(%q): %v", q, err)
	}
	var ids []string
	for _, r := range res {
		ids = append(ids, r.EntityID)
	}
	return ids
}

func builtIndex(t *testing.T) (repoDir, db string) {
	t.Helper()
	repoDir = seedBackend(t)
	db = filepath.Join(t.TempDir(), "index.db")
	if err := Rebuild(repoDir, db, time.Time{}); err != nil {
		t.Fatal(err)
	}
	return
}

func saveNoteT(t *testing.T, repoDir, id, title string) {
	t.Helper()
	now := time.Now().UTC()
	if err := store.SaveNote(repoDir, store.Note{ID: id, Title: title, State: domain.NoteActive, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateNewEditedAndRemovedIDs(t *testing.T) {
	repoDir, db := builtIndex(t)
	saveNoteT(t, repoDir, "N-0009", "zebra listrada")
	if err := Update(repoDir, db, []string{"N-0009"}, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if got := hits(t, db, "zebra"); !reflect.DeepEqual(got, []string{"N-0009"}) {
		t.Fatalf("new id: hits = %v", got)
	}
	saveNoteT(t, repoDir, "N-0009", "girafa malhada")
	if err := Update(repoDir, db, []string{"N-0009"}, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if got := hits(t, db, "zebra"); len(got) != 0 {
		t.Fatalf("edited id still matches the old title: %v", got)
	}
	if got := hits(t, db, "girafa"); !reflect.DeepEqual(got, []string{"N-0009"}) {
		t.Fatalf("edited id: hits = %v", got)
	}
	if err := os.Remove(filepath.Join(repoDir, "entities", "notes", "N-0009.json")); err != nil {
		t.Fatal(err)
	}
	if err := Update(repoDir, db, []string{"N-0009"}, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if got := hits(t, db, "girafa"); len(got) != 0 {
		t.Fatalf("removed id still indexed: %v", got)
	}
	// Untouched entities stay.
	if got := hits(t, db, "Postgres"); len(got) == 0 {
		t.Fatal("an untouched entity vanished")
	}
}

func TestUpdateRecordsSyncedAtOnlyWhenGiven(t *testing.T) {
	repoDir, db := builtIndex(t)
	if err := Update(repoDir, db, []string{"N-0001"}, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := SyncedAt(db); ok {
		t.Fatal("zero time must not record")
	}
	at := time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)
	if err := Update(repoDir, db, []string{"N-0001"}, at); err != nil {
		t.Fatal(err)
	}
	if got, ok, _ := SyncedAt(db); !ok || !got.Equal(at) {
		t.Fatalf("SyncedAt = %v, %v", got, ok)
	}
}

func TestUpdateWithoutIndexFails(t *testing.T) {
	repoDir := seedBackend(t)
	if err := Update(repoDir, filepath.Join(t.TempDir(), "none.db"), []string{"N-0001"}, time.Time{}); err == nil {
		t.Fatal("Update on a missing index must fail so the caller falls back to Rebuild")
	}
}

func TestIDsFromPaths(t *testing.T) {
	got := IDsFromPaths([]string{
		"entities/features/F-0001.json", "content/features/F-0001/spec.md", "content/features/F-0002/plan.md",
		"content/notes/N-0001.md", "content/errata/F-0003.md", "events/2026.ndjson", "README.md",
	})
	want := []string{"F-0001", "F-0002", "F-0003", "N-0001"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("IDsFromPaths = %v, want %v", got, want)
	}
}

func TestUpdateConcurrentWithSearch(t *testing.T) {
	repoDir, db := builtIndex(t)
	var wg sync.WaitGroup
	stop := make(chan struct{})
	var searchErr error
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			res, _, err := SearchEntities(db, "Postgres", 5)
			if err != nil || len(res) == 0 {
				searchErr = err
				if err == nil {
					searchErr = os.ErrInvalid // a half-written index would show no rows
				}
				return
			}
		}
	}()
	for i := 0; i < 10; i++ {
		saveNoteT(t, repoDir, "N-0010", "nota concorrente")
		if err := Update(repoDir, db, []string{"N-0010"}, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	close(stop)
	wg.Wait()
	if searchErr != nil {
		t.Fatalf("a search during Update saw a broken index: %v", searchErr)
	}
}

// TestUpdateKeepsFeatureFamilyIDs (B-0021): an incremental Update of a bug, an
// epic or a task re-inserts its rows (Update deletes every changed id first), so
// the card stays searchable like an F- one.
func TestUpdateKeepsFeatureFamilyIDs(t *testing.T) {
	repoDir, db := builtIndex(t)
	now := time.Now().UTC()
	for id, title := range map[string]string{"F-0900": "ornitorrinco", "B-0900": "capivara", "E-0900": "jaguatirica", "T-0900": "tamandua"} {
		if err := store.SaveFeature(repoDir, store.Feature{ID: id, Title: title, State: domain.StateCandidate, CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
		if err := Update(repoDir, db, []string{id}, time.Time{}); err != nil {
			t.Fatal(err)
		}
		if got := hits(t, db, title); !reflect.DeepEqual(got, []string{id}) {
			t.Errorf("%s: hits for %q = %v", id, title, got)
		}
	}
	// A second write to the same card (the start of B-0021's repro) keeps it too.
	if err := Update(repoDir, db, []string{"B-0900", "E-0900", "T-0900"}, time.Time{}); err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{"capivara", "jaguatirica", "tamandua"} {
		if got := hits(t, db, w); len(got) != 1 {
			t.Errorf("after a second update, hits for %q = %v", w, got)
		}
	}
}

// TestUpdateRowsEqualRebuildForFeatureFamily (B-0021): an incremental Update of
// a bug, an epic and a task leaves exactly the rows a full rebuild builds
// (kind, state, title and body), not just a hit.
func TestUpdateRowsEqualRebuildForFeatureFamily(t *testing.T) {
	repoDir := seedBackend(t)
	now := time.Now().UTC()
	ids := []string{"B-0901", "E-0901", "T-0901"}
	for _, id := range ids {
		if err := store.SaveFeature(repoDir, store.Feature{ID: id, Title: "titulo " + id, State: domain.StateApproved, CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(repoDir, "content", "features", id), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(repoDir, "content", "features", id, "spec.md"), []byte("corpo do spec de "+id), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	full := filepath.Join(t.TempDir(), "full.db")
	if err := Rebuild(repoDir, full, time.Time{}); err != nil {
		t.Fatal(err)
	}
	inc := filepath.Join(t.TempDir(), "inc.db")
	if err := Rebuild(repoDir, inc, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if err := Update(repoDir, inc, ids, time.Time{}); err != nil {
		t.Fatal(err)
	}
	rows := func(db string) []string {
		d, err := openDB(db)
		if err != nil {
			t.Fatal(err)
		}
		defer d.Close()
		r, err := d.Query(`SELECT entity_id, kind, doc, state, title, body FROM docs WHERE entity_id IN ('B-0901','E-0901','T-0901') ORDER BY entity_id, doc`)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Close()
		var out []string
		for r.Next() {
			var a, b, c, e, f, g string
			if err := r.Scan(&a, &b, &c, &e, &f, &g); err != nil {
				t.Fatal(err)
			}
			out = append(out, a+"|"+b+"|"+c+"|"+e+"|"+f+"|"+g)
		}
		return out
	}
	want, got := rows(full), rows(inc)
	if len(want) != 3 || !reflect.DeepEqual(got, want) {
		t.Fatalf("incremental rows differ from a full rebuild\nfull: %q\ninc:  %q", want, got)
	}
}
