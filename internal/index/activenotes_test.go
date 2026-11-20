package index

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// E-0005 criterion 6, ring (d): only active notes, filtered before the cut —
// features and a superseded note that match better never take a slot — and a
// missing index answers nothing without creating the file.
func TestRankActiveNotes(t *testing.T) {
	repoDir := t.TempDir()
	now := time.Now().UTC()
	for id, st := range map[string]domain.NoteState{"N-0001": domain.NoteActive, "N-0002": domain.NoteSuperseded, "N-0003": domain.NoteActive} {
		if err := store.SaveNote(repoDir, store.Note{ID: id, Title: "Nota " + id, State: st, CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	writeContent(t, repoDir, "content/notes/N-0001.md", "# N1\n\nO pacote da tarefa lê o índice.\n")
	writeContent(t, repoDir, "content/notes/N-0002.md", "# N2\n\nPacote tarefa índice vínculo gancho: tudo, mas substituída.\n")
	writeContent(t, repoDir, "content/notes/N-0003.md", "# N3\n\nNada a ver com o assunto.\n")
	for _, id := range []string{"F-0001", "F-0002", "F-0003"} {
		if err := store.SaveFeature(repoDir, store.Feature{ID: id, Title: "Pacote tarefa índice vínculo gancho", State: domain.StateInDevelopment, CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
		writeContent(t, repoDir, "content/features/"+id+"/spec.md", "# S\n\npacote tarefa índice vínculo gancho\n")
	}
	dbPath := filepath.Join(t.TempDir(), "index.db")
	if err := Rebuild(repoDir, dbPath, time.Time{}); err != nil {
		t.Fatal(err)
	}
	got, err := RankActiveNotes(dbPath, "pacote da tarefa, índice no vínculo e no gancho", 1)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"N-0001"}) {
		t.Fatalf("RankActiveNotes = %v, want [N-0001]: features and the superseded note never take the slot", got)
	}

	missing := filepath.Join(t.TempDir(), "index.db")
	if got, err := RankActiveNotes(missing, "pacote", 5); err != nil || got != nil {
		t.Fatalf("missing index: %v %v, want nil nil", got, err)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatalf("a missing index was created: %v", err)
	}
}
