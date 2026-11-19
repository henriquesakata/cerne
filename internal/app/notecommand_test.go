package app

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// F-0457 criterion 6: the notes whose bash: trigger matches the command, the
// longest trigger first, then newest; each note once, with its best trigger.
func TestNotesForCommand(t *testing.T) {
	repo := t.TempDir()
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	seedWhenNote(t, repo, "N-0001", "git anything", "b", t0, "bash:git")
	seedWhenNote(t, repo, "N-0002", "tag old", "b", t0, "bash:git tag")
	seedWhenNote(t, repo, "N-0003", "tag new", "b", t0.Add(time.Hour), "bash:git tag", "bash:git")
	seedWhenNote(t, repo, "N-0004", "push", "b", t0, "bash:git push")
	seedWhenNote(t, repo, "N-0005", "edit only", "b", t0, "edit:git/tag")
	seedPathNote(t, repo, "N-0006", "superseded", "b", domain.NoteSuperseded, t0)
	cache := buildIndex(t, repo)

	got, err := NotesForCommand(context.Background(), cache, repo, "cd x && git tag -a v1")
	if err != nil {
		t.Fatal(err)
	}
	var ids, words []string
	for _, n := range got {
		ids = append(ids, n.ID)
		words = append(words, n.Words)
	}
	if want := []string{"N-0003", "N-0002", "N-0001"}; !slices.Equal(ids, want) {
		t.Fatalf("ids = %v, want %v", ids, want)
	}
	if want := []string{"git tag", "git tag", "git"}; !slices.Equal(words, want) {
		t.Fatalf("words = %v, want %v", words, want)
	}

	for _, cmd := range []string{"git tags", "git log --tag", "echo hi", ""} {
		got, err := NotesForCommand(context.Background(), cache, repo, cmd)
		if err != nil {
			t.Fatal(err)
		}
		for _, n := range got {
			if n.Words == "git tag" {
				t.Fatalf("%q matched bash:git tag", cmd)
			}
		}
	}
	if got, err := NotesForCommand(context.Background(), t.TempDir(), repo, "git tag"); got != nil || err != nil {
		t.Fatalf("no index: got %v, err %v", got, err)
	}
}

// commandBench is a 1,000-note index where every tenth note declares a bash:
// trigger (F-0457 criterion 9).
func commandBench(tb testing.TB) (cache, repo string) {
	tb.Helper()
	repo = tb.TempDir()
	now := time.Now().UTC()
	for i := 1; i <= 1000; i++ {
		n := store.Note{ID: fmt.Sprintf("N-%04d", i), Title: fmt.Sprintf("Note %d", i), State: domain.NoteActive, CreatedAt: now, UpdatedAt: now}
		if i%10 == 0 {
			n.When = []string{fmt.Sprintf("bash:tool%d run", i%40), "edit:ui/dist/**"}
		}
		if err := store.SaveNote(repo, n); err != nil {
			tb.Fatal(err)
		}
	}
	return buildIndex(tb, repo), repo
}

func BenchmarkNotesForCommand(b *testing.B) {
	cache, repo := commandBench(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := NotesForCommand(context.Background(), cache, repo, "cd x && tool10 run --all"); err != nil {
			b.Fatal(err)
		}
	}
}

// TestNotesForCommandLatency pins criterion 9: median <= 20 ms over 1,000
// notes, 100 of them with triggers.
func TestNotesForCommandLatency(t *testing.T) {
	cache, repo := commandBench(t)
	var ds []time.Duration
	for i := 0; i < 50; i++ {
		s := time.Now()
		got, err := NotesForCommand(context.Background(), cache, repo, "cd x && tool10 run --all")
		if err != nil {
			t.Fatal(err)
		}
		if len(got) == 0 {
			t.Fatal("bench command matched nothing")
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
