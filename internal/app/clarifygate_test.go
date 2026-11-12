package app

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// gateBarTail gives a fixture spec a valid bar, so the refusal has no hard
// impediment behind it.
const gateBarTail = "\n## Verificação\n\nBarra: fixture de teste — referência nomeada\n"

// clarifyGateFixture seeds candidate F-0007 with spec plus a valid bar (and an
// open marker in the plan, which the gate must not list) and returns the
// gate's refusal.
func clarifyGateFixture(t *testing.T, spec string) (repoDir string, err error) {
	t.Helper()
	return clarifyGateSeed(t, spec+gateBarTail)
}

// clarifyGateSeed is clarifyGateFixture with the spec exactly as given.
func clarifyGateSeed(t *testing.T, spec string) (repoDir string, err error) {
	t.Helper()
	repoDir = t.TempDir()
	// A complete card (F-0336): the refusal under test is the clarification one alone.
	if err := store.SaveFeature(repoDir, store.Feature{ID: "F-0007", Title: "x", State: domain.StateCandidate, Components: []string{"cli"}}); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}
	dir := filepath.Join(repoDir, "content", "features", "F-0007")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"spec.md": spec, "plan.md": "[ESCLARECER: do plano?]\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	err = CheckOpenClarifications(repoDir, t.TempDir(), "F-0007")
	if !errors.Is(err, ErrOpenClarifications) {
		t.Fatalf("err = %v, want ErrOpenClarifications", err)
	}
	return repoDir, err
}

// lineWith returns the first refusal line containing s ("" when none).
func lineWith(msg, s string) string {
	for _, l := range strings.Split(msg, "\n") {
		if strings.Contains(l, s) {
			return l
		}
	}
	return ""
}

// F-0330 crit. 1: every question carries the number clarify answer accepts —
// the Index ListClarifications gives it, not a recount — with its command.
func TestApproveClarifyRefusalNumbersQuestions(t *testing.T) {
	repoDir, err := clarifyGateFixture(t, "[ESCLARECER: qual formato usar?]\n\n[ESCLARECER: qual o prazo?]\n")
	items, lerr := ListClarifications(repoDir, "F-0007")
	if lerr != nil {
		t.Fatalf("ListClarifications: %v", lerr)
	}
	msg := err.Error()
	for _, c := range items {
		if !strings.HasSuffix(c.Doc, "/spec.md") {
			if strings.Contains(msg, c.Question) {
				t.Errorf("refusal lists a non-spec question %q:\n%s", c.Question, msg)
			}
			continue
		}
		n := strconv.Itoa(c.Index)
		line := lineWith(msg, c.Question)
		cmd := `→ cerne clarify answer F-0007 ` + n + ` --expect ` + markerKey(c.Raw) + ` --answer "..."`
		if !strings.HasPrefix(strings.TrimSpace(line), n+". "+c.Question) || !strings.Contains(line, cmd) {
			t.Errorf("question %q (index %d): line %q, want it numbered %s with %q", c.Question, c.Index, line, n, cmd)
		}
	}
}

// F-0330 (critic C2): the gate refuses whatever id the core accepts — a
// lowercase id is canonicalized, not read as unknown — and an id it cannot
// load fails closed instead of passing.
func TestApproveClarifyGateCanonicalAndFailClosed(t *testing.T) {
	repoDir, _ := clarifyGateFixture(t, "[ESCLARECER: qual o prazo?]\n")
	err := CheckOpenClarifications(repoDir, t.TempDir(), "f-0007")
	if !errors.Is(err, ErrOpenClarifications) || !strings.Contains(err.Error(), "cerne clarify answer F-0007 1") {
		t.Errorf("lowercase id: %v, want the refusal with the canonical id", err)
	}
	if err := CheckOpenClarifications(repoDir, t.TempDir(), "F-9999"); !errors.Is(err, store.ErrNotFound) || !strings.Contains(err.Error(), "F-9999") {
		t.Errorf("unknown feature: %v, want a not-found error naming it (fail closed)", err)
	}
}

// F-0330 crit. 2: a marker with options shows them numbered and the --pick
// form on the question's own line — and that free text is accepted too
// (critic C7).
func TestApproveClarifyRefusalShowsOptions(t *testing.T) {
	_, err := clarifyGateFixture(t, "[ESCLARECER: a barra cobre a CLI e a UI? | só CLI | as duas]\n")
	line := lineWith(err.Error(), "a barra cobre a CLI e a UI?")
	for _, want := range []string{"cerne clarify answer F-0007 1 --expect ", " --pick <k>", "(1: só CLI, 2: as duas — or free text: --answer \"...\")"} {
		if !strings.Contains(line, want) {
			t.Errorf("question line %q missing %q", line, want)
		}
	}
}

// F-0330 crit. 3, 4: the refusal ends with the re-approve, and --force is told
// by what it does on a line of its own, after the answer paths.
func TestApproveClarifyRefusalSaysRerunAndForce(t *testing.T) {
	_, err := clarifyGateFixture(t, "[ESCLARECER: qual formato usar?]\n\n[ESCLARECER: qual o prazo?]\n")
	lines := strings.Split(err.Error(), "\n")
	rerun, force := -1, -1
	for i, l := range lines {
		switch {
		case l == "then run 'cerne feature approve F-0007' again.":
			rerun = i
		case strings.HasPrefix(l, "--force ") && strings.Contains(l, "approves with these questions still open in the spec"):
			force = i
		case strings.Contains(l, "cerne clarify answer") && strings.Contains(l, "--force"):
			t.Errorf("an answer line mentions --force: %q", l)
		}
	}
	if rerun < 0 || force < 0 || force < rerun {
		t.Fatalf("want the re-approve line, then the --force line (rerun=%d force=%d):\n%s", rerun, force, err)
	}
}

// F-0330 (critic C1): an expect key names the marker a command was printed
// for; when the n-th open marker is another one — the numbering moved — the
// answer is refused before anything is written.
func TestAnswerClarificationExpectRefusesMovedMarker(t *testing.T) {
	repoDir, projDir := newSnapshotBackend(t, false)
	seedClarifyFixture(t, repoDir)
	items, err := ListClarifications(repoDir, "F-0001")
	if err != nil {
		t.Fatalf("ListClarifications: %v", err)
	}
	specPath := filepath.Join(repoDir, "content", "features", "F-0001", "spec.md")
	before, _ := os.ReadFile(specPath)

	_, err = AnswerClarification(repoDir, projDir, "F-0001", 2, markerKey(items[0].Raw), "x")
	if !errors.Is(err, ErrClarificationMoved) {
		t.Fatalf("moved marker: err = %v, want ErrClarificationMoved", err)
	}
	for _, want := range []string{items[1].Question, "nothing was written", "'cerne feature approve F-0001'"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal missing %q: %v", want, err)
		}
	}
	if after, _ := os.ReadFile(specPath); string(after) != string(before) {
		t.Fatalf("a refused answer wrote the spec:\n%s", after)
	}
	if _, err := AnswerClarification(repoDir, projDir, "F-0001", 1, markerKey(items[0].Raw), "sincrona"); err != nil {
		t.Fatalf("matching key: %v", err)
	}
}

// orderLine is the refusal's line on order, true as displayed (critic C3):
// rows are last first, so an answer renumbers the rows above it.
const orderLine = "answer them top to bottom: each answer renumbers only the questions listed above it (a command whose number moved is refused, nothing written)."

// F-0330 (critic C10): the rows are printed last first and the order line
// says so — reverting to document order, or dropping the line, fails here.
func TestApproveClarifyRefusalOrderAndGuardLine(t *testing.T) {
	_, err := clarifyGateFixture(t, "[ESCLARECER: pergunta A?]\n\n[ESCLARECER: pergunta B?]\n\n[ESCLARECER: pergunta C?]\n")
	var rows []string
	for _, l := range strings.Split(err.Error(), "\n") {
		if strings.Contains(l, "→ cerne clarify answer") {
			rows = append(rows, strings.Fields(l)[0])
		}
	}
	if strings.Join(rows, " ") != "3. 2. 1." {
		t.Errorf("rows printed %v, want 3. 2. 1. (last first)", rows)
	}
	if lineWith(err.Error(), "top to bottom") != orderLine {
		t.Errorf("order line = %q, want %q", lineWith(err.Error(), "top to bottom"), orderLine)
	}
	if _, err := clarifyGateFixture(t, "[ESCLARECER: só uma?]\n"); strings.Contains(err.Error(), "top to bottom") {
		t.Errorf("one question needs no order line:\n%v", err)
	}
}

// F-0330 (critic C4): with a hard impediment behind the questions the refusal
// leads with it, and neither the re-approve nor --force is told as if it
// would succeed.
func TestApproveClarifyRefusalLeadsWithHardImpediments(t *testing.T) {
	_, err := clarifyGateSeed(t, "[ESCLARECER: qual o limite?]\n")
	lines := strings.Split(err.Error(), "\n")
	if len(lines) < 3 || !strings.Contains(lines[1], "bar-missing") || !strings.Contains(lines[1], "answering them (or --force) is not enough") || !strings.Contains(lines[1], "'cerne feature approve F-0007 --check'") {
		t.Fatalf("the hard impediment must come right after the header:\n%v", err)
	}
	if l := lineWith(err.Error(), "then run 'cerne feature approve F-0007' again"); l != "" {
		t.Errorf("unconditional re-approve with a hard impediment: %q", l)
	}
	if l := lineWith(err.Error(), "--force "); strings.Contains(l, "approves") || !strings.Contains(l, "bar-missing") {
		t.Errorf("--force line = %q, want it to say it does not get past bar-missing", l)
	}
	if l := lineWith(err.Error(), "run 'cerne feature approve F-0007' again"); !strings.Contains(l, "--check") {
		t.Errorf("re-approve line = %q, want it conditioned on the check", l)
	}
}

// runeCol is the rune offset of sub in l (-1 when absent).
func runeCol(l, sub string) int {
	i := strings.Index(l, sub)
	if i < 0 {
		return -1
	}
	return utf8.RuneCountInString(l[:i])
}

// F-0330 (critic C5, C9): one long question does not push every command off
// the screen — alignment is capped and a longer row is left unpadded, whole —
// and with 10+ questions the numbers are right-aligned, so the question and
// command columns line up between one- and two-digit rows.
func TestApproveClarifyRefusalAlignment(t *testing.T) {
	long := "pergunta longa " + strings.Repeat("muito ", 20) + "longa?"
	var spec strings.Builder
	for i := 1; i <= 11; i++ {
		q := "pergunta curta " + strconv.Itoa(i) + "?"
		if i == 5 {
			q = long
		}
		spec.WriteString("[ESCLARECER: " + q + "]\n\n")
	}
	_, err := clarifyGateFixture(t, spec.String())
	arrows, questions := map[int]bool{}, map[int]bool{}
	for _, l := range strings.Split(err.Error(), "\n") {
		if !strings.Contains(l, "→ cerne clarify answer") {
			continue
		}
		if strings.Contains(l, long) {
			if !strings.Contains(l, long+"  → cerne") {
				t.Errorf("long row should be whole and unpadded: %q", l)
			}
			continue
		}
		arrows[runeCol(l, "→")] = true
		questions[runeCol(l, "pergunta")] = true
	}
	if len(arrows) != 1 || len(questions) != 1 {
		t.Fatalf("short rows misaligned: arrow cols %v, question cols %v\n%v", arrows, questions, err)
	}
	for col := range arrows {
		if col > 60 {
			t.Errorf("arrow at column %d: the long question padded every row\n%v", col, err)
		}
	}
}
