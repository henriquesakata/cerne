package lint

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

func writeResultFixture(t *testing.T, state domain.FeatureState, spec, result string) (string, store.Feature) {
	t.Helper()
	dir := t.TempDir()
	f := store.Feature{ID: "F-0001", Title: "demo", State: state, CreatedAt: time.Unix(0, 0).UTC()}
	if err := store.SaveFeature(dir, f); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}
	docDir := filepath.Join(dir, "content", "features", f.ID)
	if err := os.MkdirAll(docDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"spec.md": spec, "result.md": result} {
		if err := os.WriteFile(filepath.Join(docDir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir, f
}

const checkSpec = "## Objetivo e critérios de aceitação\n\n1. a\n2. b\n"

func TestCheckResultIgnoresStateAndCutoffs(t *testing.T) {
	dir, f := writeResultFixture(t, domain.StateInDevelopment, checkSpec, "critério 1 ok\n")
	got := CheckResult(dir, f)
	if len(got) != 2 || got[0].Rule != RuleResultMissingEvidence || got[1].Rule != RuleResultMissingGauntlet {
		t.Fatalf("want evidence then gauntlet findings on an in_development feature, got %+v", got)
	}
}

func TestCheckResultCleanRecord(t *testing.T) {
	result := "critério 1 e critério 2 provados\n\n## Gauntlet\n\nBarra: não aplicável (fixture)\n"
	dir, f := writeResultFixture(t, domain.StateInDevelopment, checkSpec, result)
	if got := CheckResult(dir, f); len(got) != 0 {
		t.Fatalf("want no findings, got %+v", got)
	}
}
