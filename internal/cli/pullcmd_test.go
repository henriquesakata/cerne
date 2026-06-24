package cli

import (
	"errors"
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
	// The manifest is a gitignored sidecar now, not part of the committed lock.
	manifest, err := project.LoadManifest(projDir)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if len(manifest) == 0 {
		t.Fatalf("manifest sidecar empty, want manifest of written files")
	}
	for _, rel := range []string{"CONTEXT.md", "current/features/F-0001-parcelamento/spec.md"} {
		if _, ok := manifest[rel]; !ok {
			t.Fatalf("manifest missing %s: %v", rel, manifest)
		}
	}
	if raw, rerr := os.ReadFile(filepath.Join(projDir, "docs.lock")); rerr != nil {
		t.Fatalf("read docs.lock: %v", rerr)
	} else if strings.Contains(string(raw), "materialized") {
		t.Fatalf("docs.lock must not embed the manifest after pull: %s", raw)
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

	// pull is the index sync point: index.db exists next to repo/.
	if _, err := os.Stat(indexPathFor(repoDir)); err != nil {
		t.Fatalf("index.db missing after pull: %v", err)
	}
}

func TestPullRebuildsIndex(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	id := newFeature(t, projDir, "Parcelamento", 1)
	writeDoc(t, repoDir, id, "spec.md", realDoc(id, "Dividir compras em parcelas."))
	if _, err := gitback.CommitAndPush(repoDir, "spec F-0001"); err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}
	if out, err := execCerne(t, "pull", "--dir", projDir); err != nil {
		t.Fatalf("first pull: %v\noutput: %s", err, out)
	}

	// New content after the first pull: the next pull must refresh the index.
	writeDoc(t, repoDir, id, "plan.md", realDoc(id, "Cronograma faseado de entrega."))
	if _, err := gitback.CommitAndPush(repoDir, "plan F-0001"); err != nil {
		t.Fatalf("CommitAndPush plan: %v", err)
	}
	if out, err := execCerne(t, "pull", "--dir", projDir); err != nil {
		t.Fatalf("second pull: %v\noutput: %s", err, out)
	}

	out, err := execCerne(t, "search", "cronograma", "--dir", projDir)
	if err != nil {
		t.Fatalf("search: %v\noutput: %s", err, out)
	}
	if strings.Contains(out, "Construindo índice...") {
		t.Fatalf("search rebuilt the index, want pull to have done it:\n%s", out)
	}
	if !strings.Contains(out, "F-0001 [feature/plan] candidate — Parcelamento") {
		t.Fatalf("missing plan hit indexed by pull:\n%s", out)
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

// TestPullOKF pins the opt-in: --okf emits OKF frontmatter (and a feature
// index.md), the default stays off (verbatim), .cernerc okf=true makes it the
// default, and an explicit --okf=false overrides that config default.
func TestPullOKF(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	id := newFeature(t, projDir, "Parcelamento", 1)
	writeDoc(t, repoDir, id, "spec.md", realDoc(id, "Dividir compras em parcelas."))
	if _, err := gitback.CommitAndPush(repoDir, "spec F-0001"); err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}
	read := func(path string) string {
		t.Helper()
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		return string(b)
	}
	featDir := filepath.Join(projDir, ".docs", "current", "features", "F-0001-parcelamento")
	specPath := filepath.Join(featDir, "spec.md")
	indexPath := filepath.Join(featDir, "index.md")

	// 1) --okf emits OKF frontmatter and a feature index.md.
	if out, err := execCerne(t, "pull", "--okf", "--dir", projDir); err != nil {
		t.Fatalf("pull --okf: %v\noutput: %s", err, out)
	}
	if spec := read(specPath); !strings.HasPrefix(spec, "---\ntype: spec\n") {
		t.Fatalf("pull --okf did not add type: spec frontmatter:\n%s", spec)
	}
	if idx := read(indexPath); !strings.Contains(idx, "type: feature") {
		t.Fatalf("pull --okf did not emit a feature index.md:\n%s", idx)
	}

	// 2) Default (no flag, no config) is off: verbatim spec, no index.md.
	if out, err := execCerne(t, "pull", "--dir", projDir); err != nil {
		t.Fatalf("pull: %v\noutput: %s", err, out)
	}
	if spec := read(specPath); strings.Contains(spec, "type: spec") {
		t.Fatalf("default pull must not emit OKF frontmatter:\n%s", spec)
	}
	if _, err := os.Stat(indexPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("default pull must not emit index.md (stat err = %v)", err)
	}

	// 3) .cernerc okf=true makes OKF the default.
	cfg, _, err := project.LoadConfig(projDir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	cfg.OKF = true
	if err := project.SaveConfig(projDir, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	if out, err := execCerne(t, "pull", "--dir", projDir); err != nil {
		t.Fatalf("pull with okf config: %v\noutput: %s", err, out)
	}
	if spec := read(specPath); !strings.Contains(spec, "type: spec") {
		t.Fatalf(".cernerc okf=true should emit frontmatter by default:\n%s", spec)
	}

	// 4) Explicit --okf=false overrides the config default.
	if out, err := execCerne(t, "pull", "--okf=false", "--dir", projDir); err != nil {
		t.Fatalf("pull --okf=false: %v\noutput: %s", err, out)
	}
	if spec := read(specPath); strings.Contains(spec, "type: spec") {
		t.Fatalf("--okf=false must override the config default:\n%s", spec)
	}
}
