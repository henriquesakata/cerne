package app

import (
	"bytes"
	"encoding/json"
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

}

// nameCase is one row of testdata/project_names.json, the corpus the UI's
// ui/src/projectName.test.ts reads too: a rule added on one side only breaks
// the other side's run.
type nameCase struct {
	Name   string `json:"name"`
	In     string `json:"in"`
	Rule   string `json:"rule"`
	Stored string `json:"stored"`
}

func loadNameCases(t *testing.T) []nameCase {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "project_names.json"))
	if err != nil {
		t.Fatalf("read corpus: %v", err)
	}
	var doc struct{ Cases []nameCase }
	if err := json.Unmarshal(data, &doc); err != nil || len(doc.Cases) == 0 {
		t.Fatalf("corpus: %d cases, %v", len(doc.Cases), err)
	}
	return doc.Cases
}

// TestRenameProjectRules pins F-0331 crit. 4 on the shared corpus: a refused
// name names its rule and leaves the clone, origin and project.json as they
// were; an accepted one is stored trimmed.
func TestRenameProjectRules(t *testing.T) {
	repoDir, projDir, origin := newFeatureBackend(t, "core")
	ruleText := map[string]string{"empty": "empty", "long": "64", "control": "control"}
	for _, tc := range loadNameCases(t) {
		t.Run(tc.Name, func(t *testing.T) {
			if tc.Rule == "" {
				got, err := RenameProject(repoDir, projDir, tc.In)
				if err != nil || got != tc.Stored {
					t.Fatalf("RenameProject(%+q) = %+q, %v; want %+q, nil", tc.In, got, err, tc.Stored)
				}
				if p, _ := store.LoadProject(repoDir); p.Name != tc.Stored {
					t.Fatalf("stored name = %+q, want %+q", p.Name, tc.Stored)
				}
				return
			}
			cloneHead, _ := gitback.HeadSHA(repoDir)
			originHead, _ := gitback.HeadSHA(origin)
			before, err := os.ReadFile(filepath.Join(repoDir, "project.json"))
			if err != nil {
				t.Fatalf("read project.json: %v", err)
			}

			_, err = RenameProject(repoDir, projDir, tc.In)
			if !errors.Is(err, ErrInvalidProjectName) || !strings.Contains(err.Error(), ruleText[tc.Rule]) {
				t.Fatalf("RenameProject(%+q) err = %v; want ErrInvalidProjectName mentioning %q", tc.In, err, ruleText[tc.Rule])
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

	// The clone's stale name is not "unchanged" when origin moved on: without
	// the fast-forward this would be a silent no-op leaving origin's name.
	t.Run("trailing clone's stale name still renames origin", func(t *testing.T) {
		if _, err := RenameProject(repoDir, projDir, "Outro"); err != nil {
			t.Fatalf("RenameProject(Outro): %v", err)
		}
		renameElsewhere(t, "Terceiro") // repoDir now trails origin, still holding "Outro"
		if got, err := RenameProject(repoDir, projDir, "Outro"); err != nil || got != "Outro" {
			t.Fatalf("RenameProject(clone's stale name) = %q, %v; want %q, nil", got, err, "Outro")
		}
		if pj := gitOut(t, origin, "show", "HEAD:project.json"); !strings.Contains(pj, `"Outro"`) {
			t.Fatalf("origin project.json = %s, want name Outro", pj)
		}
	})
}
