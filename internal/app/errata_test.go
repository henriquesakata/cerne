package app

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

func TestAddErrataAppendsWithoutTouchingBody(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	if err := store.SaveFeature(repoDir, store.Feature{ID: "F-0001", Title: "Boletos", State: domain.StateImplemented}); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}
	specPath := filepath.Join(repoDir, "content", "features", "F-0001", "spec.md")
	if err := os.MkdirAll(filepath.Dir(specPath), 0o755); err != nil {
		t.Fatal(err)
	}
	specBody := []byte("# Boletos\n\nDecisão base: D-0003.\n")
	if err := os.WriteFile(specPath, specBody, 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := AddErrata(repoDir, projDir, "F-0001", "Correção: usar D-0002.", "ator"); err != nil {
		t.Fatalf("AddErrata: %v", err)
	}

	er, err := store.LoadErrata(repoDir, "F-0001")
	if err != nil || !bytes.Contains([]byte(er), []byte("usar D-0002")) {
		t.Fatalf("errata missing text: %q (err %v)", er, err)
	}
	after, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, specBody) {
		t.Fatalf("spec was mutated by errata (must stay byte-identical):\n%s", after)
	}
}

func TestAddErrataEntityNotFoundErrors(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	if _, err := AddErrata(repoDir, projDir, "F-9999", "x", "ator"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestAddErrataUnknownPrefixErrors(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	if _, err := AddErrata(repoDir, projDir, "X-0001", "x", "ator"); err == nil {
		t.Fatalf("want error for unsupported prefix, got nil")
	}
}

func TestAddErrataEmptyTextErrors(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	if err := store.SaveFeature(repoDir, store.Feature{ID: "F-0001", Title: "X", State: domain.StateImplemented}); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}
	if _, err := AddErrata(repoDir, projDir, "F-0001", "   ", "ator"); err == nil {
		t.Fatalf("want error for empty text, got nil")
	}
}
