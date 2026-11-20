package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/room"
)

func promptJSON(t *testing.T, sess, cwd, prompt string) string {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"session_id": sess, "cwd": cwd, "hook_event_name": "UserPromptSubmit", "prompt": prompt})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

const promptSymptom = "o webhook do cliente está dando timeout depois do deploy, o que faço?"

// TestPromptNotesInjects (B-0020): a prompt that describes the symptom with the
// words of a note's title gets that note's id and title (no body) as
// UserPromptSubmit additionalContext; an unrelated prompt gets nothing; the
// same prompt twice in one session injects once, another session again.
func TestPromptNotesInjects(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	seedEditNotes(t, repoDir, map[string][2]string{
		"N-0001": {"Webhook do cliente retorna timeout após deploy", "SECRET BODY raise the proxy timeout"},
	})
	out, err := execCerneStdin(t, promptJSON(t, "sess-a", projDir, promptSymptom), "hook", "prompt-notes")
	if err != nil {
		t.Fatal(err)
	}
	var env struct {
		HookSpecificOutput struct {
			HookEventName     string `json:"hookEventName"`
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("not an envelope: %q", out)
	}
	ctx := env.HookSpecificOutput.AdditionalContext
	if env.HookSpecificOutput.HookEventName != "UserPromptSubmit" ||
		!strings.Contains(ctx, "N-0001 — Webhook do cliente retorna timeout após deploy") ||
		!strings.Contains(ctx, "cerne show <id>") || strings.Contains(ctx, "SECRET BODY") {
		t.Fatalf("envelope = %s", out)
	}
	if out, err := execCerneStdin(t, promptJSON(t, "sess-a", projDir, promptSymptom), "hook", "prompt-notes"); err != nil || out != "" {
		t.Fatalf("same session again: %v %q", err, out)
	}
	if out, _ := execCerneStdin(t, promptJSON(t, "sess-b", projDir, promptSymptom), "hook", "prompt-notes"); !strings.Contains(out, "N-0001") {
		t.Fatalf("other session: %q", out)
	}
	if out, err := execCerneStdin(t, promptJSON(t, "sess-c", projDir, "como configuro o editor para formatar ao salvar?"), "hook", "prompt-notes"); err != nil || out != "" {
		t.Fatalf("unrelated prompt: %v %q", err, out)
	}
}

// TestPromptNotesCapsAtThree: at most 3 lines.
func TestPromptNotesCapsAtThree(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	notes := map[string][2]string{}
	for _, id := range []string{"N-0001", "N-0002", "N-0003", "N-0004"} {
		notes[id] = [2]string{"Webhook cliente timeout " + id, "b"}
	}
	seedEditNotes(t, repoDir, notes)
	out, _ := execCerneStdin(t, promptJSON(t, "s", projDir, promptSymptom), "hook", "prompt-notes")
	if n := strings.Count(out, "— Webhook"); n != 3 {
		t.Fatalf("%d lines, want 3: %s", n, out)
	}
}

// TestPromptNotesSilent: setting off, a broken index, no index, a bad payload,
// no session id and a non-cerne directory all exit 0 with no output.
func TestPromptNotesSilent(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	noSSH(t)
	if out, err := execCerneStdin(t, promptJSON(t, "s0", projDir, promptSymptom), "hook", "prompt-notes"); err != nil || out != "" {
		t.Fatalf("no index: %v %q", err, out)
	}
	seedEditNotes(t, repoDir, map[string][2]string{"N-0001": {"Webhook do cliente retorna timeout após deploy", "b"}})
	db := filepath.Join(filepath.Dir(repoDir), "index.db")
	good, _ := os.ReadFile(db)
	if err := os.WriteFile(db, []byte("this is not a sqlite database"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := execCerneStdin(t, promptJSON(t, "s1", projDir, promptSymptom), "hook", "prompt-notes"); err != nil || out != "" {
		t.Fatalf("broken index: %v %q", err, out)
	}
	if err := os.WriteFile(db, good, 0o644); err != nil {
		t.Fatal(err)
	}
	for name, in := range map[string]string{
		"garbage":    "not json",
		"no session": promptJSON(t, "", projDir, promptSymptom),
		"non-cerne":  promptJSON(t, "s2", t.TempDir(), promptSymptom),
	} {
		if out, err := execCerneStdin(t, in, "hook", "prompt-notes"); err != nil || out != "" {
			t.Fatalf("%s: %v %q", name, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(projDir, ".cernerc"), append(mustRead(t, filepath.Join(projDir, ".cernerc")), []byte("\n[hooks]\nprompt_notes = false\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := execCerneStdin(t, promptJSON(t, "s3", projDir, promptSymptom), "hook", "prompt-notes"); err != nil || out != "" {
		t.Fatalf("setting off: %v %q", err, out)
	}
}

// TestPromptNotesDoNotSilenceEditNotes (critic 1): a note injected at prompt
// time is not marked as shown to the on-edit block, so the more specific
// warning still fires when the agent edits a file the note cites.
func TestPromptNotesDoNotSilenceEditNotes(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	startBoundWork(t, projDir, repoDir, "sess-a", "Mexer")
	seedEditNotes(t, repoDir, map[string][2]string{
		"N-0001": {"Webhook do cliente retorna timeout após deploy", "see infra/release.sh"},
	})
	if out, _ := execCerneStdin(t, promptJSON(t, "sess-a", projDir, promptSymptom), "hook", "prompt-notes"); !strings.Contains(out, "N-0001") {
		t.Fatalf("prompt: %q", out)
	}
	if out, _ := execCerneStdin(t, editJSON(t, "sess-a", projDir, "infra/release.sh"), "hook", "work-gate"); !strings.Contains(out, "N-0001") {
		t.Fatalf("edit warning was silenced by the prompt reminder: %q", out)
	}
	// And the other way: what the edit already showed is not repeated on a prompt.
	if out, err := execCerneStdin(t, promptJSON(t, "sess-a", projDir, promptSymptom+" de novo"), "hook", "prompt-notes"); err != nil || out != "" {
		t.Fatalf("prompt after the edit block: %v %q", err, out)
	}
}

// TestPromptNotesCursorPayloadIsSilent (critic 8): Cursor runs the .claude
// hooks but discards their context, so the hook neither prints nor marks ids
// as seen for a Cursor payload.
func TestPromptNotesCursorPayloadIsSilent(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	seedEditNotes(t, repoDir, map[string][2]string{"N-0001": {"Webhook do cliente retorna timeout após deploy", "b"}})
	raw, _ := json.Marshal(map[string]any{"session_id": "sess-cur", "cwd": projDir, "prompt": promptSymptom, "cursor_version": "3.25.6"})
	if out, err := execCerneStdin(t, string(raw), "hook", "prompt-notes"); err != nil || out != "" {
		t.Fatalf("cursor payload: %v %q", err, out)
	}
	if out, _ := execCerneStdin(t, promptJSON(t, "sess-cur", projDir, promptSymptom), "hook", "prompt-notes"); !strings.Contains(out, "N-0001") {
		t.Fatalf("the cursor payload marked the id as seen: %q", out)
	}
}

// TestPromptNotesTitleCap (critic 9): a very long title is cut.
func TestPromptNotesTitleCap(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	long := "Webhook do cliente retorna timeout " + strings.Repeat("palavra ", 60)
	seedEditNotes(t, repoDir, map[string][2]string{"N-0001": {long, "b"}})
	out, _ := execCerneStdin(t, promptJSON(t, "s", projDir, "webhook cliente timeout palavra"), "hook", "prompt-notes")
	if !strings.Contains(out, "N-0001") || len(out) > 600 || !strings.Contains(out, "…") {
		t.Fatalf("title not capped (%d bytes): %s", len(out), out)
	}
}

// TestPromptDeliversChannel (B-0054 crit. 1): the user's prompt is a channel
// contact point — a session that only converses gets its pending messages in
// the UserPromptSubmit envelope, once. Cursor discards this context and the
// Copilot CLI ignores it, so neither consumes anything.
func TestPromptDeliversChannel(t *testing.T) {
	projDir, repoDir, block := channelContactFixture(t)
	chDir := filepath.Join(filepath.Dir(repoDir), "rooms")
	t.Setenv("COPILOT_CLI", "1")
	if out, err := execCerneStdin(t, promptJSON(t, "sess-a", projDir, "oi"), "hook", "prompt-notes"); err != nil || out != "" {
		t.Fatalf("copilot cli: %v %q, want silence", err, out)
	}
	t.Setenv("COPILOT_CLI", "")
	cursor, err := json.Marshal(map[string]any{"session_id": "sess-a", "cwd": projDir, "prompt": "oi", "cursor_version": "3.25.6"})
	if err != nil {
		t.Fatal(err)
	}
	if out, err := execCerneStdin(t, string(cursor), "hook", "prompt-notes"); err != nil || out != "" {
		t.Fatalf("cursor: %v %q, want silence", err, out)
	}
	if dels, _ := room.OpenChannel(chDir).Deliveries(); len(dels) != 0 {
		t.Fatalf("a host that drops the context consumed the channel: %+v", dels)
	}
	out, err := execCerneStdin(t, promptJSON(t, "sess-a", projDir, "oi"), "hook", "prompt-notes")
	if err != nil {
		t.Fatal(err)
	}
	if c := channelEnvelope(t, out); c.HookSpecificOutput.HookEventName != "UserPromptSubmit" || c.HookSpecificOutput.AdditionalContext != block {
		t.Fatalf("envelope = %+v, want the channel block %q", c, block)
	}
	if out, err := execCerneStdin(t, promptJSON(t, "sess-a", projDir, "oi"), "hook", "prompt-notes"); err != nil || out != "" {
		t.Fatalf("second prompt: %v %q, want zero bytes", err, out)
	}
}
