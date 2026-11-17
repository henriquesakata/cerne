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

// Second review round: the exact block recorded at pull is what push removes.

// The begin line deleted: the rest of the block must not reach the backend.
func TestPushRefusesBlockWithBeginLineRemoved(t *testing.T) {
	projDir, repoDir, file := notePulledWithErrata(t)
	got, _ := os.ReadFile(file)
	var kept []string
	for _, l := range strings.Split(string(got), "\n") {
		if !strings.HasPrefix(l, "<!-- cerne:errata:begin") {
			kept = append(kept, l)
		}
	}
	mustWrite(t, file, strings.Join(kept, "\n"))
	out, err := execCerne(t, "push", "--dir", projDir)
	if err == nil || !strings.Contains(err.Error(), "errata block") {
		t.Fatalf("push must refuse: err=%v\n%s", err, out)
	}
	if stored, _ := store.LoadNoteContent(repoDir, "N-0001"); strings.Contains(stored, "DTS do RTSP") {
		t.Fatalf("errata text reached the backend:\n%s", stored)
	}
}

// A complete example block the user quotes AFTER the generated one is body.
func TestPushKeepsUserExampleBlockAfterGeneratedOne(t *testing.T) {
	projDir, repoDir, file := notePulledWithErrata(t)
	got, _ := os.ReadFile(file)
	example := "```\n<!-- cerne:errata:begin -->\n## Errata\n\nexemplo do usuario\n<!-- cerne:errata:end -->\n```\n"
	mustWrite(t, file, string(got)+"\n"+example)
	rb(t, projDir, "push")
	stored, _ := store.LoadNoteContent(repoDir, "N-0001")
	if !strings.Contains(stored, "exemplo do usuario") || strings.Contains(stored, "DTS do RTSP") {
		t.Fatalf("example kept, generated block gone:\n%s", stored)
	}
}

// A body that carries its own marker block, with NO errata: nothing is stripped
// at pull (no false edit) and nothing is lost at push.
func TestAuthoredMarkerBlockWithoutErrataSurvives(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	authored := "Corpo.\n\n<!-- cerne:errata:begin — meu -->\n## Errata\n\nescrito por mim\n<!-- cerne:errata:end -->\n"
	body := filepath.Join(t.TempDir(), "n.md")
	mustWrite(t, body, authored)
	rb(t, projDir, "note", "new", "Autoral", "--file", body)
	rb(t, projDir, "pull")
	if out := rb(t, projDir, "pull"); strings.Contains(out, "onflict") {
		t.Fatalf("second pull: %s", out)
	}
	if out := rb(t, projDir, "push"); strings.Contains(out, "Sent: 1") {
		t.Fatalf("authored block must not look like an edit:\n%s", out)
	}
	m, _ := filepath.Glob(filepath.Join(projDir, ".docs", "current", "notes", "N-0001-*.md"))
	got, _ := os.ReadFile(m[0])
	mustWrite(t, m[0], strings.Replace(string(got), "Corpo.", "Corpo editado.", 1))
	rb(t, projDir, "push")
	stored, _ := store.LoadNoteContent(repoDir, "N-0001")
	if !strings.Contains(stored, "escrito por mim") || !strings.Contains(stored, "Corpo editado.") {
		t.Fatalf("authored block lost:\n%s", stored)
	}
}

// pull → CRLF edit (autocrlf) → push: body only, no errata.
func TestErrataRoundTripCRLF(t *testing.T) {
	projDir, repoDir, file := notePulledWithErrata(t)
	got, _ := os.ReadFile(file)
	edited := strings.Replace(string(got), "decode por GPU", "decode editado", 1)
	mustWrite(t, file, strings.ReplaceAll(edited, "\n", "\r\n"))
	rb(t, projDir, "push")
	stored, _ := store.LoadNoteContent(repoDir, "N-0001")
	if !strings.Contains(stored, "decode editado") || strings.Contains(stored, "cerne:errata") || strings.Contains(stored, "DTS do RTSP") {
		t.Fatalf("CRLF edit must push the body only:\n%s", stored)
	}
}

// OKF: the projection carries the block, the body that reaches the backend does not.
func TestErrataRoundTripOKF(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	body := filepath.Join(t.TempDir(), "n.md")
	mustWrite(t, body, "Corpo original.\n")
	rb(t, projDir, "note", "new", "Causa", "--file", body)
	rb(t, projDir, "note", "errata", "N-0001", "A causa real era o DTS.")
	rb(t, projDir, "pull", "--okf")
	m, _ := filepath.Glob(filepath.Join(projDir, ".docs", "current", "notes", "N-0001-*.md"))
	got, _ := os.ReadFile(m[0])
	if !strings.Contains(string(got), "A causa real era o DTS.") {
		t.Fatalf("okf projection misses the erratum:\n%s", got)
	}
	if out := rb(t, projDir, "push"); strings.Contains(out, "Sent: 1") {
		t.Fatalf("untouched okf projection must not push:\n%s", out)
	}
	mustWrite(t, m[0], strings.Replace(string(got), "Corpo original.", "Corpo okf editado.", 1))
	rb(t, projDir, "push")
	stored, _ := store.LoadNoteContent(repoDir, "N-0001")
	if !strings.Contains(stored, "Corpo okf editado.") || strings.Contains(stored, "DTS") || strings.Contains(stored, "cerne:errata") {
		t.Fatalf("backend body must hold the edit and no errata:\n%s", stored)
	}
}
