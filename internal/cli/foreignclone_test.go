package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/gitback"
)

func bareHead(t *testing.T, bare string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", bare, "for-each-ref").CombinedOutput()
	if err != nil {
		t.Fatalf("for-each-ref %s: %v\n%s", bare, err, out)
	}
	return strings.TrimSpace(string(out))
}

// foreignPair returns project A (owner of the cache clone), its bare backend,
// and project B, which shares A's project_id and cache but declares another
// backend. The cache env is left pointing at A's cache.
func foreignPair(t *testing.T) (projA, originA, projB string) {
	t.Helper()
	projA, originA, _ = setupProject(t) // project_id "demo"
	cacheA := cacheRootEnv(t)
	// B is initialized against its own cache, then pointed at A's cache.
	t.Setenv("CERNE_CACHE_DIR", t.TempDir())
	projB = t.TempDir()
	if out, err := execInit(t, "--docs-language", "pt-BR", "--name", "demo", "--backend", newOrigin(t), "--dir", projB); err != nil {
		t.Fatalf("init B: %v\n%s", err, out)
	}
	t.Setenv("CERNE_CACHE_DIR", cacheA)
	return projA, originA, projB
}

// TestResolve_RefusesForeignClone pins F-0434 criterion 1: project B shares
// project A's project_id (hence its cache clone) but declares another
// backend; every command that reads or writes the clone refuses it and A's
// backend never moves.
func TestResolve_RefusesForeignClone(t *testing.T) {
	projA, originA, projB := foreignPair(t)
	headA := bareHead(t, originA)
	for _, args := range [][]string{
		{"brief", "--dir", projB},
		{"capability", "new", "Intruder", "--dir", projB},
		{"push", "--dir", projB},
		{"pull", "--dir", projB},
		{"sync", "--dir", projB},
		{"msg", "read", "--session", "s1", "--dir", projB},
	} {
		out, err := execCerne(t, args...)
		if !errors.Is(err, gitback.ErrForeignClone) {
			t.Fatalf("%v: err = %v, want ErrForeignClone\n%s", args, err, out)
		}
	}
	if got := bareHead(t, originA); got != headA {
		t.Fatalf("A's backend moved: %s -> %s", headA, got)
	}
	if _, err := execCerne(t, "brief", "--dir", projA); err != nil {
		t.Fatalf("A must keep working: %v", err)
	}
}

// A relative backend in .cernerc is relative to the .cernerc directory, not
// the process cwd (F-0434).
func TestResolve_RelativeBackendIsRelativeToCernerc(t *testing.T) {
	projA, originA, _ := setupProject(t)
	rel, err := filepath.Rel(projA, originA)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(projA, ".cernerc")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), originA) {
		t.Fatalf(".cernerc does not hold the backend path:\n%s", raw)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(raw), originA, rel, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	if out, err := execCerne(t, "brief", "--dir", projA); err != nil {
		t.Fatalf("brief from another cwd: %v\n%s", err, out)
	}
}

func cacheRootEnv(t *testing.T) string {
	t.Helper()
	return os.Getenv("CERNE_CACHE_DIR")
}

// breakOrigin renames the clone's origin remote, so its origin cannot be read.
func breakOrigin(t *testing.T, repoDir string) {
	t.Helper()
	cfg := filepath.Join(repoDir, ".git", "config")
	raw, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg, []byte(strings.Replace(string(raw), `[remote "origin"]`, `[remote "gone"]`, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestResolve_RefusesUnverifiableClone: a clone without an origin cannot be
// proven ours and is refused with its own sentinel (F-0434).
func TestResolve_RefusesUnverifiableClone(t *testing.T) {
	projA, _, repoDir := setupProject(t)
	breakOrigin(t, repoDir)
	out, err := execCerne(t, "brief", "--dir", projA)
	if !errors.Is(err, gitback.ErrUnverifiableClone) {
		t.Fatalf("err = %v, want ErrUnverifiableClone\n%s", err, out)
	}
}

// cacheSnapshot lists every file under dir with size and mtime.
func cacheSnapshot(t *testing.T, dir string) string {
	t.Helper()
	var b strings.Builder
	_ = filepath.Walk(dir, func(p string, fi os.FileInfo, err error) error {
		if err == nil {
			fmt.Fprintf(&b, "%s %d %d\n", p, fi.Size(), fi.ModTime().UnixNano())
		}
		return nil
	})
	return b.String()
}

// TestHooks_FailOpenOnForeignClone: every hook entry point that reads the
// backend clone exits 0, leaks nothing of the clone and writes nothing when
// the clone is another backend's or unverifiable (F-0434).
func TestHooks_FailOpenOnForeignClone(t *testing.T) {
	for _, scenario := range []string{"foreign", "unverifiable"} {
		t.Run(scenario, func(t *testing.T) {
			var proj string
			var origin string
			if scenario == "foreign" {
				var projA string
				projA, origin, proj = foreignPair(t)
				seedLeak(t, projA)
			} else {
				var repoDir string
				proj, origin, repoDir = setupProject(t)
				seedLeak(t, proj)
				breakOrigin(t, repoDir)
			}
			cacheDir := os.Getenv("CERNE_CACHE_DIR")
			msg := writeMsgFile(t, proj, "feat: x (F-0001)\n")
			enableWorkGate(t, proj)
			head, snap := bareHead(t, origin), cacheSnapshot(t, cacheDir)
			for _, h := range []struct {
				name  string
				stdin string
				args  []string
			}{
				{"bind-work", hookJSON(t, "s1", proj, "Bash", map[string]any{"command": `cerne work start "Leak work"`}, map[string]any{"stdout": "W-0001 criada (active) — Leak work"}), []string{"hook", "bind-work"}},
				{"work-gate", hookJSON(t, "s1", proj, "Edit", map[string]any{"file_path": filepath.Join(proj, "a.go")}, nil), []string{"hook", "work-gate"}},
				{"stop-check", stopPayload(t, "s1", proj), []string{"hook", "stop-check"}},
				{"precompact", stopPayload(t, "s1", proj), []string{"hook", "precompact"}},
				{"prompt-notes", promptJSON(t, "s1", proj, "Leak work leak note"), []string{"hook", "prompt-notes"}},
				{"session-context", sessionPayload("s1", proj, "compact"), []string{"hook", "session-context"}},
				{"channel", sessionStartJSON(t, "s1", proj), []string{"hook", "channel", "--point", "session-start"}},
				{"provenance", hookJSON(t, "s1", proj, "Bash", map[string]any{"command": "git push"}, nil), []string{"hook", "provenance"}},
				{"commit-trailer", "", []string{"hook", "commit-trailer", msg}},
				{"guard commit-msg", "", []string{"guard", "commit-msg", msg}},
			} {
				out, err := execCerneStdin(t, h.stdin, h.args...)
				if err != nil {
					t.Errorf("%s: exit %v, want fail-open\n%s", h.name, err, out)
				}
				if strings.Contains(out, "Leak") || strings.Contains(out, "W-0001") || strings.Contains(out, "F-0001") {
					t.Errorf("%s leaks clone data:\n%s", h.name, out)
				}
			}
			if raw, _ := os.ReadFile(msg); string(raw) != "feat: x (F-0001)\n" {
				t.Errorf("commit-msg rewritten: %q", raw)
			}
			if got := bareHead(t, origin); got != head {
				t.Errorf("backend moved")
			}
			if got := cacheSnapshot(t, cacheDir); got != snap {
				t.Errorf("hooks wrote into the cache:\nbefore:\n%s\nafter:\n%s", snap, got)
			}
		})
	}
}

// seedLeak puts a feature and an active work, both titled "Leak ...", in the
// backend of projDir.
func seedLeak(t *testing.T, projDir string) {
	t.Helper()
	for _, args := range [][]string{
		{"feature", "new", "Leak marker", "--context", "ctx", "--dir", projDir},
		{"work", "start", "Leak work", "--dir", projDir},
	} {
		if out, err := execCerne(t, args...); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}
}
