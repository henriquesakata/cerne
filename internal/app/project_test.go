package app

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/store"
)

func TestRenameProjectWritesBackend(t *testing.T) {
	repoDir, projDir, origin := newFeatureBackend(t, "core")
	head0, err := gitback.HeadSHA(origin)
	if err != nil {
		t.Fatalf("HeadSHA origin: %v", err)
	}

	got, err := RenameProject(repoDir, projDir, "  Cerne  ")
	if err != nil || got != "Cerne" {
		t.Fatalf("RenameProject = %q, %v; want %q, nil (trimmed)", got, err, "Cerne")
	}
	p, err := store.LoadProject(repoDir)
	if err != nil {
		t.Fatalf("LoadProject: %v", err)
	}
	if want := (store.Project{Name: "Cerne", Modules: []string{"core"}}); !reflect.DeepEqual(p, want) {
		t.Fatalf("project.json = %+v, want %+v (modules kept)", p, want)
	}
	if log := gitOut(t, origin, "log", "--format=%s", head0+"..HEAD"); log != `cerne: rename project to "Cerne"` {
		t.Fatalf("origin log since seed = %q, want exactly one rename commit", log)
	}

	t.Run("64 runes is accepted", func(t *testing.T) {
		long := strings.Repeat("é", 64)
		if got, err := RenameProject(repoDir, projDir, long); err != nil || got != long {
			t.Fatalf("RenameProject(64×é) = %q, %v; want accepted", got, err)
		}
	})
}

func TestRenameProjectRefusesInvalid(t *testing.T) {
	repoDir, projDir, origin := newFeatureBackend(t, "core")
	for _, tc := range []struct{ in, want string }{
		{"", "empty"},
		{"   ", "empty"},
		{strings.Repeat("é", 65), "64"},
		{"a\nb", "control"},
		{"a\tb", "control"},
	} {
		t.Run(tc.in, func(t *testing.T) {
			cloneHead, _ := gitback.HeadSHA(repoDir)
			originHead, _ := gitback.HeadSHA(origin)
			before, err := os.ReadFile(filepath.Join(repoDir, "project.json"))
			if err != nil {
				t.Fatalf("read project.json: %v", err)
			}

			_, err = RenameProject(repoDir, projDir, tc.in)
			if !errors.Is(err, ErrInvalidProjectName) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("RenameProject(%q) err = %v; want ErrInvalidProjectName mentioning %q", tc.in, err, tc.want)
			}
			if h, _ := gitback.HeadSHA(repoDir); h != cloneHead {
				t.Errorf("clone HEAD moved: %s -> %s", cloneHead, h)
			}
			if h, _ := gitback.HeadSHA(origin); h != originHead {
				t.Errorf("origin HEAD moved: %s -> %s", originHead, h)
			}
			after, _ := os.ReadFile(filepath.Join(repoDir, "project.json"))
			if !bytes.Equal(before, after) {
				t.Errorf("project.json changed:\n%s\n->\n%s", before, after)
			}
		})
	}
}

func TestRenameProjectSameNameNoCommit(t *testing.T) {
	repoDir, projDir, origin := newFeatureBackend(t, "core")
	head0, _ := gitback.HeadSHA(origin)

	if got, err := RenameProject(repoDir, projDir, " Demo "); err != nil || got != "Demo" {
		t.Fatalf("RenameProject(same) = %q, %v; want %q, nil", got, err, "Demo")
	}
	if h, _ := gitback.HeadSHA(origin); h != head0 {
		t.Fatalf("same name committed: origin %s -> %s", head0, h)
	}

	// renameElsewhere renames the project from a second clone, leaving repoDir
	// trailing origin.
	renameElsewhere := func(t *testing.T, name string) {
		t.Helper()
		other := filepath.Join(t.TempDir(), "other")
		if err := gitback.Clone(origin, other); err != nil {
			t.Fatalf("Clone: %v", err)
		}
		if err := store.SaveProject(other, store.Project{Name: name, Modules: []string{"core"}}); err != nil {
			t.Fatalf("SaveProject: %v", err)
		}
		if _, err := gitback.CommitAndPush(other, "rename elsewhere"); err != nil {
			t.Fatalf("CommitAndPush: %v", err)
		}
	}

	t.Run("trailing clone compares against origin", func(t *testing.T) {
		renameElsewhere(t, "Outro")
		count := gitOut(t, origin, "rev-list", "--count", "HEAD")

		if got, err := RenameProject(repoDir, projDir, "Outro"); err != nil || got != "Outro" {
			t.Fatalf("RenameProject(origin's name) = %q, %v; want %q, nil", got, err, "Outro")
		}
		if after := gitOut(t, origin, "rev-list", "--count", "HEAD"); after != count {
			t.Fatalf("origin commit count %s -> %s: an already-current name committed", count, after)
		}
	})

	// The clone's stale name is not "unchanged" when origin moved on: without
	// the fast-forward this would be a silent no-op leaving origin's name.
	t.Run("trailing clone's stale name still renames origin", func(t *testing.T) {
		renameElsewhere(t, "Terceiro")
		if got, err := RenameProject(repoDir, projDir, "Outro"); err != nil || got != "Outro" {
			t.Fatalf("RenameProject(clone's stale name) = %q, %v; want %q, nil", got, err, "Outro")
		}
		if pj := gitOut(t, origin, "show", "HEAD:project.json"); !strings.Contains(pj, `"Outro"`) {
			t.Fatalf("origin project.json = %s, want name Outro", pj)
		}
	})
}
