package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// seedInformingNotes saves n active notes informing id straight into the
// clone — each with a gist and a body carrying a marker — and pushes them.
func seedInformingNotes(t *testing.T, repoDir, id string, n int) {
	t.Helper()
	now := time.Now().UTC()
	for i := 1; i <= n; i++ {
		nid := fmt.Sprintf("N-%04d", i)
		if err := store.SaveNote(repoDir, store.Note{ID: nid, Title: fmt.Sprintf("Rule %d", i), State: domain.NoteActive,
			Informs: []string{id}, Gist: fmt.Sprintf("gist %d", i), CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(repoDir, "content", "notes", nid+".md")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("# Rule\n\nBODY-MARKER of the rule.\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	commitBackend(t, repoDir)
}

// approvedWithPlan is F-0001, approved, its plan written: ready to start.
func approvedWithPlan(t *testing.T, projDir, repoDir string) string {
	t.Helper()
	id := newFeature(t, projDir, "Feature A", 1)
	writeDoc(t, repoDir, id, "spec.md", realDoc(id, "Problema real descrito aqui."))
	commitBackend(t, repoDir)
	mustCerne(t, "feature", "approve", id, "--dir", projDir)
	writeDoc(t, repoDir, id, "plan.md", realDoc(id, "Passo 1: implementar."))
	commitBackend(t, repoDir)
	return id
}

func outLines(out string) []string { return strings.Split(strings.TrimRight(out, "\n"), "\n") }

// stdoutOf runs cerne and returns its stdout alone: what `| tail -1` reads.
func stdoutOf(t *testing.T, args ...string) string {
	t.Helper()
	out, errOut, err := execCerneSplit(t, args...)
	if err != nil {
		t.Fatalf("cerne %s: %v\n%s%s", strings.Join(args, " "), err, out, errOut)
	}
	return out
}

var indexLineRe = regexp.MustCompile(`(?m)^  \[a\] N-\d{4}  Rule \d+ — gist \d+$`)

// E-0005 criterion 9 (T-0017): feature start ends with the index of the notes
// of rings (a) to (c) — ten, then the count of the rest — and the package
// command; no body is printed.
func TestBindIndexOnFeatureStart(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	id := approvedWithPlan(t, projDir, repoDir)
	seedInformingNotes(t, repoDir, id, 12)
	out := stdoutOf(t, "feature", "start", id, "--dir", projDir)
	if n := len(indexLineRe.FindAllString(out, -1)); n != 10 {
		t.Fatalf("%d index lines, want 10:\n%s", n, out)
	}
	if !strings.Contains(out, "  and 2 more: cerne context F-0001\n") {
		t.Fatalf("no count of the rest:\n%s", out)
	}
	if last := outLines(out)[len(outLines(out))-1]; last != "Task package: cerne context F-0001" {
		t.Fatalf("last line %q, want the package command:\n%s", last, out)
	}
	if strings.Contains(out, "BODY-MARKER") {
		t.Fatalf("a note body was printed:\n%s", out)
	}
}

func TestBindIndexWithNoNote(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	id := approvedWithPlan(t, projDir, repoDir)
	out := stdoutOf(t, "feature", "start", id, "--dir", projDir)
	lines := outLines(out)
	if lines[len(lines)-2] != "No note informs F-0001 by a link, a cited path or a shared component." || lines[len(lines)-1] != "Task package: cerne context F-0001" {
		t.Fatalf("no-note index:\n%s", out)
	}
}

// Criterion 9 on work start --ref: the same index, and the last line still
// names the created work, so `cerne work start … | tail -1` keeps binding.
func TestBindIndexOnWorkStartKeepsTheTailBinding(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	newFeature(t, projDir, "Feature A", 1)
	seedInformingNotes(t, repoDir, "F-0001", 3)
	out := stdoutOf(t, "work", "start", "Investigar erro X", "--ref", "F-0001", "--dir", projDir)
	if n := len(indexLineRe.FindAllString(out, -1)); n != 3 {
		t.Fatalf("%d index lines, want 3:\n%s", n, out)
	}
	last := outLines(out)[len(outLines(out))-1]
	if last != "W-0001 created (active) — task package: cerne context F-0001" {
		t.Fatalf("last line %q:\n%s", last, out)
	}
	stdin := hookJSON(t, "sess-tail", projDir, "Bash",
		map[string]any{"command": `cerne work start "Investigar erro X" --ref F-0001 | tail -1`}, map[string]any{"stdout": last})
	if o, err := execCerneStdin(t, stdin, "hook", "bind-work"); err != nil {
		t.Fatalf("bind-work: %v\n%s", err, o)
	}
	if id, ok, err := app.CurrentBinding(filepath.Dir(repoDir), repoDir, "sess-tail", ""); err != nil || !ok || id != "W-0001" {
		t.Fatalf("binding = (%q, %v, %v), want W-0001", id, ok, err)
	}
	// Without a feature ref there is no index and nothing changes.
	plain := mustCerne(t, "work", "start", "Revisar o MR", "--dir", projDir)
	if strings.Contains(plain, "cerne context") {
		t.Fatalf("an index without a feature ref:\n%s", plain)
	}
}

// Criterion 9 on handoff take: the new work inherits the origin's refs; the
// take ends with the index and a last line that binds.
func TestBindIndexOnHandoffTake(t *testing.T) {
	projDir, repoDir := handoffProject(t)
	seedInformingNotes(t, repoDir, "F-0001", 2)
	out := stdoutOf(t, "handoff", "take", "HO-0001", "--note", "li", "--dir", projDir)
	if n := len(indexLineRe.FindAllString(out, -1)); n != 2 {
		t.Fatalf("%d index lines, want 2:\n%s", n, out)
	}
	last := outLines(out)[len(outLines(out))-1]
	if !workCreatedRe.MatchString(last) || !strings.Contains(last, "cerne context F-0001") {
		t.Fatalf("last line %q does not bind or name the package:\n%s", last, out)
	}
}
