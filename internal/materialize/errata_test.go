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
		stripped, _ := store.StripErrataBlock(got)
		sum := sha256.Sum256(stripped)
		if hex.EncodeToString(sum[:]) != m[rel] || strings.Contains(string(stripped), "texto da correcao") {
			t.Fatalf("okf=%v: manifest must hash the body without the block", okf)
		}
	}
}

// OKF: a live feature's spec carries the erratum, the hash stays the body's.
func TestMaterializeOKFFeatureSpecErrata(t *testing.T) {
	dir := seedBackend(t)
	addFeature(t, dir, "F-0001", "Parcelamento", "", domain.StateApproved, map[string]string{"spec.md": "---\ncerne_id: F-0001\n---\n\n# Spec\n\ncorpo\n"})
	if err := store.AppendErrata(dir, "F-0001", "correcao da spec", "dev", seedTime); err != nil {
		t.Fatal(err)
	}
	docs := t.TempDir()
	m, err := Materialize(dir, docs, "", true)
	if err != nil {
		t.Fatal(err)
	}
	rel := "current/features/F-0001-parcelamento/spec.md"
	got, _ := os.ReadFile(filepath.Join(docs, filepath.FromSlash(rel)))
	stripped, _ := store.StripErrataBlock(got)
	sum := sha256.Sum256(stripped)
	if !strings.Contains(string(got), "correcao da spec") || hex.EncodeToString(sum[:]) != m[rel] {
		t.Fatalf("errata missing or hash includes the block:\n%s", got)
	}
}

// An errata file with only frontmatter renders no block.
func TestMaterializeFrontmatterOnlyErrataNoBlock(t *testing.T) {
	dir := seedBackend(t)
	addNote(t, dir, "N-0001", "Causa", domain.NoteActive, "corpo\n")
	if err := os.MkdirAll(filepath.Join(dir, "content", "errata"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "content", "errata", "N-0001.md"), []byte("---\ncerne_id: N-0001\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	docs := t.TempDir()
	if _, err := Materialize(dir, docs, "", false); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(docs, "current", "notes", "N-0001-causa.md"))
	if string(got) != "corpo\n" {
		t.Fatalf("no block expected: %q", got)
	}
}
