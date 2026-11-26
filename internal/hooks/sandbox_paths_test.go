package hooks

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// bareRepo makes dir look like a bare git repository (HEAD + objects/ + refs/).
func bareRepo(t *testing.T, dir string) {
	t.Helper()
	for _, d := range []string{"objects", "refs"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// F-0512 crit. 1 (review #1b): the shared helper (Codex and Cursor) opens a
// cache folder only for a project_id that is one safe folder name, and a local
// backend only when it is a git repository that is neither the home nor holds
// it; anything else is left out with a note.
func TestSandboxPathsRefuseUnsafeFolders(t *testing.T) {
	_, home := codexProject(t, "x")
	bare := filepath.Join(home, "backends", "demo.git")
	bareRepo(t, bare)
	work := filepath.Join(home, "work", "docs")
	if err := os.MkdirAll(filepath.Join(work, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	ssh := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(ssh, 0o700); err != nil {
		t.Fatal(err)
	}
	bareRepo(t, home) // even a home that is a repository is never opened
	cases := []struct {
		name, id, backend string
		want              []string // exact portable list
		note              string   // "" = no note
	}{
		{"bare backend under home", "demo", bare, []string{"~/Library/Caches/cerne/demo", "~/.cache/cerne/demo", "~/backends/demo.git"}, ""},
		{"working-copy backend", "demo", work, []string{"~/Library/Caches/cerne/demo", "~/.cache/cerne/demo", "~/work/docs"}, ""},
		{"backend not a repository", "demo", ssh, []string{"~/Library/Caches/cerne/demo", "~/.cache/cerne/demo"}, "not a git repository"},
		{"backend missing", "demo", filepath.Join(home, "nope"), []string{"~/Library/Caches/cerne/demo", "~/.cache/cerne/demo"}, "not a git repository"},
		{"backend is the home", "demo", home, []string{"~/Library/Caches/cerne/demo", "~/.cache/cerne/demo"}, "home"},
		{"backend holds the home", "demo", filepath.Dir(home), []string{"~/Library/Caches/cerne/demo", "~/.cache/cerne/demo"}, "home"},
		{"project_id climbs", "../../..", bare, []string{"~/backends/demo.git"}, "project_id"},
		{"project_id with a slash", "a/b", bare, []string{"~/backends/demo.git"}, "project_id"},
		{"project_id with a backslash", `a\b`, bare, []string{"~/backends/demo.git"}, "project_id"},
		{"project_id dot", ".", bare, []string{"~/backends/demo.git"}, "project_id"},
		{"project_id absolute", "/etc", bare, []string{"~/backends/demo.git"}, "project_id"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			proj := t.TempDir()
			rc := "project_id = '" + tc.id + "'\nbackend = '" + tc.backend + "'\n"
			if err := os.WriteFile(filepath.Join(proj, ".cernerc"), []byte(rc), 0o644); err != nil {
				t.Fatal(err)
			}
			portable, machine, notes, err := sandboxPaths(proj)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(portable, tc.want) || len(machine) != 0 {
				t.Errorf("portable = %v machine = %v, want %v", portable, machine, tc.want)
			}
			joined := strings.Join(notes, "\n")
			if tc.note == "" && joined != "" || tc.note != "" && !strings.Contains(joined, tc.note) {
				t.Errorf("notes = %q, want one about %q", joined, tc.note)
			}
		})
	}
}

// F-0512 crit. 1 and 2 (review #1c): the install note names exactly what it
// added — each path and the host — and a run that adds nothing names nothing.
func TestInstallCursorSandboxNoteNamesWhatItAdded(t *testing.T) {
	proj, _ := codexProject(t, "git@git.example.com:o/r.git")
	writeCursorSandbox(t, proj, `{"additionalReadwritePaths":["~/.cache/cerne/demo"]}`)
	rs, err := InstallNetwork(proj, true, ProviderCursor)
	if err != nil {
		t.Fatal(err)
	}
	note := cursorSandboxResult(t, rs).Note
	for _, want := range []string{"added to additionalReadwritePaths: ~/Library/Caches/cerne/demo", "added to networkPolicy.allow: git.example.com"} {
		if !strings.Contains(note, want) {
			t.Errorf("note lacks %q: %q", want, note)
		}
	}
	if strings.Contains(note, "~/.cache/cerne/demo") {
		t.Errorf("note names a path the user already had: %q", note)
	}
	rs, err = InstallNetwork(proj, true, ProviderCursor)
	if err != nil {
		t.Fatal(err)
	}
	if note := cursorSandboxResult(t, rs).Note; strings.Contains(note, "added to") {
		t.Errorf("second run claims additions: %q", note)
	}
}

// Review #1b through an adapter: the reviewer's ~/.ssh backend never reaches
// .cursor/sandbox.json even on an explicit install, and the note says why.
func TestInstallCursorSandboxSkipsANonRepositoryBackend(t *testing.T) {
	proj, home := codexProject(t, "x")
	ssh := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(ssh, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, ".cernerc"), []byte("project_id = \"demo\"\nbackend = \""+ssh+"\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rs, err := InstallNetwork(proj, false, ProviderCursor)
	if err != nil {
		t.Fatal(err)
	}
	if _, raw := readCursorSandbox(t, proj); strings.Contains(raw, ".ssh") {
		t.Errorf("~/.ssh written:\n%s", raw)
	}
	if r := cursorSandboxResult(t, rs); !strings.Contains(r.Note, "not a git repository") {
		t.Errorf("note does not say why the backend was left out: %q", r.Note)
	}
}
