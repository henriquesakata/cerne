package cli

import (
	"errors"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/clarify"
)

// F-0317: a marker broken across a soft line break is one marker on every
// surface. mlPre/mlPost surround the marker so "no byte outside it
// touched" is checkable.
const (
	mlPre  = "# Feature\n\n## Problema\n\nContexto real.\n\nAntes "
	mlPost = " depois.\n\n## Verificação\n\nBarra: fixture de teste — referência nomeada\nPergunta ao juiz: qual é mais claro?\n"
)

func multiLineSpec(marker string) string { return mlPre + marker + mlPost }

func TestClarifyListMultiLineMarker(t *testing.T) {
	projDir, _, _ := setupProject(t)
	newFeatureWithSpec(t, projDir, "Quebrada", multiLineSpec("[ESCLARECER: sincrona ou\n  assincrona? | sincrona | assincrona]"))

	out, err := execCerne(t, "clarify", "list", "F-0001", "--dir", projDir)
	if err != nil {
		t.Fatalf("clarify list: %v\n%s", err, out)
	}
	for _, want := range []string{"#1", "sincrona ou assincrona?"} {
		if !strings.Contains(out, want) {
			t.Errorf("list output missing %q:\n%s", want, out)
		}
	}
}

func TestClarifyAnswerMultiLineMarker(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	newFeatureWithSpec(t, projDir, "Quebrada", multiLineSpec("[ESCLARECER: sincrona ou\n  assincrona?]"))

	if out, err := execCerne(t, "clarify", "answer", "F-0001", "1", "--answer", "sincrona", "--dir", projDir); err != nil {
		t.Fatalf("clarify answer: %v\n%s", err, out)
	}
	spec := readBackendSpec(t, repoDir)
	if want := multiLineSpec("[ESCLARECIDO: sincrona ou assincrona? → sincrona]"); !strings.Contains(spec, want) {
		t.Errorf("spec not rewritten in place as one line, bytes outside touched?\ngot:\n%s", spec)
	}
	for _, c := range clarify.Parse(spec) {
		if !c.Resolved {
			t.Errorf("reparse still has an open marker: %+v", c)
		}
	}

	// In a fresh project, correcting a resolved TWO-line marker keeps the
	// answer trail and rewrites it in place as one line.
	projDir2, _, repoDir2 := setupProject(t)
	newFeatureWithSpec(t, projDir2, "Quebrada resolvida", multiLineSpec("[ESCLARECIDO: sincrona ou\n  assincrona? → sincrona]"))
	if out, err := execCerne(t, "clarify", "correct", "F-0001", "1", "--answer", "assincrona", "--dir", projDir2); err != nil {
		t.Fatalf("clarify correct: %v\n%s", err, out)
	}
	if want := multiLineSpec("[ESCLARECIDO: sincrona ou assincrona? → sincrona → assincrona]"); !strings.Contains(readBackendSpec(t, repoDir2), want) {
		t.Errorf("correct did not preserve the trail in place:\n%s", readBackendSpec(t, repoDir2))
	}
}

func TestApproveRefusesMultiLineClarification(t *testing.T) {
	projDir, _, _ := setupProject(t)
	newFeatureWithSpec(t, projDir, "Quebrada", multiLineSpec("[ESCLARECER: qual\n  formato usar?]"))

	out, err := execCerne(t, "feature", "approve", "F-0001", "--dir", projDir)
	if err == nil {
		t.Fatalf("approve with a two-line open marker succeeded, want refusal:\n%s", out)
	}
	if !errors.Is(err, app.ErrOpenClarifications) {
		t.Fatalf("err = %v, want ErrOpenClarifications", err)
	}
	if !strings.Contains(err.Error(), "qual formato usar?") {
		t.Errorf("error missing the normalized question: %v", err)
	}
}
