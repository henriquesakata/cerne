package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/index"
	"github.com/henriquesakata/cerne/internal/store"
)

// seedPathNote writes an active/superseded note with a body, updated at upd.
func seedPathNote(t *testing.T, repoDir, id, title, body string, st domain.NoteState, upd time.Time) {
	t.Helper()
	if err := store.SaveNote(repoDir, store.Note{ID: id, Title: title, State: st, CreatedAt: upd, UpdatedAt: upd}); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(repoDir, "content", "notes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, id+".md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func buildIndex(t *testing.T, repoDir string) (cacheDir string) {
	t.Helper()
	cacheDir = t.TempDir()
	if err := index.Rebuild(repoDir, filepath.Join(cacheDir, "index.db"), time.Time{}); err != nil {
		t.Fatal(err)
	}
	return cacheDir
}

func noteIDs(ns []PathNote) []string {
	var out []string
	for _, n := range ns {
		out = append(out, n.ID)
	}
	return out
}

func sameIDs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

const relRelease = "infra/k8s/scripts/release.sh"

func TestNotesForPathMatchesFullSuffixAndUniqueName(t *testing.T) {
	repo := t.TempDir()
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	seedPathNote(t, repo, "N-0001", "full", "edit infra/k8s/scripts/release.sh carefully", domain.NoteActive, t0)
	seedPathNote(t, repo, "N-0002", "suffix", "see scripts/release.sh", domain.NoteActive, t0)
	seedPathNote(t, repo, "N-0003", "name", "release.sh is flaky", domain.NoteActive, t0)
	seedPathNote(t, repo, "N-0004", "token noise", "release notes: the sh script, k8s infra scripts", domain.NoteActive, t0) // tokens co-occur, no path
	seedPathNote(t, repo, "N-0005", "prefix glue", "prerelease.sh differs", domain.NoteActive, t0)
	cache := buildIndex(t, repo)
	got, err := NotesForPath(context.Background(), cache, repo, relRelease, func(string) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"N-0001", "N-0002", "N-0003"}; !sameIDs(noteIDs(got), want) {
		t.Fatalf("ids = %v, want %v", noteIDs(got), want)
	}
	if got[0].Score != 3 || got[1].Score != 2 || got[2].Score != 1 {
		t.Fatalf("scores = %+v", got)
	}
}

func TestNotesForPathNameOnlyNeedsUnique(t *testing.T) {
	repo := t.TempDir()
	t0 := time.Now().UTC()
	seedPathNote(t, repo, "N-0001", "pkg", "bump package.json first", domain.NoteActive, t0)
	seedPathNote(t, repo, "N-0002", "pkg dir", "web/package.json pins node", domain.NoteActive, t0)
	cache := buildIndex(t, repo)
	got, err := NotesForPath(context.Background(), cache, repo, "web/package.json", func(string) bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	if !sameIDs(noteIDs(got), []string{"N-0002"}) {
		t.Fatalf("ids = %v", noteIDs(got))
	}
}

func TestNotesForPathOrderByScoreThenRecency(t *testing.T) {
	repo := t.TempDir()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	seedPathNote(t, repo, "N-0001", "old suffix", "scripts/release.sh", domain.NoteActive, base)
	seedPathNote(t, repo, "N-0002", "new suffix", "scripts/release.sh", domain.NoteActive, base.Add(48*time.Hour))
	seedPathNote(t, repo, "N-0003", "mid suffix", "scripts/release.sh", domain.NoteActive, base.Add(24*time.Hour))
	seedPathNote(t, repo, "N-0004", "oldest full", relRelease, domain.NoteActive, base.Add(-time.Hour))
	cache := buildIndex(t, repo)
	got, err := NotesForPath(context.Background(), cache, repo, relRelease, func(string) bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"N-0004", "N-0002", "N-0003", "N-0001"}; !sameIDs(noteIDs(got), want) {
		t.Fatalf("ids = %v, want %v", noteIDs(got), want)
	}
}

func TestNotesForPathSupersededNever(t *testing.T) {
	repo := t.TempDir()
	seedPathNote(t, repo, "N-0001", "gone", relRelease, domain.NoteSuperseded, time.Now().UTC())
	cache := buildIndex(t, repo)
	got, err := NotesForPath(context.Background(), cache, repo, relRelease, func(string) bool { return true })
	if err != nil || len(got) != 0 {
		t.Fatalf("got=%v err=%v", got, err)
	}
}

func TestNotesForPathNoIndex(t *testing.T) {
	got, err := NotesForPath(context.Background(), t.TempDir(), t.TempDir(), relRelease, nil)
	if err != nil || got != nil {
		t.Fatalf("got=%v err=%v", got, err)
	}
}

// TestNotesForPath_Deadline: a write-locked index must not hold the hook; the
// call returns no notes and no error well inside the hook's budget.
func TestNotesForPath_Deadline(t *testing.T) {
	repo := t.TempDir()
	seedPathNote(t, repo, "N-0001", "t", relRelease, domain.NoteActive, time.Now().UTC())
	cache := buildIndex(t, repo)
	unlock := lockIndexExclusive(t, filepath.Join(cache, "index.db"))
	defer unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	start := time.Now()
	got, err := NotesForPath(ctx, cache, repo, relRelease, func(string) bool { return true })
	if d := time.Since(start); d > 200*time.Millisecond {
		t.Fatalf("took %v", d)
	}
	if err != nil || len(got) != 0 {
		t.Fatalf("got=%v err=%v", got, err)
	}
}

// Criterion 2: a root-level file is name-only (needs uniqueness), and a path
// cited under another directory is not a citation of this one.
func TestNotesForPathRootFileIsNameOnly(t *testing.T) {
	repo := t.TempDir()
	t0 := time.Now().UTC()
	seedPathNote(t, repo, "N-0001", "root", "bump package.json first", domain.NoteActive, t0)
	seedPathNote(t, repo, "N-0002", "web", "web/package.json pins node", domain.NoteActive, t0)
	cache := buildIndex(t, repo)
	got, err := NotesForPath(context.Background(), cache, repo, "package.json", func(string) bool { return true })
	if err != nil || !sameIDs(noteIDs(got), []string{"N-0001"}) {
		t.Fatalf("unique: %v %v", noteIDs(got), err)
	}
	got, _ = NotesForPath(context.Background(), cache, repo, "package.json", func(string) bool { return false })
	if len(got) != 0 {
		t.Fatalf("not unique: %v", noteIDs(got))
	}
}

func TestNotesForPathLeftBoundary(t *testing.T) {
	repo := t.TempDir()
	t0 := time.Now().UTC()
	seedPathNote(t, repo, "N-0001", "longer", "see other/src/app.go", domain.NoteActive, t0)
	seedPathNote(t, repo, "N-0002", "dot-slash", "run ./src/app.go", domain.NoteActive, t0)
	seedPathNote(t, repo, "N-0003", "plain", "edit src/app.go (twice)", domain.NoteActive, t0)
	seedPathNote(t, repo, "N-0004", "dotdot", "see ../src/app.go", domain.NoteActive, t0)
	cache := buildIndex(t, repo)
	got, err := NotesForPath(context.Background(), cache, repo, "src/app.go", func(string) bool { return false })
	if err != nil || !sameIDs(noteIDs(got), []string{"N-0002", "N-0003"}) {
		t.Fatalf("ids = %v %v", noteIDs(got), err)
	}
}

// Uniqueness is asked lazily: only for a bare-name hit, and at most once.
func TestNotesForPathUniquenessIsLazy(t *testing.T) {
	repo := t.TempDir()
	t0 := time.Now().UTC()
	seedPathNote(t, repo, "N-0001", "full", relRelease, domain.NoteActive, t0)
	cache := buildIndex(t, repo)
	n := 0
	u := func(string) bool { n++; return true }
	if _, err := NotesForPath(context.Background(), cache, repo, relRelease, u); err != nil || n != 0 {
		t.Fatalf("calls = %d err=%v, want 0", n, err)
	}
	seedPathNote(t, repo, "N-0002", "n1", "release.sh a", domain.NoteActive, t0)
	seedPathNote(t, repo, "N-0003", "n2", "release.sh b", domain.NoteActive, t0)
	cache = buildIndex(t, repo)
	if _, err := NotesForPath(context.Background(), cache, repo, relRelease, u); err != nil || n != 1 {
		t.Fatalf("calls = %d err=%v, want 1", n, err)
	}
}

// Round 2: inside a tier, prominence (title, then number of mentions) comes
// before recency.
func TestNotesForPathProminenceBeforeRecency(t *testing.T) {
	repo := t.TempDir()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	seedPathNote(t, repo, "N-0001", "unrelated title", "once: scripts/release.sh", domain.NoteActive, base.Add(72*time.Hour))
	seedPathNote(t, repo, "N-0002", "about scripts/release.sh", "body", domain.NoteActive, base)
	seedPathNote(t, repo, "N-0003", "three", "scripts/release.sh, then scripts/release.sh and scripts/release.sh", domain.NoteActive, base.Add(24*time.Hour))
	seedPathNote(t, repo, "N-0004", "two", "scripts/release.sh x scripts/release.sh", domain.NoteActive, base.Add(48*time.Hour))
	seedPathNote(t, repo, "N-0005", "tie older", "scripts/release.sh once", domain.NoteActive, base.Add(time.Hour))
	cache := buildIndex(t, repo)
	got, err := NotesForPath(context.Background(), cache, repo, relRelease, func(string) bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	// title wins over newer body-only; 3 mentions beat 2 and 1; equal prominence: newest first
	if want := []string{"N-0002", "N-0003", "N-0004", "N-0001", "N-0005"}; !sameIDs(noteIDs(got), want) {
		t.Fatalf("ids = %v, want %v", noteIDs(got), want)
	}
}

// Specificity still outranks prominence.
func TestNotesForPathSpecificityBeatsProminence(t *testing.T) {
	repo := t.TempDir()
	t0 := time.Now().UTC()
	seedPathNote(t, repo, "N-0001", "scripts/release.sh", "scripts/release.sh scripts/release.sh", domain.NoteActive, t0)
	seedPathNote(t, repo, "N-0002", "plain", relRelease, domain.NoteActive, t0.Add(-time.Hour))
	cache := buildIndex(t, repo)
	got, _ := NotesForPath(context.Background(), cache, repo, relRelease, func(string) bool { return false })
	if !sameIDs(noteIDs(got), []string{"N-0002", "N-0001"}) {
		t.Fatalf("ids = %v", noteIDs(got))
	}
}
