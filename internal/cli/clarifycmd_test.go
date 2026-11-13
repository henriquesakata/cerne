package cli

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/app"
)

func clarifySpecBody() string {
	return "# Feature\n\n## Problema\n\nContexto real.\n\n" +
		"[ESCLARECER: sincrona ou assincrona? | sincrona | assincrona]\n\n" +
		"[CLARIFY: which format?]\n\n" +
		"## Verificação\n\nBarra: fixture de teste — referência nomeada\nPergunta ao juiz: qual é mais claro?\n"
}

func readBackendSpec(t *testing.T, repoDir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoDir, "content", "features", "F-0001", "spec.md"))
	if err != nil {
		t.Fatalf("reading backend spec: %v", err)
	}
	return string(data)
}

func TestClarifyListHumanAndJSON(t *testing.T) {
	projDir, _, _ := setupProject(t)
	newFeatureWithSpec(t, projDir, "Com perguntas", clarifySpecBody())

	out, err := execCerne(t, "clarify", "list", "--dir", projDir)
	if err != nil {
		t.Fatalf("clarify list: %v\n%s", err, out)
	}
	for _, want := range []string{"F-0001", "#1", "sincrona ou assincrona?", "sincrona | assincrona", "#2", "which format?"} {
		if !strings.Contains(out, want) {
			t.Errorf("list output missing %q:\n%s", want, out)
		}
	}

	out, err = execCerne(t, "clarify", "list", "F-0001", "--json", "--dir", projDir)
	if err != nil {
		t.Fatalf("clarify list --json: %v\n%s", err, out)
	}
	var items []app.OpenClarification
	if err := json.Unmarshal([]byte(out), &items); err != nil {
		t.Fatalf("list --json is not a JSON array: %v\n%s", err, out)
	}
	if len(items) != 2 || items[0].Index != 1 || items[0].Question != "sincrona ou assincrona?" ||
		len(items[0].Options) != 2 || items[1].Question != "which format?" {
		t.Fatalf("list --json = %+v", items)
	}
}

func TestClarifyListEmptyAndUnknown(t *testing.T) {
	projDir, _, _ := setupProject(t)

	out, err := execCerne(t, "clarify", "list", "--dir", projDir)
	if err != nil {
		t.Fatalf("clarify list on empty project: %v\n%s", err, out)
	}
	if !strings.Contains(out, "no open clarifications") {
		t.Errorf("empty list output = %q, want 'no open clarifications'", out)
	}

	if out, err = execCerne(t, "clarify", "list", "F-9999", "--dir", projDir); err == nil {
		t.Fatalf("clarify list F-9999 succeeded, want unknown-id error:\n%s", out)
	}
}

func TestClarifyAnswerFreeText(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	newFeatureWithSpec(t, projDir, "Com perguntas", clarifySpecBody())

	out, err := execCerne(t, "clarify", "answer", "F-0001", "2", "--answer", "json", "--dir", projDir)
	if err != nil {
		t.Fatalf("clarify answer: %v\n%s", err, out)
	}
	if !strings.Contains(out, "which format?") || !strings.Contains(out, "json") {
		t.Errorf("answer echo missing question/answer:\n%s", out)
	}
	spec := readBackendSpec(t, repoDir)
	if !strings.Contains(spec, "[CLARIFIED: which format? → json]") {
		t.Errorf("spec not rewritten in en vocabulary:\n%s", spec)
	}
	if !strings.Contains(spec, "[ESCLARECER: sincrona ou assincrona? | sincrona | assincrona]") {
		t.Errorf("untouched marker was modified:\n%s", spec)
	}
}

func TestClarifyAnswerPick(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	newFeatureWithSpec(t, projDir, "Com perguntas", clarifySpecBody())

	out, err := execCerne(t, "clarify", "answer", "F-0001", "1", "--pick", "2", "--dir", projDir)
	if err != nil {
		t.Fatalf("clarify answer --pick: %v\n%s", err, out)
	}
	if !strings.Contains(readBackendSpec(t, repoDir), "[ESCLARECIDO: sincrona ou assincrona? → assincrona]") {
		t.Errorf("picked option not written:\n%s", readBackendSpec(t, repoDir))
	}
}

func TestClarifyAnswerUsageErrors(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	newFeatureWithSpec(t, projDir, "Com perguntas", clarifySpecBody())
	before := readBackendSpec(t, repoDir)

	cases := [][]string{
		{"clarify", "answer", "F-0001", "1", "--dir", projDir},                                 // neither flag
		{"clarify", "answer", "F-0001", "1", "--answer", "x", "--pick", "1", "--dir", projDir}, // both flags
		{"clarify", "answer", "F-0001", "1", "--pick", "9", "--dir", projDir},                  // pick out of range
		{"clarify", "answer", "F-0001", "2", "--pick", "1", "--dir", projDir},                  // pick without options
	}
	for _, args := range cases {
		if out, err := execCerne(t, args...); err == nil {
			t.Errorf("%v succeeded, want usage error:\n%s", args, out)
		}
	}
	if got := readBackendSpec(t, repoDir); got != before {
		t.Error("a refused answer modified the spec")
	}

	_, err := execCerne(t, "clarify", "answer", "F-0001", "99", "--answer", "x", "--dir", projDir)
	if !errors.Is(err, app.ErrClarificationGone) {
		t.Fatalf("out-of-range index err = %v, want ErrClarificationGone", err)
	}
}

// TestClarifyAnswerThenApprove pins spec criteria 6 and 7 (F-0193): the
// refused approve names the CLI path, and answering every open marker opens
// the gate without --force.
func TestClarifyAnswerThenApprove(t *testing.T) {
	projDir, _, _ := setupProject(t)
	newFeatureWithSpec(t, projDir, "Com perguntas", clarifySpecBody())

	_, err := execCerne(t, "feature", "approve", "F-0001", "--dir", projDir)
	if err == nil {
		t.Fatal("approve with open clarifications succeeded, want refusal")
	}
	if !strings.Contains(err.Error(), "cerne clarify") {
		t.Errorf("refusal does not point at the CLI answer path: %v", err)
	}

	if out, err := execCerne(t, "clarify", "answer", "F-0001", "1", "--pick", "1", "--dir", projDir); err != nil {
		t.Fatalf("answer #1: %v\n%s", err, out)
	}
	if out, err := execCerne(t, "clarify", "answer", "F-0001", "1", "--answer", "json", "--dir", projDir); err != nil {
		t.Fatalf("answer remaining: %v\n%s", err, out)
	}
	if out, err := execCerne(t, "feature", "approve", "F-0001", "--dir", projDir); err != nil {
		t.Fatalf("approve after answering: %v\n%s", err, out)
	}
}

// F-0197: a fila de correção e o verbo que a consome.
func TestClarifyListResolved(t *testing.T) {
	projDir, _, _ := setupProject(t)
	newFeatureWithSpec(t, projDir, "Com perguntas", clarifySpecBody())
	if out, err := execCerne(t, "clarify", "answer", "F-0001", "1", "--pick", "1", "--dir", projDir); err != nil {
		t.Fatalf("seeding an answer: %v\n%s", err, out)
	}

	out, err := execCerne(t, "clarify", "list", "--resolved", "--dir", projDir)
	if err != nil {
		t.Fatalf("clarify list --resolved: %v\n%s", err, out)
	}
	if !strings.Contains(out, "sincrona ou assincrona?") {
		t.Errorf("resolved listing missing the answered question:\n%s", out)
	}
	// A pergunta ainda ABERTA não pertence à fila de correção.
	if strings.Contains(out, "which format?") {
		t.Errorf("resolved listing leaked an open clarification:\n%s", out)
	}
}

func TestClarifyCorrectAppendsTrail(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	newFeatureWithSpec(t, projDir, "Com perguntas", clarifySpecBody())
	if out, err := execCerne(t, "clarify", "answer", "F-0001", "1", "--pick", "1", "--dir", projDir); err != nil {
		t.Fatalf("seeding an answer: %v\n%s", err, out)
	}

	out, err := execCerne(t, "clarify", "correct", "F-0001", "1", "--answer", "assincrona", "--dir", projDir)
	if err != nil {
		t.Fatalf("clarify correct: %v\n%s", err, out)
	}
	spec := readBackendSpec(t, repoDir)
	if !strings.Contains(spec, "[ESCLARECIDO: sincrona ou assincrona? → sincrona → assincrona]") {
		t.Errorf("trail not written to the backend doc:\n%s", spec)
	}
}

func TestClarifyCorrectIndexOutOfRange(t *testing.T) {
	projDir, _, _ := setupProject(t)
	newFeatureWithSpec(t, projDir, "Com perguntas", clarifySpecBody())

	out, err := execCerne(t, "clarify", "correct", "F-0001", "99", "--answer", "x", "--dir", projDir)
	if err == nil {
		t.Fatalf("clarify correct with an out-of-range index succeeded:\n%s", out)
	}
	if !errors.Is(err, app.ErrClarificationGone) {
		t.Fatalf("err = %v, want ErrClarificationGone", err)
	}
}

// F-0330 (critic C8): the refusal's command run as typed would record its
// placeholder as the human's answer; clarify answer refuses "...", "…" and
// what doccheck.Unwritten reads as template — blank, <…> slots, quoted or
// emphasized slots — and writes nothing. An autolink is a written answer.
func TestClarifyAnswerRefusesPlaceholder(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	newFeatureWithSpec(t, projDir, "Com perguntas", clarifySpecBody())
	before := readBackendSpec(t, repoDir)
	for _, ph := range []string{"...", " … ", "<resposta>", "   ", `""`, `"<resposta>"`, "“<k>”", "**<k>**", "`<k>`", "<a> / <b>"} {
		_, err := execCerne(t, "clarify", "answer", "F-0001", "2", "--answer", ph, "--dir", projDir)
		if !errors.Is(err, errPlaceholderAnswer) {
			t.Errorf("--answer %q: err = %v, want errPlaceholderAnswer", ph, err)
		}
	}
	if got := readBackendSpec(t, repoDir); got != before {
		t.Fatalf("a placeholder answer wrote the spec:\n%s", got)
	}
	if out, err := execCerne(t, "clarify", "answer", "F-0001", "2", "--answer", "<https://example.com/formato>", "--dir", projDir); err != nil {
		t.Fatalf("an autolink is a written answer: %v\n%s", err, out)
	}
}
