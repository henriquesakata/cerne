package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/store"
)

// B-0012: a note with an erratum, pulled. Returns the project dir, the backend
// clone and the materialized file's path.
func notePulledWithErrata(t *testing.T) (projDir, repoDir, file string) {
	t.Helper()
	projDir, _, repoDir, file = notePulledWithErrataAt(t)
	return
}

func notePulledWithErrataAt(t *testing.T) (projDir, origin, repoDir, file string) {
	t.Helper()
	projDir, origin, repoDir = setupProject(t)
	body := filepath.Join(t.TempDir(), "n.md")
	mustWrite(t, body, "A causa do OOM era o decode por GPU.\n")
	rb(t, projDir, "note", "new", "Causa do OOM", "--file", body)
	rb(t, projDir, "note", "errata", "N-0001", "A causa real era o DTS do RTSP andando para tras.")
	rb(t, projDir, "pull")
	m, _ := filepath.Glob(filepath.Join(projDir, ".docs", "current", "notes", "N-0001-*.md"))
	if len(m) != 1 {
		t.Fatalf("materialized note not found: %v", m)
	}
	return projDir, origin, repoDir, m[0]
}

// crit 1: the projection carries the erratum text.
func TestPullProjectsErrataIntoNote(t *testing.T) {
	_, _, file := notePulledWithErrata(t)
	got, _ := os.ReadFile(file)
	if !strings.Contains(string(got), "DTS do RTSP") || !strings.Contains(string(got), "decode por GPU") {
		t.Fatalf("projection misses the erratum or the body:\n%s", got)
	}
}

// crit 1 (runbook): an item already folded into the body is one line, not the text again.
func TestPullProjectsMergedRunbookErrataOnce(t *testing.T) {
	projDir, _ := runbookFixture(t)
	rb(t, projDir, "runbook", "errata", "RB-0001", "Decidida e com passo.", "--altera", "S6.6")
	rb(t, projDir, "runbook", "errata", "RB-0001", "Ainda proposta.", "--altera", "S6.7")
	rb(t, projDir, "runbook", "decide", "RB-0001", "E1", "--by", "dono", "--quote", "vale")
	rb(t, projDir, "runbook", "merge", "RB-0001")
	rb(t, projDir, "pull")
	m, _ := filepath.Glob(filepath.Join(projDir, ".docs", "current", "runbooks", "RB-0001-*.md"))
	if len(m) != 1 {
		t.Fatalf("materialized runbook not found: %v", m)
	}
	got, _ := os.ReadFile(m[0])
	if strings.Count(string(got), "Decidida e com passo.") != 1 || !strings.Contains(string(got), "E1 merged into revision 2") || !strings.Contains(string(got), "Ainda proposta.") {
		t.Fatalf("folded item must appear once (body), open one whole:\n%s", got)
	}
}

// crit 2: the per-type show prints the erratum.
func TestNoteShowPrintsErrata(t *testing.T) {
	projDir, _, _ := notePulledWithErrata(t)
	out := rb(t, projDir, "note", "show", "N-0001")
	if !strings.Contains(out, "DTS do RTSP") {
		t.Fatalf("note show misses the erratum:\n%s", out)
	}
}

// crit 3: a pull with no edit leaves nothing to push; an edit pushes the body only.
func TestErrataProjectionRoundTrip(t *testing.T) {
	projDir, repoDir, file := notePulledWithErrata(t)

	out := rb(t, projDir, "push")
	if strings.Contains(out, "Sent: 1") {
		t.Fatalf("untouched projection must not look like a local edit:\n%s", out)
	}
	// a second pull must not conflict or flag an edit either
	rb(t, projDir, "pull")

	got, _ := os.ReadFile(file)
	edited := strings.Replace(string(got), "A causa do OOM era o decode por GPU.", "Corpo editado localmente.", 1)
	mustWrite(t, file, edited)
	if out := rb(t, projDir, "push"); !strings.Contains(out, "Sent: 1 document(s)") {
		t.Fatalf("edit should be sent:\n%s", out)
	}
	stored, err := store.LoadNoteContent(repoDir, "N-0001")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stored, "Corpo editado localmente.") || strings.Contains(stored, "DTS do RTSP") || strings.Contains(stored, "cerne:errata") {
		t.Fatalf("backend body must hold the edit and no errata text:\n%s", stored)
	}
}

// A remote erratum arriving while a body edit is unsent: pull merges without a
// conflict, keeps the edit and shows the new erratum.
func TestPullRefreshesErrataUnderUnsentEdit(t *testing.T) {
	projDir, _, file := notePulledWithErrata(t)
	got, _ := os.ReadFile(file)
	mustWrite(t, file, strings.Replace(string(got), "decode por GPU", "decode editado", 1))
	rb(t, projDir, "note", "errata", "N-0001", "Segunda correcao chegou depois.")
	out := rb(t, projDir, "pull")
	if !strings.Contains(out, "1 note(s)") {
		t.Fatalf("pull summary must count notes:\n%s", out)
	}
	got, _ = os.ReadFile(file)
	if !strings.Contains(string(got), "decode editado") || !strings.Contains(string(got), "Segunda correcao") || strings.Count(string(got), "cerne:errata:begin") != 1 {
		t.Fatalf("edit kept and new erratum shown, one block:\n%s", got)
	}
}

// Review round: text typed after the block must neither reach the backend as
// body nor duplicate the block on the next pull.
func TestPullKeepsTextAfterBlockWithoutDuplicating(t *testing.T) {
	projDir, repoDir, file := notePulledWithErrata(t)
	got, _ := os.ReadFile(file)
	mustWrite(t, file, string(got)+"\nanotacao final\n")
	rb(t, projDir, "note", "errata", "N-0001", "Outra correcao.")
	rb(t, projDir, "pull")
	got, _ = os.ReadFile(file)
	if strings.Count(string(got), "cerne:errata:begin") != 1 || !strings.Contains(string(got), "anotacao final") || !strings.Contains(string(got), "Outra correcao.") {
		t.Fatalf("one fresh block, trailing text kept:\n%s", got)
	}
	rb(t, projDir, "push")
	stored, _ := store.LoadNoteContent(repoDir, "N-0001")
	if strings.Contains(stored, "cerne:errata") || strings.Contains(stored, "DTS do RTSP") {
		t.Fatalf("backend body carries the block:\n%s", stored)
	}
}

// A block left open is refused by push, never sent.
func TestPushRefusesBrokenErrataBlock(t *testing.T) {
	projDir, repoDir, file := notePulledWithErrata(t)
	got, _ := os.ReadFile(file)
	i := strings.Index(string(got), "<!-- cerne:errata:end -->")
	mustWrite(t, file, string(got)[:i])
	out, err := execCerne(t, "push", "--dir", projDir)
	if err == nil || !strings.Contains(err.Error(), "errata block") {
		t.Fatalf("push must refuse a broken block: err=%v\n%s", err, out)
	}
	if stored, _ := store.LoadNoteContent(repoDir, "N-0001"); strings.Contains(stored, "cerne:errata") {
		t.Fatalf("broken block reached the backend:\n%s", stored)
	}
}

// A same-document conflict keeps MINE (stale block and all): push strips it.
func TestConflictMineStaleBlockStripsOnPush(t *testing.T) {
	projDir, origin, repoDir, file := notePulledWithErrataAt(t)
	got, _ := os.ReadFile(file)
	mustWrite(t, file, strings.Replace(string(got), "decode por GPU", "meu corpo", 1))
	clone2 := filepath.Join(t.TempDir(), "clone2")
	if err := gitback.Clone(origin, clone2); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(clone2, "content", "notes", "N-0001.md"), "---\ncerne_id: N-0001\n---\n\ncorpo remoto\n")
	if _, err := gitback.CommitAndPush(clone2, "remote body edit"); err != nil {
		t.Fatal(err)
	}
	if out, err := execCerne(t, "pull", "--dir", projDir); err == nil || !strings.Contains(out, "Conflict in 1") {
		t.Fatalf("expected a conflict: %v\n%s", err, out)
	}
	if kept, _ := os.ReadFile(file); !strings.Contains(string(kept), "meu corpo") {
		t.Fatalf("mine must stay in .docs/:\n%s", kept)
	}
	rb(t, projDir, "push")
	stored, _ := store.LoadNoteContent(repoDir, "N-0001")
	if !strings.Contains(stored, "meu corpo") || strings.Contains(stored, "cerne:errata") {
		t.Fatalf("pushed mine must be body only:\n%s", stored)
	}
}
