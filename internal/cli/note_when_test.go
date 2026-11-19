package cli

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// TestNoteNewWhen (F-0457 criterion 1): --when is repeatable and persists the
// canonical triggers on the entity.
func TestNoteNewWhen(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	out, err := execCerne(t, "note", "new", "Tag goes on the merge commit", "--when", "bash:git   tag",
		"--when", "edit:./ui/dist/**", "--dir", projDir)
	if err != nil {
		t.Fatalf("note new --when: %v\n%s", err, out)
	}
	n, err := store.LoadNote(repoDir, "N-0001")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"bash:git tag", "edit:ui/dist/**"}; !slices.Equal(n.When, want) {
		t.Fatalf("when = %q, want %q", n.When, want)
	}
}

// TestNoteNewWhenRefused (F-0457 criterion 1): an invalid trigger fails with
// the accepted forms and writes nothing — no note, no id minted.
func TestNoteNewWhenRefused(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	out, err := execCerne(t, "note", "new", "T", "--when", "read:x", "--dir", projDir)
	if !errors.Is(err, domain.ErrTriggerKind) {
		t.Fatalf("err = %v, want ErrTriggerKind\n%s", err, out)
	}
	if !strings.Contains(err.Error(), "edit:<path>") || !strings.Contains(err.Error(), "bash:<words>") {
		t.Fatalf("refusal does not list the accepted forms: %v", err)
	}
	if notes, _ := store.LoadAllNotes(repoDir); len(notes) != 0 {
		t.Fatalf("refused note new wrote %d note(s)", len(notes))
	}
	if _, err := os.Stat(filepath.Join(repoDir, "content", "notes", "N-0001.md")); err == nil {
		t.Fatal("refused note new wrote a body")
	}
}

// TestNoteWhen (F-0457 criterion 2): replaces the triggers, --clear removes
// them, bad input is refused, and a superseded note is history.
func TestNoteWhen(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	if _, err := execCerne(t, "note", "new", "T", "--when", "bash:git tag", "--dir", projDir); err != nil {
		t.Fatalf("note new: %v", err)
	}
	if out, err := execCerne(t, "note", "when", "N-0001", "edit:docs.lock", "bash:cerne import", "--dir", projDir); err != nil {
		t.Fatalf("note when: %v\n%s", err, out)
	}
	n, _ := store.LoadNote(repoDir, "N-0001")
	if want := []string{"edit:docs.lock", "bash:cerne import"}; !slices.Equal(n.When, want) {
		t.Fatalf("when = %q, want %q", n.When, want)
	}
	evs, _ := store.ReadEvents(repoDir)
	if last := evs[len(evs)-1]; last.Type != "note.when" || last.Payload["when"] != "edit:docs.lock,bash:cerne import" {
		t.Fatalf("last event = %+v, want note.when", last)
	}

	if _, err := execCerne(t, "note", "when", "N-0001", "edit:/abs", "--dir", projDir); !errors.Is(err, domain.ErrTriggerPath) {
		t.Fatalf("absolute path: err = %v, want ErrTriggerPath", err)
	}
	if _, err := execCerne(t, "note", "when", "N-0001", "--dir", projDir); err == nil {
		t.Fatal("note when with no trigger and no --clear must fail")
	}
	if _, err := execCerne(t, "note", "when", "N-0001", "bash:x", "--clear", "--dir", projDir); err == nil {
		t.Fatal("note when with triggers and --clear must fail")
	}
	n, _ = store.LoadNote(repoDir, "N-0001")
	if len(n.When) != 2 {
		t.Fatalf("a refused note when mutated the note: %q", n.When)
	}

	if out, err := execCerne(t, "note", "when", "N-0001", "--clear", "--dir", projDir); err != nil {
		t.Fatalf("note when --clear: %v\n%s", err, out)
	}
	n, _ = store.LoadNote(repoDir, "N-0001")
	if n.When != nil {
		t.Fatalf("--clear left %q", n.When)
	}

	if _, err := execCerne(t, "note", "retire", "N-0001", "--reason", "done", "--dir", projDir); err != nil {
		t.Fatalf("note retire: %v", err)
	}
	if _, err := execCerne(t, "note", "when", "N-0001", "bash:git tag", "--dir", projDir); err == nil {
		t.Fatal("note when on a superseded note must fail")
	}
}

// TestShowNoteWhen (F-0457 criterion 3): the when line in show and note show,
// and the when field in show --json.
func TestShowNoteWhen(t *testing.T) {
	projDir, _, _ := setupProject(t)
	if _, err := execCerne(t, "note", "new", "T", "--when", "bash:git tag", "--when", "edit:docs.lock", "--dir", projDir); err != nil {
		t.Fatalf("note new: %v", err)
	}
	for _, args := range [][]string{{"show", "N-0001"}, {"note", "show", "N-0001"}} {
		out, err := execCerne(t, append(args, "--dir", projDir)...)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if !strings.Contains(out, "\nwhen: bash:git tag, edit:docs.lock\n") {
			t.Fatalf("%v output lacks the when line:\n%s", args, out)
		}
	}
	out, err := execCerne(t, "show", "N-0001", "--json", "--dir", projDir)
	if err != nil {
		t.Fatalf("show --json: %v", err)
	}
	if !strings.Contains(out, `"when"`) {
		t.Fatalf("show --json lacks when:\n%s", out)
	}
	var probe map[string]any
	if json.Unmarshal([]byte(out), &probe) != nil {
		t.Fatalf("show --json is not JSON:\n%s", out)
	}
}

// TestNotesOnEditDeclared (F-0457 criterion 4): a note created with
// --when edit:<dir>/** reaches the work-gate block of a file under dir with no
// citation of it, above a note that cites the file, once per session. The
// index comes from the note creation itself (refreshIndex), as in real use.
func TestNotesOnEditDeclared(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	noSSH(t)
	startBoundWork(t, projDir, repoDir, "sess-a", "Mexer na UI")
	if out, err := execCerne(t, "note", "new", "Cites the file", "--file", writeTemp(t, "edit ui/dist/index.html with care"), "--dir", projDir); err != nil {
		t.Fatalf("note new: %v\n%s", err, out)
	}
	if out, err := execCerne(t, "note", "new", "Commit the dist with the source", "--file", writeTemp(t, "the bundle is not reproducible"),
		"--when", "edit:ui/dist/**", "--dir", projDir); err != nil {
		t.Fatalf("note new --when: %v\n%s", err, out)
	}
	in := editJSON(t, "sess-a", projDir, "ui/dist/index.html")
	out, err := execCerneStdin(t, in, "hook", "work-gate")
	if err != nil {
		t.Fatal(err)
	}
	ctx := channelEnvelope(t, out).HookSpecificOutput.AdditionalContext
	if want := "cerne notes on ui/dist/index.html:\n- N-0002 — Commit the dist with the source\n- N-0001 — Cites the file"; !strings.Contains(ctx, want) {
		t.Fatalf("block:\n%s\nwant:\n%s", ctx, want)
	}
	if out, err := execCerneStdin(t, in, "hook", "work-gate"); err != nil || out != "" {
		t.Fatalf("second edit, same session: %v %q", err, out)
	}
}

// TestNoteListPathDeclared (F-0457 criterion 5): note list --path includes the
// declared notes, in the hook's order, with no cap.
func TestNoteListPathDeclared(t *testing.T) {
	projDir, _, _ := setupProject(t)
	for _, args := range [][]string{
		{"Cites", "--file", writeTemp(t, "edit ui/dist/index.html")},
		{"Dir glob", "--when", "edit:ui/dist/**"},
		{"Exact", "--when", "edit:ui/dist/index.html"},
		{"Other dir", "--when", "edit:ui/src/**"},
	} {
		if out, err := execCerne(t, append(append([]string{"note", "new"}, args...), "--dir", projDir)...); err != nil {
			t.Fatalf("note new %v: %v\n%s", args, err, out)
		}
	}
	out, err := execCerne(t, "note", "list", "--path", "ui/dist/index.html", "--dir", projDir)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		ids = append(ids, strings.Fields(l)[0])
	}
	if want := []string{"N-0003", "N-0002", "N-0001"}; !slices.Equal(ids, want) {
		t.Fatalf("ids = %v, want %v\n%s", ids, want, out)
	}
}

func writeTemp(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "body.md")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}
