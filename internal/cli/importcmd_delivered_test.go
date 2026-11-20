package cli

import (
	"errors"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/importer"
	"github.com/henriquesakata/cerne/internal/store"
)

func TestImportDeliveredStateFlag(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want domain.FeatureState
	}{
		{"default", nil, domain.StateImplemented},
		{"implemented", []string{"--delivered-state", "implemented"}, domain.StateImplemented},
		{"done", []string{"--delivered-state", "done"}, domain.StateDone},
	}
	for _, tt := range tests {
		t.Run("case-folder/"+tt.name, func(t *testing.T) {
			projDir, _, repoDir := setupProject(t)
			treeDir := t.TempDir()
			writeTree(t, treeDir, importFixtureFiles())
			args := append([]string{"import", "--root", treeDir, "--yes", "--dir", projDir}, tt.args...)
			if out, err := execCerne(t, args...); err != nil {
				t.Fatalf("import: %v\n%s", err, out)
			}
			fs, _ := store.LoadAllFeatures(repoDir)
			if len(fs) != 2 || fs[0].State != tt.want || fs[1].State != domain.StateCandidate {
				t.Fatalf("features = %+v, want [%s candidate]", fs, tt.want)
			}
		})
		t.Run("agent/"+tt.name, func(t *testing.T) {
			projDir, _, repoDir := setupProject(t)
			writeAgentPlan(t, projDir, gabaritoPlan)
			args := append([]string{"import", "--agent", "--ingest", "--yes", "--dir", projDir}, tt.args...)
			if out, err := execCerne(t, args...); err != nil {
				t.Fatalf("ingest: %v\n%s", err, out)
			}
			fs, _ := store.LoadAllFeatures(repoDir)
			if len(fs) != 1 || fs[0].State != tt.want {
				t.Fatalf("features = %+v, want state %s", fs, tt.want)
			}
		})
	}
}

func TestImportDeliveredStateInvalid(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	treeDir := t.TempDir()
	writeTree(t, treeDir, importFixtureFiles())
	_, err := execCerne(t, "import", "--root", treeDir, "--yes", "--delivered-state", "approved", "--dir", projDir)
	if !errors.Is(err, importer.ErrInvalidDeliveredState) {
		t.Fatalf("err = %v, want ErrInvalidDeliveredState", err)
	}
	if fs, _ := store.LoadAllFeatures(repoDir); len(fs) != 0 {
		t.Fatalf("invalid value still imported %d feature(s)", len(fs))
	}
}
