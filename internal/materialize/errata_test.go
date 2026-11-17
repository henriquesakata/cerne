package materialize

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// B-0012: plain and OKF projections carry the erratum; the manifest hash is the
// body's alone, so the generated block never reads as a local edit.
func TestMaterializeProjectsErrata(t *testing.T) {
	for _, okf := range []bool{false, true} {
		dir := seedBackend(t)
		addNote(t, dir, "N-0001", "Causa", domain.NoteActive, "corpo original\n")
		if err := store.AppendErrata(dir, "N-0001", "texto da correcao", "dev", seedTime); err != nil {
			t.Fatal(err)
		}
		docs := t.TempDir()
		m, err := Materialize(dir, docs, "", okf)
		if err != nil {
			t.Fatal(err)
		}
		rel := "current/notes/N-0001-causa.md"
		got, err := os.ReadFile(filepath.Join(docs, filepath.FromSlash(rel)))
		if err != nil || !strings.Contains(string(got), "texto da correcao") {
			t.Fatalf("okf=%v: errata missing (err %v):\n%s", okf, err, got)
		}
		sum := sha256.Sum256(store.StripErrataBlock(got))
		if hex.EncodeToString(sum[:]) != m[rel] || strings.Contains(string(store.StripErrataBlock(got)), "texto da correcao") {
			t.Fatalf("okf=%v: manifest must hash the body without the block", okf)
		}
	}
}
