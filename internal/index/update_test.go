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
