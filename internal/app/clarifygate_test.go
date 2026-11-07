package app

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

// clarifyGateFixture seeds candidate F-0007 with spec (and an open marker in
// the plan, which the gate must not list) and returns the gate's refusal.
func clarifyGateFixture(t *testing.T, spec string) (repoDir string, err error) {
	t.Helper()
	repoDir = t.TempDir()
	if err := store.SaveFeature(repoDir, store.Feature{ID: "F-0007", Title: "x", State: domain.StateCandidate}); err != nil {
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
	err = CheckOpenClarifications(repoDir, "F-0007")
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
		cmd := `→ cerne clarify answer F-0007 ` + n + ` --answer "..."`
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
	err := CheckOpenClarifications(repoDir, "f-0007")
	if !errors.Is(err, ErrOpenClarifications) || !strings.Contains(err.Error(), "cerne clarify answer F-0007 1") {
		t.Errorf("lowercase id: %v, want the refusal with the canonical id", err)
	}
	if err := CheckOpenClarifications(repoDir, "F-9999"); !errors.Is(err, store.ErrNotFound) || !strings.Contains(err.Error(), "F-9999") {
		t.Errorf("unknown feature: %v, want a not-found error naming it (fail closed)", err)
	}
}

// F-0330 crit. 2: a marker with options shows them numbered and the --pick
// form on the question's own line.
func TestApproveClarifyRefusalShowsOptions(t *testing.T) {
	_, err := clarifyGateFixture(t, "[ESCLARECER: a barra cobre a CLI e a UI? | só CLI | as duas]\n")
	line := lineWith(err.Error(), "a barra cobre a CLI e a UI?")
	for _, want := range []string{"cerne clarify answer F-0007 1 --pick <k>", "(1: só CLI, 2: as duas)"} {
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
