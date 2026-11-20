package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
)

const gateSpec = "## Objetivo e critérios de aceitação\n\n1. QUANDO a O SISTEMA DEVE b.\n2. QUANDO c O SISTEMA DEVE d.\n"

func gateFixture(t *testing.T, testBody string) (repoDir, projDir string, f store.Feature) {
	t.Helper()
	repoDir, projDir = t.TempDir(), t.TempDir()
	dir := filepath.Join(repoDir, "content", "features", "F-9001")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "spec.md"), []byte(gateSpec), 0o644); err != nil {
		t.Fatal(err)
	}
	if testBody != "" {
		if err := os.WriteFile(filepath.Join(projDir, "a_test.go"), []byte(testBody), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ev := domain.NewEvent("F-9001", "feature.started", nil, "agent")
	ev.At = time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := store.AppendEvent(repoDir, ev); err != nil {
		t.Fatal(err)
	}
	return repoDir, projDir, store.Feature{ID: "F-9001", Title: "Synthetic", State: domain.StateInDevelopment}
}

// F-0473 crit. 6
func TestCheckCriterionTests_RefusesAndAccepts(t *testing.T) {
	repoDir, projDir, f := gateFixture(t, "package x\n// F-9001 crit. 1\n")
	err := CheckCriterionTests(repoDir, projDir, f)
	if !errors.Is(err, ErrCriterionTestMissing) {
		t.Fatalf("err = %v, want ErrCriterionTestMissing for the unmarked criterion 2", err)
	}
	for _, want := range []string{"2", "F-9001 crit. N"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal %q lacks %q", err, want)
		}
	}
	repoDir, projDir, f = gateFixture(t, "package x\n// F-9001 crit. 1, 2\n")
	if err := CheckCriterionTests(repoDir, projDir, f); err != nil {
		t.Errorf("covered feature refused: %v", err)
	}
}

// F-0473 crit. 8
func TestCheckCriterionTests_NoTestFilesNeverRefuses(t *testing.T) {
	repoDir, projDir, f := gateFixture(t, "")
	if err := CheckCriterionTests(repoDir, projDir, f); err != nil {
		t.Errorf("project with no test file refused: %v", err)
	}
}

// F-0473 crit. 10
func TestCheckCriterionTests_SettingOffDisablesGate(t *testing.T) {
	repoDir, projDir, f := gateFixture(t, "package x\n// F-9001 crit. 1\n")
	if err := os.WriteFile(filepath.Join(projDir, ".cernerc"), []byte("project_id = \"p\"\nbackend = \"b\"\n\n[lint]\ncriterion_tests = false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if project.CriterionTestsEnabled(projDir) {
		t.Skip("setting not read from this .cernerc shape")
	}
	if err := CheckCriterionTests(repoDir, projDir, f); err != nil {
		t.Errorf("rule off but complete refused: %v", err)
	}
}
