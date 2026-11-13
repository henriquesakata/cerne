package app

import (
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/index"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
)

// saveNote is a mutate that writes the note id with the given title.
func saveNote(repoDir, id, title string) func() (string, error) {
	return func() (string, error) {
		now := time.Now().UTC()
		n := store.Note{ID: id, Title: title, State: domain.NoteActive, CreatedAt: now, UpdatedAt: now}
		return "note " + id, store.SaveNote(repoDir, n)
	}
}

func indexHas(t *testing.T, repoDir, id string) bool {
	t.Helper()
	res, _, err := index.SearchEntities(project.IndexPathForRepo(repoDir), id, 5)
	if err != nil {
		t.Fatalf("SearchEntities: %v", err)
	}
	return len(res) > 0 && res[0].EntityID == id
}

// F-0394 crit. 1: a write is findable on this machine with no pull.
func TestMutateAndSnapshotRefreshesIndex(t *testing.T) {
	_, repoDir, projDir, _ := remintFixture(t)
	if err := MutateAndSnapshot(repoDir, projDir, saveNote(repoDir, "N-0001", "nota nova")); err != nil {
		t.Fatalf("MutateAndSnapshot: %v", err)
	}
	if !indexHas(t, repoDir, "N-0001") {
		t.Fatal("N-0001 not found in the index right after the write")
	}
	at, ok, err := index.SyncedAt(project.IndexPathForRepo(repoDir))
	if err != nil || !ok || time.Since(at) > time.Minute {
		t.Fatalf("SyncedAt = %v, %v, %v; want a fresh time", at, ok, err)
	}
}

// F-0394 crit. 1: the push reconcile path refreshes too.
func TestReconcileCommitPushRefreshesIndex(t *testing.T) {
	_, repoDir, projDir, base := remintFixture(t)
	_, _, _, err := ReconcileCommitPushMutate(repoDir, projDir, base, "note", nil, func() error {
		_, err := saveNote(repoDir, "N-0002", "nota do push")()
		return err
	})
	if err != nil {
		t.Fatalf("ReconcileCommitPushMutate: %v", err)
	}
	if !indexHas(t, repoDir, "N-0002") {
		t.Fatal("N-0002 not found in the index right after the push")
	}
}

// F-0394 crit. 2: an index that cannot be rebuilt keeps the write and tells
// IndexNotice once.
func TestMutateAndSnapshotIndexFailureKeepsWrite(t *testing.T) {
	origin, repoDir, projDir, _ := remintFixture(t)
	if err := os.MkdirAll(project.IndexPathForRepo(repoDir), 0o755); err != nil { // a directory where the file should be
		t.Fatal(err)
	}
	var got []error
	IndexNotice = func(err error) { got = append(got, err) }
	t.Cleanup(func() { IndexNotice = func(error) {} })
	if err := MutateAndSnapshot(repoDir, projDir, saveNote(repoDir, "N-0003", "nota")); err != nil {
		t.Fatalf("the write must survive an index failure: %v", err)
	}
	if len(got) != 1 || got[0] == nil {
		t.Fatalf("IndexNotice calls = %v, want exactly one error", got)
	}
	if _, ok := originHas(t, origin, "entities/notes/N-0003.json"); !ok {
		t.Fatal("N-0003 not pushed")
	}
}

// F-0394 crit. 2: two writers at once end with the whole index and no notice.
func TestConcurrentWritesLeaveWholeIndex(t *testing.T) {
	_, repoDir, projDir, _ := remintFixture(t)
	var mu sync.Mutex
	var notices []string
	IndexNotice = func(err error) { mu.Lock(); notices = append(notices, err.Error()); mu.Unlock() }
	t.Cleanup(func() { IndexNotice = func(error) {} })
	ids := []string{"N-0004", "N-0005"}
	errs := make([]error, len(ids))
	var wg sync.WaitGroup
	for i, id := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = MutateAndSnapshot(repoDir, projDir, saveNote(repoDir, id, "nota "+id))
		}()
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatalf("MutateAndSnapshot: %v", err)
		}
	}
	if len(notices) > 0 {
		t.Fatalf("index notices: %s", strings.Join(notices, "; "))
	}
	for _, id := range ids {
		if !indexHas(t, repoDir, id) {
			t.Fatalf("%s missing from the index", id)
		}
	}
}
