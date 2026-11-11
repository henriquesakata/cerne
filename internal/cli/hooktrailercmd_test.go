package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/project"
)

// enableCommitTrailer flips [gates] commit_trailer on for the project.
func enableCommitTrailer(t *testing.T, projDir string) {
	t.Helper()
	cfg, _, err := project.LoadConfig(projDir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	cfg.Gates.CommitTrailer = "on"
	if err := project.SaveConfig(projDir, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
}

// writeMsgFile lays a COMMIT_EDITMSG-style file inside the project tree (git
// hooks receive a path under .git/, so LoadConfig walks up to the project).
func writeMsgFile(t *testing.T, projDir, content string) string {
	t.Helper()
	dir := filepath.Join(projDir, ".git")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "COMMIT_EDITMSG")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

const msgWithComments = "feat: coisa\n\ncorpo da mensagem\n\n# Please enter the commit message\n# for your changes.\n"

// TestHookCommitTrailerStampsSingleLiveBinding (F-0173 crit. 1): toggle on +
// exactly one live binding -> the trailer lands after the message body, the
// comment block preserved byte for byte.
func TestHookCommitTrailerStampsSingleLiveBinding(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	enableCommitTrailer(t, projDir)
	if out, err := execCerne(t, "work", "start", "Investigar erro X", "--dir", projDir); err != nil {
		t.Fatalf("work start: %v\noutput: %s", err, out)
	}
	if err := app.BindSession(filepath.Dir(repoDir), "sess-a", "W-0001"); err != nil {
		t.Fatal(err)
	}
	p := writeMsgFile(t, projDir, msgWithComments)
	if out, err := execCerneStdin(t, "", "hook", "commit-trailer", p); err != nil {
		t.Fatalf("hook commit-trailer: %v\noutput: %s", err, out)
	}
	got, _ := os.ReadFile(p)
	if !strings.Contains(string(got), "corpo da mensagem\n\nCerne: W-0001\n") {
		t.Fatalf("trailer not stamped after body:\n%s", got)
	}
	if !strings.Contains(string(got), "# Please enter the commit message\n# for your changes.\n") {
		t.Fatalf("comment block not preserved:\n%s", got)
	}
}

// TestHookCommitTrailerNoOps (F-0173 crit. 2/3/4/6): ambiguity (0 or 2 live
// bindings), an existing Cerne: trailer, a disabled toggle, and a missing file
// all leave the message untouched and exit 0.
func TestHookCommitTrailerNoOps(t *testing.T) {
	t.Run("zero bindings", func(t *testing.T) {
		projDir, _, _ := setupProject(t)
		enableCommitTrailer(t, projDir)
		p := writeMsgFile(t, projDir, msgWithComments)
		if _, err := execCerneStdin(t, "", "hook", "commit-trailer", p); err != nil {
			t.Fatal(err)
		}
		got, _ := os.ReadFile(p)
		if string(got) != msgWithComments {
			t.Fatalf("message changed:\n%s", got)
		}
	})
	t.Run("two live bindings", func(t *testing.T) {
		projDir, _, repoDir := setupProject(t)
		enableCommitTrailer(t, projDir)
		for i, title := range []string{"Investigar erro X", "Revisar MR Y"} {
			if out, err := execCerne(t, "work", "start", title, "--dir", projDir); err != nil {
				t.Fatalf("work start %d: %v\noutput: %s", i, err, out)
			}
		}
		cacheDir := filepath.Dir(repoDir)
		if err := app.BindSession(cacheDir, "sess-a", "W-0001"); err != nil {
			t.Fatal(err)
		}
		if err := app.BindSession(cacheDir, "sess-b", "W-0002"); err != nil {
			t.Fatal(err)
		}
		p := writeMsgFile(t, projDir, msgWithComments)
		if _, err := execCerneStdin(t, "", "hook", "commit-trailer", p); err != nil {
			t.Fatal(err)
		}
		got, _ := os.ReadFile(p)
		if string(got) != msgWithComments {
			t.Fatalf("ambiguous bindings must not stamp:\n%s", got)
		}
	})
	t.Run("existing trailer", func(t *testing.T) {
		projDir, _, repoDir := setupProject(t)
		enableCommitTrailer(t, projDir)
		if out, err := execCerne(t, "work", "start", "Investigar erro X", "--dir", projDir); err != nil {
			t.Fatalf("work start: %v\noutput: %s", err, out)
		}
		if err := app.BindSession(filepath.Dir(repoDir), "sess-a", "W-0001"); err != nil {
			t.Fatal(err)
		}
		pre := "fix: y\n\nCerne: F-0999\n"
		p := writeMsgFile(t, projDir, pre)
		if _, err := execCerneStdin(t, "", "hook", "commit-trailer", p); err != nil {
			t.Fatal(err)
		}
		got, _ := os.ReadFile(p)
		if string(got) != pre {
			t.Fatalf("existing trailer must not be duplicated:\n%s", got)
		}
	})
	t.Run("toggle off", func(t *testing.T) {
		projDir, _, repoDir := setupProject(t)
		if out, err := execCerne(t, "work", "start", "Investigar erro X", "--dir", projDir); err != nil {
			t.Fatalf("work start: %v\noutput: %s", err, out)
		}
		if err := app.BindSession(filepath.Dir(repoDir), "sess-a", "W-0001"); err != nil {
			t.Fatal(err)
		}
		p := writeMsgFile(t, projDir, msgWithComments)
		if _, err := execCerneStdin(t, "", "hook", "commit-trailer", p); err != nil {
			t.Fatal(err)
		}
		got, _ := os.ReadFile(p)
		if string(got) != msgWithComments {
			t.Fatalf("disabled toggle must not stamp:\n%s", got)
		}
	})
	t.Run("missing file", func(t *testing.T) {
		projDir, _, _ := setupProject(t)
		enableCommitTrailer(t, projDir)
		if _, err := execCerneStdin(t, "", "hook", "commit-trailer", filepath.Join(projDir, "nope")); err != nil {
			t.Fatalf("missing file must be fail-open: %v", err)
		}
	})
}
