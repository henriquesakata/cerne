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
	if err := index.Rebuild(repoDir, filepath.Join(cacheDir, "index.db")); err != nil {
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
