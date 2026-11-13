package index

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

func seedNote(t *testing.T, repoDir, id, title, body string, state domain.NoteState) {
	t.Helper()
	now := time.Now().UTC()
	if err := store.SaveNote(repoDir, store.Note{ID: id, Title: title, State: state, CreatedAt: now, UpdatedAt: now}); err != nil {
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

func TestPhraseNotesFiltersKindAndState(t *testing.T) {
	repoDir := t.TempDir()
	seedNote(t, repoDir, "N-0001", "Release gotcha", "run infra/k8s/scripts/release.sh twice", domain.NoteActive)
	seedNote(t, repoDir, "N-0002", "Old release", "infra/k8s/scripts/release.sh was different", domain.NoteSuperseded)
	if err := store.SaveFeature(repoDir, store.Feature{ID: "F-0001", Title: "infra/k8s/scripts/release.sh", State: domain.StateCandidate}); err != nil {
		t.Fatal(err)
	}
	db := filepath.Join(t.TempDir(), "index.db")
	if err := Rebuild(repoDir, db, time.Time{}); err != nil {
		t.Fatal(err)
	}
	hits, err := PhraseNotes(context.Background(), db, []string{"infra/k8s/scripts/release.sh", `we"ird (x)`})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].ID != "N-0001" || hits[0].Title != "Release gotcha" || hits[0].Body == "" {
		t.Fatalf("hits = %+v", hits)
	}
}

func TestPhraseNotesMissingDB(t *testing.T) {
	hits, err := PhraseNotes(context.Background(), filepath.Join(t.TempDir(), "none.db"), []string{"a/b.sh"})
	if hits != nil || err != nil {
		t.Fatalf("hits=%v err=%v", hits, err)
	}
}

func TestPhraseNotesLockedIndexIsBusy(t *testing.T) {
	repoDir := t.TempDir()
	seedNote(t, repoDir, "N-0001", "t", "see a/b.sh", domain.NoteActive)
	db := filepath.Join(t.TempDir(), "index.db")
	if err := Rebuild(repoDir, db, time.Time{}); err != nil {
		t.Fatal(err)
	}
	unlock := lockIndex(t, db)
	defer unlock()
	start := time.Now()
	_, err := PhraseNotes(context.Background(), db, []string{"a/b.sh"})
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("err = %v, want ErrBusy", err)
	}
	if d := time.Since(start); d > 500*time.Millisecond {
		t.Fatalf("took %v", d)
	}
}

// BenchmarkPhraseNotes: 1,000 notes; the work-gate runs this on every edit.
func BenchmarkPhraseNotes(b *testing.B) {
	db := benchIndex(b)
	phrases := []string{"infra/k8s/scripts/release.sh", "k8s/scripts/release.sh", "scripts/release.sh", "release.sh"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := PhraseNotes(context.Background(), db, phrases); err != nil {
			b.Fatal(err)
		}
	}
}

// TestPhraseNotesMedianLatency pins criterion 6: median <= 20 ms over 1,000 notes.
func TestPhraseNotesMedianLatency(t *testing.T) {
	db := benchIndex(t)
	phrases := []string{"infra/k8s/scripts/release.sh", "k8s/scripts/release.sh", "scripts/release.sh", "release.sh"}
	var ds []time.Duration
	for i := 0; i < 50; i++ {
		s := time.Now()
		if _, err := PhraseNotes(context.Background(), db, phrases); err != nil {
			t.Fatal(err)
		}
		ds = append(ds, time.Since(s))
	}
	slices.Sort(ds)
	med := ds[len(ds)/2]
	t.Logf("median %v over 50 runs", med)
	if med > 20*time.Millisecond {
		t.Fatalf("median %v > 20ms", med)
	}
}

func benchIndex(tb testing.TB) string {
	tb.Helper()
	repoDir := tb.TempDir()
	now := time.Now().UTC()
	dir := filepath.Join(repoDir, "content", "notes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		tb.Fatal(err)
	}
	for i := 1; i <= 1000; i++ {
		id := fmt.Sprintf("N-%04d", i)
		if err := store.SaveNote(repoDir, store.Note{ID: id, Title: fmt.Sprintf("Note %d about deploys", i), State: domain.NoteActive, CreatedAt: now, UpdatedAt: now}); err != nil {
			tb.Fatal(err)
		}
		body := fmt.Sprintf("Long body %d: kubernetes rollout, helm values, pipelines, see scripts/other%d.sh and package.json. ", i, i%40)
		for j := 0; j < 20; j++ {
			body += "lorem ipsum dolor sit amet consectetur adipiscing elit sed do eiusmod "
		}
		if i%10 == 0 {
			body += "touches infra/k8s/scripts/release.sh"
		}
		if err := os.WriteFile(filepath.Join(dir, id+".md"), []byte(body), 0o644); err != nil {
			tb.Fatal(err)
		}
	}
	db := filepath.Join(tb.TempDir(), "index.db")
	if err := Rebuild(repoDir, db, time.Time{}); err != nil {
		tb.Fatal(err)
	}
	return db
}

// The MATCH is restricted to title and body: a phrase equal to an entity id
// (or any other column) is not a citation.
func TestPhraseNotesMatchesTitleAndBodyOnly(t *testing.T) {
	repoDir := t.TempDir()
	seedNote(t, repoDir, "N-0001", "plain", "nothing", domain.NoteActive)
	db := filepath.Join(t.TempDir(), "index.db")
	if err := Rebuild(repoDir, db, time.Time{}); err != nil {
		t.Fatal(err)
	}
	hits, err := PhraseNotes(context.Background(), db, []string{"N-0001"})
	if err != nil || len(hits) != 0 {
		t.Fatalf("hits=%v err=%v", hits, err)
	}
}
