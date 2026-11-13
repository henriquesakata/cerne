package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/index"
	"github.com/henriquesakata/cerne/internal/store"
)

// seedEditNote writes an active note citing body and (re)builds the project's
// local index, the way a pull would.
func seedEditNotes(t *testing.T, repoDir string, notes map[string][2]string) {
	t.Helper()
	now := time.Now().UTC()
	dir := filepath.Join(repoDir, "content", "notes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for id, tb := range notes {
		if err := store.SaveNote(repoDir, store.Note{ID: id, Title: tb[0], State: domain.NoteActive, CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, id+".md"), []byte(tb[1]), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := index.Rebuild(repoDir, filepath.Join(filepath.Dir(repoDir), "index.db"), time.Time{}); err != nil {
		t.Fatal(err)
	}
}

// noSSH puts an ssh on PATH that fails the test's expectation if the hook
// ever reaches for the network.
func noSSH(t *testing.T) {
	t.Helper()
	bin := t.TempDir()
	marker := filepath.Join(bin, "called")
	if err := os.WriteFile(filepath.Join(bin, "ssh"), []byte("#!/bin/sh\ntouch "+marker+"\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Cleanup(func() {
		if _, err := os.Stat(marker); err == nil {
			t.Error("the hook called ssh")
		}
	})
}

func editJSON(t *testing.T, sess, projDir, rel string) string {
	return hookJSON(t, sess, projDir, "Edit", map[string]any{"file_path": filepath.Join(projDir, rel)}, nil)
}

// TestNotesOnEditBlock (crit. 1, 3): an allowed edit lists the notes citing the
// file, specific first, capped at 3 with a pointer to the rest; the channel
// block rides the same envelope.
func TestNotesOnEditBlock(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	noSSH(t)
	b := startBoundWork(t, projDir, repoDir, "sess-b", "Revisar a interface")
	startBoundWork(t, projDir, repoDir, "sess-a", "Mexer na infra")
	seedChannel(t, repoDir, b, "build verde")
	seedEditNotes(t, repoDir, map[string][2]string{
		"N-0001": {"full", "edit infra/k8s/scripts/release.sh twice"},
		"N-0002": {"sfx", "see scripts/release.sh"},
		"N-0003": {"sfx2", "scripts/release.sh again"},
		"N-0004": {"sfx3", "scripts/release.sh yet again"},
		"N-0005": {"sfx4", "scripts/release.sh and again"},
	})
	out, err := execCerneStdin(t, editJSON(t, "sess-a", projDir, "infra/k8s/scripts/release.sh"), "hook", "work-gate")
	if err != nil {
		t.Fatal(err)
	}
	c := channelEnvelope(t, out)
	ctx := c.HookSpecificOutput.AdditionalContext
	if !strings.HasPrefix(ctx, "cerne channel:") {
		t.Fatalf("channel first:\n%s", ctx)
	}
	i := strings.Index(ctx, "cerne notes on infra/k8s/scripts/release.sh:\n- N-0001 — full\n")
	if i < 0 {
		t.Fatalf("notes block missing:\n%s", ctx)
	}
	block := ctx[i:]
	if n := strings.Count(block, "\n- N-"); n != 3 {
		t.Fatalf("%d lines, want 3:\n%s", n, block)
	}
	if !strings.HasSuffix(block, "(+2: cerne note list --path infra/k8s/scripts/release.sh)") {
		t.Fatalf("overflow line:\n%s", block)
	}
}

// TestNotesOnEditOncePerSession (crit. 4): the second edit shows nothing the
// session already saw; another session sees it again.
func TestNotesOnEditOncePerSession(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	startBoundWork(t, projDir, repoDir, "sess-a", "Mexer")
	seedEditNotes(t, repoDir, map[string][2]string{"N-0001": {"t", "run infra/release.sh"}})
	in := editJSON(t, "sess-a", projDir, "infra/release.sh")
	if out, _ := execCerneStdin(t, in, "hook", "work-gate"); !strings.Contains(out, "N-0001 — t") {
		t.Fatalf("first: %q", out)
	}
	if out, err := execCerneStdin(t, in, "hook", "work-gate"); err != nil || out != "" {
		t.Fatalf("second: %v %q", err, out)
	}
	if out, _ := execCerneStdin(t, editJSON(t, "sess-z", projDir, "infra/release.sh"), "hook", "work-gate"); !strings.Contains(out, "N-0001") {
		t.Fatalf("other session: %q", out)
	}
}

// TestNotesOnEditSilent (crit. 5, 8): no match, no index, setting off, a denied
// edit, Cursor and Gemini — all exactly as before.
func TestNotesOnEditSilent(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	startBoundWork(t, projDir, repoDir, "sess-a", "Mexer")
	in := editJSON(t, "sess-a", projDir, "infra/release.sh")
	if out, err := execCerneStdin(t, in, "hook", "work-gate"); err != nil || out != "" {
		t.Fatalf("no index: %v %q", err, out)
	}
	seedEditNotes(t, repoDir, map[string][2]string{"N-0001": {"t", "run infra/release.sh"}})
	if out, _ := execCerneStdin(t, editJSON(t, "sess-a", projDir, "other/file.sh"), "hook", "work-gate"); out != "" {
		t.Fatalf("no match: %q", out)
	}
	for _, f := range []string{"cursor", "gemini"} {
		if out, err := execCerneStdin(t, editJSON(t, "sess-"+f, projDir, "infra/release.sh"), "hook", "work-gate", "--format", f); err != nil || out != "" {
			t.Fatalf("%s: %v %q", f, err, out)
		}
	}
	if out, _ := execCerneStdin(t, editJSON(t, "sess-codex", projDir, "infra/release.sh"), "hook", "work-gate", "--format", "codex"); !strings.Contains(out, "N-0001") {
		t.Fatalf("codex: %q", out)
	}
	// the outside-the-repo file carries no notes
	outside := hookJSON(t, "sess-q", projDir, "Edit", map[string]any{"file_path": filepath.Join(t.TempDir(), "infra", "release.sh")}, nil)
	if out, _ := execCerneStdin(t, outside, "hook", "work-gate"); out != "" {
		t.Fatalf("outside repo: %q", out)
	}
	// setting off
	if err := os.WriteFile(filepath.Join(projDir, ".cernerc"), append(mustRead(t, filepath.Join(projDir, ".cernerc")), []byte("\n[hooks]\nnotes_on_edit = false\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, _ := execCerneStdin(t, editJSON(t, "sess-off", projDir, "infra/release.sh"), "hook", "work-gate"); out != "" {
		t.Fatalf("setting off: %q", out)
	}
}

// TestNotesOnEditDenyUnchanged (crit. 5): a denied edit carries no notes.
func TestNotesOnEditDenyUnchanged(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	enableWorkGate(t, projDir)
	seedEditNotes(t, repoDir, map[string][2]string{"N-0001": {"t", "run infra/release.sh"}})
	out, _ := execCerneStdin(t, editJSON(t, "sess-unbound", projDir, "infra/release.sh"), "hook", "work-gate")
	if !strings.Contains(out, "deny") || strings.Contains(out, "N-0001") {
		t.Fatalf("deny output:\n%s", out)
	}
}

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestNoteListPath (crit. 7, 9): `note list --path` lists every active note that
// cites the file, same order as the hook, no cap of 3, no "seen" bookkeeping.
func TestNoteListPath(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	seedEditNotes(t, repoDir, map[string][2]string{
		"N-0001": {"sfx a", "scripts/release.sh"},
		"N-0002": {"full", "infra/k8s/scripts/release.sh"},
		"N-0003": {"sfx b", "scripts/release.sh"},
		"N-0004": {"sfx c", "scripts/release.sh"},
		"N-0005": {"sfx d", "scripts/release.sh"},
		"N-0006": {"unrelated", "nothing here"},
	})
	for i := 0; i < 2; i++ { // twice: listing never marks notes as seen
		out, err := execCerne(t, "note", "list", "--path", "infra/k8s/scripts/release.sh", "--dir", projDir)
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.TrimSpace(out), "\n")
		if len(lines) != 5 || !strings.HasPrefix(lines[0], "N-0002  [active]  full") {
			t.Fatalf("out:\n%s", out)
		}
		if strings.Contains(out, "N-0006") {
			t.Fatalf("unrelated note listed:\n%s", out)
		}
	}
	if out, err := execCerne(t, "note", "list", "--path", "nowhere/else.go", "--dir", projDir); err != nil || !strings.Contains(out, "No active notes cite nowhere/else.go") {
		t.Fatalf("empty: %v %q", err, out)
	}
}

func slowGit(t *testing.T, sleep, out string) {
	t.Helper()
	bin := t.TempDir()
	body := "#!/bin/sh\n[ " + sleep + " != 0 ] && exec sleep " + sleep + "\nprintf '" + out + "'\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// A cold, slow git costs only the bare-name hits: path notes still show.
func TestNotesOnEditSlowGitKeepsPathNotes(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	startBoundWork(t, projDir, repoDir, "sess-a", "Mexer")
	seedEditNotes(t, repoDir, map[string][2]string{
		"N-0001": {"path", "run infra/release.sh"},
		"N-0002": {"bare", "release.sh is flaky"},
	})
	origPath := os.Getenv("PATH")
	slowGit(t, "5", "")
	out, _ := execCerneStdin(t, editJSON(t, "sess-a", projDir, "infra/release.sh"), "hook", "work-gate")
	if !strings.Contains(out, "N-0001") || strings.Contains(out, "N-0002") {
		t.Fatalf("slow git:\n%s", out)
	}
	// real git (a fresh fake script can exceed the 50 ms sub-budget to exec)
	t.Setenv("PATH", origPath)
	for _, args := range [][]string{{"init", "-q", "."}, {"add", "-N", "infra/release.sh"}} {
		if args[0] == "add" {
			if err := os.MkdirAll(filepath.Join(projDir, "infra"), 0o755); err != nil {
				t.Fatal(err)
			}
			_ = os.WriteFile(filepath.Join(projDir, "infra", "release.sh"), nil, 0o644)
		}
		if out, err := exec.Command("git", append([]string{"-C", projDir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	out, _ = execCerneStdin(t, editJSON(t, "sess-b", projDir, "infra/release.sh"), "hook", "work-gate")
	if !strings.Contains(out, "N-0001") || !strings.Contains(out, "N-0002") {
		t.Fatalf("unique name:\n%s", out)
	}
}

// Without a usable session id dedup is impossible: no block at all.
func TestNotesOnEditNoSessionNoBlock(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	seedEditNotes(t, repoDir, map[string][2]string{"N-0001": {"t", "run infra/release.sh"}})
	for _, sess := range []string{"", "a/b"} {
		if out, err := execCerneStdin(t, editJSON(t, sess, projDir, "infra/release.sh"), "hook", "work-gate"); err != nil || out != "" {
			t.Fatalf("sess %q: %v %q", sess, err, out)
		}
	}
}

// The (+N) count is the further matches `note list --path` shows (seen ones
// included); the hint quotes a path with spaces; newlines in a title are flat.
func TestNotesOnEditHintBasisAndSanitizing(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	startBoundWork(t, projDir, repoDir, "sess-a", "Mexer")
	seedEditNotes(t, repoDir, map[string][2]string{
		"N-0001": {"one\nline\r", "my dir/a b.sh"},
		"N-0002": {"two", "my dir/a b.sh"},
		"N-0003": {"three", "my dir/a b.sh"},
		"N-0004": {"four", "my dir/a b.sh"},
	})
	out, _ := execCerneStdin(t, editJSON(t, "sess-a", projDir, "my dir/a b.sh"), "hook", "work-gate")
	ctx := channelEnvelope(t, out).HookSpecificOutput.AdditionalContext
	if !strings.Contains(ctx, "\n(+1: cerne note list --path 'my dir/a b.sh')") {
		t.Fatalf("hint:\n%s", ctx)
	}
	if strings.Count(ctx, "\n") != 4 || strings.Contains(ctx, "\r") {
		t.Fatalf("title not flattened:\n%q", ctx)
	}
	// second edit: one fresh note left, but the list still holds 4 → (+3)
	out, _ = execCerneStdin(t, editJSON(t, "sess-a", projDir, "my dir/a b.sh"), "hook", "work-gate")
	if c := channelEnvelope(t, out).HookSpecificOutput.AdditionalContext; !strings.Contains(c, "(+3: cerne note list --path") {
		t.Fatalf("second:\n%s", c)
	}
}
