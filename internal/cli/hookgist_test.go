package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/store"
)

// gistProject is a project whose notes are created through the CLI, each as
// {title, gist, trigger...}; an empty gist means none.
func gistProject(t *testing.T, notes ...[]string) (projDir, repoDir string) {
	t.Helper()
	projDir, _, repoDir = setupProject(t)
	for _, n := range notes {
		args := []string{"note", "new", n[0]}
		if n[1] != "" {
			args = append(args, "--gist", n[1])
		}
		for _, w := range n[2:] {
			args = append(args, "--when", w)
		}
		if out, err := execCerne(t, append(args, "--dir", projDir)...); err != nil {
			t.Fatalf("note new %v: %v\n%s", n, err, out)
		}
	}
	noSSH(t)
	return projDir, repoDir
}

// setGistByHand writes a gist straight into the entity, as a hand edit of the
// clone would, bypassing NormalizeGist.
func setGistByHand(t *testing.T, repoDir, id, gist string) {
	t.Helper()
	n, err := store.LoadNote(repoDir, id)
	if err != nil {
		t.Fatal(err)
	}
	n.Gist = gist
	if err := store.SaveNote(repoDir, n); err != nil {
		t.Fatal(err)
	}
}

// TestCommandNotesGist (F-0459 criteria 5, 6): before the tool, a note with a
// gist gets an indented line under its own; one without keeps today's line.
func TestCommandNotesGist(t *testing.T) {
	projDir, _ := gistProject(t,
		[]string{"Tag on the merge commit", "Tag the merge commit, never the pin.", "bash:git tag"},
		[]string{"Signed tags", "", "bash:git tag"},
	)
	for _, format := range []string{"claude", "codex"} {
		out, err := execCerneStdin(t, bashJSON(t, "sess-"+format, projDir, "git tag -a v1"), "hook", "tool-notes", "--format", format)
		if err != nil {
			t.Fatal(err)
		}
		want := "cerne notes on `git tag`:\n- N-0002 — Signed tags\n- N-0001 — Tag on the merge commit\n  Tag the merge commit, never the pin."
		if ctx := channelEnvelope(t, out).HookSpecificOutput.AdditionalContext; ctx != want {
			t.Fatalf("%s block:\n%q\nwant\n%q", format, ctx, want)
		}
	}
}

// TestToolNotesAfterGist (F-0459 criteria 4, 5): after the tool, the command
// and the edit blocks carry the gist under the note, headers unchanged.
func TestToolNotesAfterGist(t *testing.T) {
	projDir, _ := gistProject(t,
		[]string{"Tag on the merge commit", "Tag the merge commit.", "bash:git tag"},
		[]string{"Commit the dist with the source", "Rebuild in node:24 and commit the dist with the source.", "edit:ui/dist/**"},
	)
	for _, tc := range []struct {
		tool  string
		input map[string]any
		want  string
	}{
		{"run_shell_command", map[string]any{"command": "git tag v1"},
			"cerne notes on `git tag` (the command already ran):\n- N-0001 — Tag on the merge commit\n  Tag the merge commit."},
		{"write_file", map[string]any{"file_path": filepath.Join(projDir, "ui", "dist", "index.html"), "content": "x"},
			"cerne notes on ui/dist/index.html (after this edit):\n- N-0002 — Commit the dist with the source\n  Rebuild in node:24 and commit the dist with the source."},
	} {
		in := hookJSON(t, "sess-gem", projDir, tc.tool, tc.input, map[string]any{"llmContent": "ok"})
		out, err := execCerneStdin(t, in, "hook", "tool-notes", "--after", "--format", "gemini")
		if err != nil {
			t.Fatal(err)
		}
		if ctx := channelEnvelope(t, out).HookSpecificOutput.AdditionalContext; ctx != tc.want {
			t.Fatalf("gemini %s:\n%q\nwant\n%q", tc.tool, ctx, tc.want)
		}
	}
}

// TestNotesOnEditGist (F-0459 criteria 4, 6): before the edit, gists sit under
// their notes, a note without one keeps its line, the (+N) hint stays last,
// and a hand-edited gist comes out flat and capped.
func TestNotesOnEditGist(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	noSSH(t)
	seedEditNotes(t, repoDir, map[string][2]string{
		"N-0001": {"full", "edit infra/k8s/scripts/release.sh twice"},
		"N-0002": {"sfx", "see scripts/release.sh"},
		"N-0003": {"sfx2", "scripts/release.sh again"},
		"N-0004": {"sfx3", "scripts/release.sh yet again"},
		"N-0005": {"sfx4", "scripts/release.sh and again"},
	})
	setGistByHand(t, repoDir, "N-0001", "Line one\r\n\tline two ")
	setGistByHand(t, repoDir, "N-0002", strings.Repeat("é", 200))
	out, err := execCerneStdin(t, editJSON(t, "sess-a", projDir, "infra/k8s/scripts/release.sh"), "hook", "work-gate")
	if err != nil {
		t.Fatal(err)
	}
	want := "cerne notes on infra/k8s/scripts/release.sh:\n" +
		"- N-0001 — full\n  Line one line two\n" +
		"- N-0002 — sfx\n  " + strings.Repeat("é", 159) + "…\n" +
		"- N-0003 — sfx2\n" +
		"(+2: cerne note list --path infra/k8s/scripts/release.sh)"
	if ctx := channelEnvelope(t, out).HookSpecificOutput.AdditionalContext; ctx != want {
		t.Fatalf("block:\n%q\nwant\n%q", ctx, want)
	}
}

// TestNoteGistUnreadableEntity (F-0459 criterion 8): an entity that cannot be
// read leaves its note without a gist; the block still comes out.
func TestNoteGistUnreadableEntity(t *testing.T) {
	projDir, repoDir := gistProject(t, []string{"T", "Never shown.", "bash:git tag"})
	if err := os.WriteFile(filepath.Join(repoDir, "entities", "notes", "N-0001.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := execCerneStdin(t, bashJSON(t, "sess-x", projDir, "git tag v1"), "hook", "tool-notes")
	if err != nil {
		t.Fatal(err)
	}
	if ctx := channelEnvelope(t, out).HookSpecificOutput.AdditionalContext; ctx != "cerne notes on `git tag`:\n- N-0001 — T" {
		t.Fatalf("block:\n%q", ctx)
	}
}

// TestPromptNotesNeverCarryGist (F-0459 criterion 7): the prompt block stays
// `id — title` even when the matched note has a gist.
func TestPromptNotesNeverCarryGist(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	seedEditNotes(t, repoDir, map[string][2]string{
		"N-0001": {"Webhook do cliente retorna timeout após deploy", "corpo"},
	})
	setGistByHand(t, repoDir, "N-0001", "GIST raise the proxy timeout")
	out, err := execCerneStdin(t, promptJSON(t, "sess-a", projDir, promptSymptom), "hook", "prompt-notes")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "N-0001 — Webhook do cliente retorna timeout após deploy") || strings.Contains(out, "GIST") {
		t.Fatalf("prompt block:\n%s", out)
	}
}
