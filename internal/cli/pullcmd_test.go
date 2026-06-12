package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/project"
)

func TestPullGoldenPath(t *testing.T) {
	projDir, origin, repoDir := setupProject(t)
	id := newFeature(t, projDir, "Parcelamento", 1)
	writeDoc(t, repoDir, id, "spec.md", realDoc(id, "Dividir compras em parcelas."))
	if _, err := gitback.CommitAndPush(repoDir, "spec F-0001"); err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}

	out, err := execCerne(t, "pull", "--dir", projDir)
	if err != nil {
		t.Fatalf("pull: %v\noutput: %s", err, out)
	}

	docsDir := filepath.Join(projDir, ".docs")
	specPath := filepath.Join(docsDir, "current", "features", "F-0001-parcelamento", "spec.md")
	spec, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("materialized spec: %v", err)
	}
	if !strings.Contains(string(spec), "Dividir compras em parcelas.") {
		t.Fatalf("materialized spec content = %q, want the real spec body", spec)
	}
	ctx, err := os.ReadFile(filepath.Join(docsDir, "CONTEXT.md"))
	if err != nil {
		t.Fatalf("CONTEXT.md: %v", err)
	}
	for _, want := range []string{"F-0001", "Parcelamento", "candidate"} {
		if !strings.Contains(string(ctx), want) {
			t.Fatalf("CONTEXT.md missing %q:\n%s", want, ctx)
		}
	}

	lock, err := project.LoadLock(projDir)
	if err != nil {
		t.Fatalf("LoadLock: %v", err)
	}
	if len(lock.Materialized) == 0 {
		t.Fatalf("lock.Materialized empty, want manifest of written files")
	}
	for _, rel := range []string{"CONTEXT.md", "current/features/F-0001-parcelamento/spec.md"} {
		if _, ok := lock.Materialized[rel]; !ok {
			t.Fatalf("lock.Materialized missing %s: %v", rel, lock.Materialized)
		}
	}
	originHead, err := gitback.HeadSHA(origin)
	if err != nil {
		t.Fatalf("HeadSHA origin: %v", err)
	}
	if lock.Snapshot != originHead {
		t.Fatalf("lock.Snapshot = %s, want origin HEAD %s", lock.Snapshot, originHead)
	}

	want := "Materializado: 1 feature(s), 0 decisão(ões), 0 referência(s) em .docs/ (snapshot " + originHead[:8] + ")."
	if !strings.Contains(out, want) {
		t.Fatalf("output missing summary %q:\n%s", want, out)
	}
}

func TestPullScopeFilters(t *testing.T) {
	projDir, _, repoDir := setupProject(t, "finance", "core")
	if out, err := execCerne(t, "feature", "new", "Parcelamento", "--module", "finance", "--dir", projDir); err != nil {
		t.Fatalf("feature new finance: %v\noutput: %s", err, out)
	}
	if out, err := execCerne(t, "feature", "new", "Tema escuro", "--module", "core", "--dir", projDir); err != nil {
		t.Fatalf("feature new core: %v\noutput: %s", err, out)
	}
	writeDoc(t, repoDir, "F-0001", "spec.md", realDoc("F-0001", "Spec finance."))
	writeDoc(t, repoDir, "F-0002", "spec.md", realDoc("F-0002", "Spec core."))
	if _, err := gitback.CommitAndPush(repoDir, "specs"); err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}

	out, err := execCerne(t, "pull", "--scope", "finance", "--dir", projDir)
	if err != nil {
		t.Fatalf("pull --scope: %v\noutput: %s", err, out)
	}
	docsDir := filepath.Join(projDir, ".docs")
	if _, err := os.Stat(filepath.Join(docsDir, "current", "features", "F-0001-parcelamento", "spec.md")); err != nil {
		t.Fatalf("scoped feature not materialized: %v", err)
	}
	if _, err := os.Stat(filepath.Join(docsDir, "current", "features", "F-0002-tema-escuro")); err == nil {
		t.Fatalf("out-of-scope feature materialized")
	}
	ctx, err := os.ReadFile(filepath.Join(docsDir, "CONTEXT.md"))
	if err != nil {
		t.Fatalf("CONTEXT.md: %v", err)
	}
	if strings.Contains(string(ctx), "F-0002") {
		t.Fatalf("CONTEXT.md leaks out-of-scope feature:\n%s", ctx)
	}
	if !strings.Contains(out, "Materializado: 1 feature(s)") {
		t.Fatalf("output summary should count only scoped features:\n%s", out)
	}

	// Without --scope, .cernerc default_scope is the fallback.
	cfg, _, err := project.LoadConfig(projDir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	cfg.DefaultScope = "core"
	if err := project.SaveConfig(projDir, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	out, err = execCerne(t, "pull", "--dir", projDir)
	if err != nil {
		t.Fatalf("pull with default_scope: %v\noutput: %s", err, out)
	}
	if _, err := os.Stat(filepath.Join(docsDir, "current", "features", "F-0002-tema-escuro", "spec.md")); err != nil {
		t.Fatalf("default_scope feature not materialized: %v", err)
	}
	if _, err := os.Stat(filepath.Join(docsDir, "current", "features", "F-0001-parcelamento")); err == nil {
		t.Fatalf("wipe semantics: previous scope's feature still materialized")
	}
}
