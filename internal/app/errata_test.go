package app

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
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
	resultPath := filepath.Join(filepath.Dir(specPath), "result.md")
	resultBody := []byte("# Result\n\ncritério 1\n")
	if err := os.WriteFile(resultPath, resultBody, 0o644); err != nil {
		t.Fatal(err)
	}
	commitBackend(t, repoDir)

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
	if got, _ := os.ReadFile(resultPath); !bytes.Equal(got, resultBody) {
		t.Fatalf("result was mutated by errata (must stay byte-identical):\n%s", got)
	}
}

func TestAddErrataEventActor(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	if err := store.SaveFeature(repoDir, store.Feature{ID: "F-0001", Title: "X", State: domain.StateImplemented}); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}
	commitBackend(t, repoDir)
	if _, err := AddErrata(repoDir, projDir, "F-0001", "auto texto", "drift-auto"); err != nil {
		t.Fatalf("AddErrata: %v", err)
	}
	events, err := store.ReadEvents(repoDir)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	for _, ev := range events {
		if ev.Type == "errata.added" && ev.EntityID == "F-0001" {
			if ev.Actor != "drift-auto" {
				t.Fatalf("errata.added actor = %q, want %q", ev.Actor, "drift-auto")
			}
			return
		}
	}
	t.Fatalf("errata.added event not found")
}

// TestErrataExcerpt pins F-0163 crit. 5: the excerpt is the first line,
// truncated at 120 runes with an ellipsis when cut.
func TestErrataExcerpt(t *testing.T) {
	long := strings.Repeat("á", 130)
	tests := []struct {
		name, in, want string
	}{
		{"short single line stays whole", "Correção curta.", "Correção curta."},
		{"multiline keeps only the first line", "Primeira linha.\nSegunda linha.", "Primeira linha."},
		{"long line truncated at 120 runes", long, strings.Repeat("á", 120) + "…"},
		{"leading whitespace trimmed", "  espaço à esquerda", "espaço à esquerda"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := errataExcerpt(tc.in); got != tc.want {
				t.Fatalf("errataExcerpt(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestAddErrataEventCarriesExcerpt(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	if err := store.SaveFeature(repoDir, store.Feature{ID: "F-0001", Title: "X", State: domain.StateImplemented}); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}
	commitBackend(t, repoDir)
	if _, err := AddErrata(repoDir, projDir, "F-0001", "Referência movida para o sucessor.\nDetalhe longo.", "ator"); err != nil {
		t.Fatalf("AddErrata: %v", err)
	}
	events, err := store.ReadEvents(repoDir)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	for _, ev := range events {
		if ev.Type == "errata.added" && ev.EntityID == "F-0001" {
			if got := ev.Payload["excerpt"]; got != "Referência movida para o sucessor." {
				t.Fatalf("excerpt = %q, want first line", got)
			}
			return
		}
	}
	t.Fatalf("errata.added event not found")
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
