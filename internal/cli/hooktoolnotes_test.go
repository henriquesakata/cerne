package cli

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/project"
)

// toolNotesProject is a project whose notes declare bash: triggers, created
// through the CLI so the index is the one a real note new leaves behind.
func toolNotesProject(t *testing.T, notes ...[]string) (projDir, repoDir string) {
	t.Helper()
	projDir, _, repoDir = setupProject(t)
	for _, n := range notes {
		args := []string{"note", "new", n[0]}
		for _, w := range n[1:] {
			args = append(args, "--when", w)
		}
		if out, err := execCerne(t, append(args, "--dir", projDir)...); err != nil {
			t.Fatalf("note new %v: %v\n%s", n, err, out)
		}
	}
	return projDir, repoDir
}

func bashJSON(t *testing.T, sess, projDir, command string) string {
	return hookJSON(t, sess, projDir, "Bash", map[string]any{"command": command}, nil)
}

// TestToolNotesEmits pins who receives context in which mode (F-0457
// criteria 6, 10, 11): before the tool only Claude Code and Codex, after it
// only Cursor, Gemini and Copilot; Copilot CLI running the .claude hooks gets
// nothing.
func TestToolNotesEmits(t *testing.T) {
	cases := []struct {
		after          bool
		format         string
		cursor, copilt bool
		want           bool
	}{
		{false, "claude", false, false, true},
		{false, "codex", false, false, true},
		{false, "claude", true, false, false},
		{false, "claude", false, true, false},
		{false, "gemini", false, false, false},
		{false, "copilot", false, false, false},
		{true, "claude", false, false, false},
		{true, "codex", false, false, false},
		{true, "claude", true, false, true},
		{true, "gemini", false, false, true},
		{true, "copilot", false, false, true},
		{true, "copilot", false, true, true},
		{false, "nope", false, false, false},
	}
	for _, c := range cases {
		if got := toolNotesEmits(c.after, c.format, c.cursor, c.copilt); got != c.want {
			t.Errorf("toolNotesEmits(after=%v, %q, cursor=%v, copilotCLI=%v) = %v", c.after, c.format, c.cursor, c.copilt, got)
		}
	}
}

// TestHookToolNotesBefore (F-0457 criteria 6, 9): a matching command gets the
// block in the PreToolUse envelope, longest trigger first, capped at 3, once
// per session; never a deny; no network.
func TestHookToolNotesBefore(t *testing.T) {
	projDir, _ := toolNotesProject(t,
		[]string{"Any git", "bash:git"},
		[]string{"Tag on the merge commit", "bash:git tag"},
		[]string{"Signed tags", "bash:git tag"},
		[]string{"Tags trigger release", "bash:git tag"},
		[]string{"Push rules", "bash:git push"},
	)
	noSSH(t)
	in := bashJSON(t, "sess-a", projDir, "cd x && git tag -a v1")
	for _, format := range []string{"claude", "codex"} {
		sess := "sess-" + format
		out, err := execCerneStdin(t, strings.Replace(in, "sess-a", sess, 1), "hook", "tool-notes", "--format", format)
		if err != nil {
			t.Fatal(err)
		}
		c := channelEnvelope(t, out)
		if c.HookSpecificOutput.HookEventName != "PreToolUse" {
			t.Fatalf("%s: event %q", format, c.HookSpecificOutput.HookEventName)
		}
		ctx := c.HookSpecificOutput.AdditionalContext
		if !strings.HasPrefix(ctx, "cerne notes on `git tag`:\n- N-0004 — Tags trigger release\n- N-0003 — Signed tags\n- N-0002 — Tag on the merge commit") {
			t.Fatalf("%s block:\n%s", format, ctx)
		}
		if strings.Contains(ctx, "N-0001") || strings.Contains(ctx, "N-0005") {
			t.Fatalf("%s: beyond the cap of 3 or unmatched:\n%s", format, ctx)
		}
		if strings.Contains(out, "permissionDecision") {
			t.Fatalf("%s: the hook must never decide:\n%s", format, out)
		}
		// Same session: the shown notes do not come back; the fourth does.
		out, _ = execCerneStdin(t, strings.Replace(in, "sess-a", sess, 1), "hook", "tool-notes", "--format", format)
		if ctx := channelEnvelope(t, out).HookSpecificOutput.AdditionalContext; ctx != "cerne notes on `git`:\n- N-0001 — Any git" {
			t.Fatalf("%s second call:\n%s", format, ctx)
		}
	}
}

// TestHookToolNotesSilent (F-0457 criteria 7, 9, 10): no output and nothing
// marked as seen when nothing matches, the setting is off, the payload is
// Cursor's or the Copilot CLI's, the tool is not a shell, or the input is junk.
func TestHookToolNotesSilent(t *testing.T) {
	projDir, repoDir := toolNotesProject(t, []string{"Tag on the merge commit", "bash:git tag"})
	noSSH(t)
	cacheDir := filepath.Dir(repoDir)
	quiet := func(name, stdin string, args ...string) {
		t.Helper()
		out, err := execCerneStdin(t, stdin, append([]string{"hook", "tool-notes"}, args...)...)
		if err != nil || out != "" {
			t.Errorf("%s: err %v, out %q", name, err, out)
		}
	}
	quiet("no match", bashJSON(t, "s1", projDir, "git tags"))
	quiet("edit before", editJSON(t, "s1", projDir, "README.md"))
	quiet("junk", "{not json")
	quiet("no project", bashJSON(t, "s1", t.TempDir(), "git tag"))

	cur := cursorFixture(t, "post-tool-use-terminals-cwd.json", projDir)
	cur["hook_event_name"] = "preToolUse"
	cur["tool_input"] = map[string]any{"command": "git tag v1"}
	quiet("cursor payload before", payloadString(t, cur))

	t.Setenv("COPILOT_CLI", "1")
	quiet("copilot running .claude", bashJSON(t, "s2", projDir, "git tag v1"))
	t.Setenv("COPILOT_CLI", "")

	if err := os.WriteFile(filepath.Join(projDir, ".cernerc"), append(mustRead(t, filepath.Join(projDir, ".cernerc")), []byte("\n[hooks]\nnotes_on_bash = false\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	if project.NotesOnBash(projDir) {
		t.Fatal("setting not applied")
	}
	quiet("setting off", bashJSON(t, "s3", projDir, "git tag v1"))

	for _, sess := range []string{"s1", "s2", "s3", cursorSess} {
		if len(app.SeenNotes(cacheDir, sess)) != 0 {
			t.Errorf("session %s marked notes as seen while silent", sess)
		}
	}
}

// afterProject has one note per trigger kind, for the post-tool harnesses.
func afterProject(t *testing.T) (projDir, repoDir string) {
	t.Helper()
	projDir, repoDir = toolNotesProject(t,
		[]string{"Tag on the merge commit", "bash:git tag"},
		[]string{"Commit the dist with the source", "edit:ui/dist/**"},
	)
	noSSH(t)
	return projDir, repoDir
}

const (
	afterCmdHeader  = "cerne notes on `git tag` (the command already ran):\n- N-0001 — Tag on the merge commit"
	afterEditHeader = "cerne notes on ui/dist/index.html (after this edit):\n- N-0002 — Commit the dist with the source"
)

// TestHookToolNotesAfterCursor (F-0457 criterion 11): Cursor runs the .claude
// PostToolUse entry through its third-party import; a Shell or Write call
// gets the block in Cursor's native additional_context.
func TestHookToolNotesAfterCursor(t *testing.T) {
	projDir, _ := afterProject(t)
	shell := cursorFixture(t, "post-tool-use-terminals-cwd.json", projDir)
	shell["tool_input"] = map[string]any{"command": "git tag v1", "cwd": projDir}
	out, err := execCerneStdin(t, payloadString(t, shell), "hook", "tool-notes", "--after")
	if err != nil {
		t.Fatal(err)
	}
	if m := decodeTop(t, out); m["additional_context"] != afterCmdHeader {
		t.Fatalf("cursor shell:\n%s", out)
	}
	write := cursorFixture(t, "pre-tool-use-write.json", projDir)
	write["hook_event_name"] = "postToolUse"
	write["tool_input"] = map[string]any{"file_path": filepath.Join(projDir, "ui", "dist", "index.html"), "content": "x"}
	out, err = execCerneStdin(t, payloadString(t, write), "hook", "tool-notes", "--after")
	if err != nil {
		t.Fatal(err)
	}
	if m := decodeTop(t, out); m["additional_context"] != afterEditHeader {
		t.Fatalf("cursor write:\n%s", out)
	}
}

// TestHookToolNotesAfterGemini (F-0457 criterion 11): AfterTool appends
// hookSpecificOutput.additionalContext to the tool result (R-0036).
func TestHookToolNotesAfterGemini(t *testing.T) {
	projDir, _ := afterProject(t)
	for _, tc := range []struct {
		tool  string
		input map[string]any
		want  string
	}{
		{"run_shell_command", map[string]any{"command": "git tag v1", "description": "tag"}, afterCmdHeader},
		{"write_file", map[string]any{"file_path": filepath.Join(projDir, "ui", "dist", "index.html"), "content": "x"}, afterEditHeader},
	} {
		in := hookJSON(t, "sess-gem", projDir, tc.tool, tc.input, map[string]any{"llmContent": "ok"})
		out, err := execCerneStdin(t, in, "hook", "tool-notes", "--after", "--format", "gemini")
		if err != nil {
			t.Fatal(err)
		}
		c := channelEnvelope(t, out)
		if c.HookSpecificOutput.HookEventName != "AfterTool" || c.HookSpecificOutput.AdditionalContext != tc.want {
			t.Fatalf("gemini %s:\n%s", tc.tool, out)
		}
	}
}

// TestHookToolNotesAfterCopilot (F-0457 criterion 11): Copilot's PostToolUse
// takes a top-level additionalContext; its edits carry the apply_patch text.
func TestHookToolNotesAfterCopilot(t *testing.T) {
	projDir, _ := afterProject(t)
	res := `,"tool_result":{"result_type":"success","text_result_for_llm":"ok"}`
	bash := copilotPayload("sess-cop", projDir, "PostToolUse", "Bash", `{"command":"git tag v1","description":"x"}`, res)
	out, err := execCerneStdin(t, bash, "hook", "tool-notes", "--after", "--format", "copilot")
	if err != nil {
		t.Fatal(err)
	}
	if m := decodeTop(t, out); m["additionalContext"] != afterCmdHeader {
		t.Fatalf("copilot bash:\n%s", out)
	}
	patch := jsonString("*** Begin Patch\n*** Update File: ui/dist/index.html\n@@\n-a\n+b\n*** End Patch\n")
	out, err = execCerneStdin(t, copilotPayload("sess-cop", projDir, "PostToolUse", "Edit", patch, res), "hook", "tool-notes", "--after", "--format", "copilot")
	if err != nil {
		t.Fatal(err)
	}
	if m := decodeTop(t, out); m["additionalContext"] != afterEditHeader {
		t.Fatalf("copilot edit:\n%s", out)
	}
}

// TestHookToolNotesAfterSilent (F-0457 criteria 10, 11): the after mode says
// nothing to Claude Code and Codex (their before mode already did), and an
// edit honors hooks.notes_on_edit.
func TestHookToolNotesAfterSilent(t *testing.T) {
	projDir, repoDir := afterProject(t)
	for _, format := range []string{"claude", "codex"} {
		in := hookJSON(t, "sess-q", projDir, "Bash", map[string]any{"command": "git tag v1"}, map[string]any{"stdout": ""})
		if out, err := execCerneStdin(t, in, "hook", "tool-notes", "--after", "--format", format); err != nil || out != "" {
			t.Errorf("%s after: err %v, out %q", format, err, out)
		}
	}
	if len(app.SeenNotes(filepath.Dir(repoDir), "sess-q")) != 0 {
		t.Error("a silent after call marked notes as seen")
	}
	if err := os.WriteFile(filepath.Join(projDir, ".cernerc"), append(mustRead(t, filepath.Join(projDir, ".cernerc")), []byte("\n[hooks]\nnotes_on_edit = false\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	in := hookJSON(t, "sess-off", projDir, "write_file", map[string]any{"file_path": filepath.Join(projDir, "ui", "dist", "index.html")}, map[string]any{"llmContent": "ok"})
	if out, err := execCerneStdin(t, in, "hook", "tool-notes", "--after", "--format", "gemini"); err != nil || out != "" {
		t.Errorf("notes_on_edit off: err %v, out %q", err, out)
	}
}

// TestNotesOnEditSilentUnderCopilotCLI (F-0457 criterion 10, seen live): the
// Copilot CLI also runs the .claude work gate; there its notes block must not
// mark the note as seen, or Copilot's own post-edit hook — the channel it
// documents — stays silent for it.
func TestNotesOnEditSilentUnderCopilotCLI(t *testing.T) {
	projDir, repoDir := afterProject(t)
	t.Setenv("COPILOT_CLI", "1")
	in := editJSON(t, "sess-cop2", projDir, "ui/dist/index.html")
	if out, err := execCerneStdin(t, in, "hook", "work-gate"); err != nil || strings.Contains(out, "cerne notes") {
		t.Fatalf("work gate under Copilot CLI: err %v, out %q", err, out)
	}
	if len(app.SeenNotes(filepath.Dir(repoDir), "sess-cop2")) != 0 {
		t.Fatal("the .claude work gate marked notes as seen under Copilot CLI")
	}
	patch := jsonString("*** Begin Patch\n*** Update File: ui/dist/index.html\n@@\n-a\n+b\n*** End Patch\n")
	res := `,"tool_result":{"result_type":"success","text_result_for_llm":"ok"}`
	out, err := execCerneStdin(t, copilotPayload("sess-cop2", projDir, "PostToolUse", "Edit", patch, res), "hook", "tool-notes", "--after", "--format", "copilot")
	if err != nil {
		t.Fatal(err)
	}
	if m := decodeTop(t, out); m["additionalContext"] != afterEditHeader {
		t.Fatalf("copilot after edit:\n%s", out)
	}
}

// codexPatchJSON is what Codex 0.169 sends for an apply_patch edit (seen live
// in F-0457): tool_name apply_patch, the patch text under tool_input.command,
// paths relative to cwd.
func codexPatchJSON(t *testing.T, sess, projDir, rel string) string {
	return hookJSON(t, sess, projDir, "apply_patch", map[string]any{"command": "*** Begin Patch\n*** Update File: " + rel + "\n@@\n+third line\n*** End Patch"}, nil)
}

// TestCodexApplyPatchEdit (F-0457 criterion 12, seen live): the work gate
// reads the files of a Codex apply_patch — it shows their notes and, with the
// gate on, denies an unbound session — and tool-notes never reads the patch
// text as a shell command.
func TestCodexApplyPatchEdit(t *testing.T) {
	projDir, _ := toolNotesProject(t,
		[]string{"Tag on the merge commit", "bash:git tag"},
		[]string{"Commit the dist with the source", "edit:ui/dist/**"},
	)
	noSSH(t)
	in := codexPatchJSON(t, "sess-cx", projDir, "ui/dist/index.html")
	out, err := execCerneStdin(t, in, "hook", "work-gate")
	if err != nil {
		t.Fatal(err)
	}
	if ctx := channelEnvelope(t, out).HookSpecificOutput.AdditionalContext; ctx != "cerne notes on ui/dist/index.html:\n- N-0002 — Commit the dist with the source" {
		t.Fatalf("codex apply_patch notes:\n%s", out)
	}
	patchWithTag := hookJSON(t, "sess-cx2", projDir, "apply_patch", map[string]any{"command": "*** Begin Patch\n*** Add File: notes.txt\n+git tag v1\n*** End Patch"}, nil)
	if out, err := execCerneStdin(t, patchWithTag, "hook", "tool-notes", "--format", "codex"); err != nil || out != "" {
		t.Fatalf("a patch is not a shell command: err %v, out %q", err, out)
	}
	enableWorkGate(t, projDir)
	out, err = execCerneStdin(t, codexPatchJSON(t, "sess-unbound", projDir, "internal/a.go"), "hook", "work-gate")
	if err != nil || !strings.Contains(out, `"permissionDecision":"deny"`) {
		t.Fatalf("codex apply_patch on an unbound session not denied: %v\n%s", err, out)
	}
}

// TestHookToolNotesEditWithCommandKey (critic finding 3): an edit tool whose
// input carries both a command verb and a path (text-editor style) is an edit,
// never a shell command.
func TestHookToolNotesEditWithCommandKey(t *testing.T) {
	projDir, _ := afterProject(t)
	in := hookJSON(t, "sess-ed", projDir, "replace", map[string]any{"command": "git tag", "path": filepath.Join(projDir, "ui", "dist", "index.html")}, map[string]any{"llmContent": "ok"})
	out, err := execCerneStdin(t, in, "hook", "tool-notes", "--after", "--format", "gemini")
	if err != nil {
		t.Fatal(err)
	}
	if ctx := channelEnvelope(t, out).HookSpecificOutput.AdditionalContext; ctx != afterEditHeader {
		t.Fatalf("edit with a command key:\n%s", out)
	}
}

// TestWorkGateNotesSilentOnPostToolHarnesses (critic finding 2, criterion
// 10): the work gate in Copilot's and Gemini's own formats never shows nor
// marks edit notes — those harnesses get them after the edit.
func TestWorkGateNotesSilentOnPostToolHarnesses(t *testing.T) {
	projDir, repoDir := afterProject(t)
	cacheDir := filepath.Dir(repoDir)
	patch := jsonString("*** Begin Patch\n*** Update File: ui/dist/index.html\n@@\n-a\n+b\n*** End Patch\n")
	if out, err := execCerneStdin(t, copilotPayload("sess-wc", projDir, "PreToolUse", "Edit", patch, ""), "hook", "work-gate", "--format", "copilot"); err != nil || strings.Contains(out, "cerne notes") {
		t.Fatalf("copilot work gate: err %v, out %q", err, out)
	}
	in := hookJSON(t, "sess-wg", projDir, "write_file", map[string]any{"file_path": filepath.Join(projDir, "ui", "dist", "index.html")}, nil)
	if out, err := execCerneStdin(t, in, "hook", "work-gate", "--format", "gemini"); err != nil || strings.Contains(out, "cerne notes") {
		t.Fatalf("gemini work gate: err %v, out %q", err, out)
	}
	for _, sess := range []string{"sess-wc", "sess-wg"} {
		if len(app.SeenNotes(cacheDir, sess)) != 0 {
			t.Errorf("%s: the work gate marked notes as seen", sess)
		}
	}
	// Positive control (critic finding 8): a delivering call does mark, in
	// this very cache dir.
	if out, _ := execCerneStdin(t, editJSON(t, "sess-wclaude", projDir, "ui/dist/index.html"), "hook", "work-gate"); !strings.Contains(out, "N-0002") {
		t.Fatalf("claude work gate did not deliver: %q", out)
	}
	if !app.SeenNotes(cacheDir, "sess-wclaude")["N-0002"] {
		t.Fatal("positive control: the delivering call did not mark N-0002 in the cache dir the silent checks read")
	}
}

// TestNoteWhenAfterOldIndex (critic finding 4): a project whose index was
// built before declared triggers gets the trigger to the hook right after
// note when (Update fails, the write path rebuilds), and --clear removes it.
func TestNoteWhenAfterOldIndex(t *testing.T) {
	projDir, repoDir := toolNotesProject(t, []string{"Tag on the merge commit"})
	db, err := sql.Open("sqlite", filepath.Join(filepath.Dir(repoDir), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DROP TABLE note_when`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if out, err := execCerne(t, "note", "when", "N-0001", "bash:git tag", "--dir", projDir); err != nil {
		t.Fatalf("note when: %v\n%s", err, out)
	}
	out, err := execCerneStdin(t, bashJSON(t, "sess-old", projDir, "git tag v1"), "hook", "tool-notes")
	if err != nil || !strings.Contains(out, "N-0001 — Tag on the merge commit") {
		t.Fatalf("after note when on an old index: err %v, out %q", err, out)
	}
	if out, err := execCerne(t, "note", "when", "N-0001", "--clear", "--dir", projDir); err != nil {
		t.Fatalf("note when --clear: %v\n%s", err, out)
	}
	if out, err := execCerneStdin(t, bashJSON(t, "sess-old2", projDir, "git tag v1"), "hook", "tool-notes"); err != nil || out != "" {
		t.Fatalf("after --clear: err %v, out %q", err, out)
	}
}
