package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/lint"
	"gitlab.com/rockt/cerne/internal/project"
	"gitlab.com/rockt/cerne/internal/store"
)

func TestPullScopeImplementedExplainsEmpty(t *testing.T) {
	projDir, _, repoDir := setupProject(t, "finance")
	now := time.Now().UTC()
	// An implemented feature in module finance: pull --scope finance materializes 0.
	if err := store.SaveFeature(repoDir, store.Feature{ID: "F-0001", Title: "Boletos", Module: "finance", State: domain.StateImplemented, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}
	writeBackendDoc(t, repoDir, "content/features/F-0001/spec.md", realDoc("F-0001", "Boletos entregue."))
	if _, err := gitback.CommitAndPush(repoDir, "seed implemented"); err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}

	out, err := execCerne(t, "pull", "--scope", "finance", "--dir", projDir)
	if err != nil {
		t.Fatalf("pull --scope: %v\n%s", err, out)
	}
	for _, want := range []string{"don't materialize content", "cerne show", "cerne feature errata"} {
		if !strings.Contains(out, want) {
			t.Errorf("pull --scope no-op output missing %q:\n%s", want, out)
		}
	}
}

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

	want := "Materialized: 1 feature(s), 0 decision(s), 0 reference(s) into .docs/ (snapshot " + originHead[:8] + ")."
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
	if out, err := execCerne(t, "feature", "new", "Parcelamento", "--context", "ctx de teste", "--module", "finance", "--dir", projDir); err != nil {
		t.Fatalf("feature new finance: %v\noutput: %s", err, out)
	}
	if out, err := execCerne(t, "feature", "new", "Tema escuro", "--context", "ctx de teste", "--module", "core", "--dir", projDir); err != nil {
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
	if !strings.Contains(out, "Materialized: 1 feature(s)") {
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

// pulledFeatureProject sets up a project with F-0001 (spec=body) pulled into
// .docs/, returning the project, origin and the materialized spec path.
func pulledFeatureProject(t *testing.T, body string) (projDir, origin, repoDir, specPath string) {
	t.Helper()
	projDir, origin, repoDir = setupProject(t)
	id := newFeature(t, projDir, "Parcelamento", 1)
	writeDoc(t, repoDir, id, "spec.md", realDoc(id, body))
	if _, err := gitback.CommitAndPush(repoDir, "spec F-0001"); err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}
	if out, err := execCerne(t, "pull", "--dir", projDir); err != nil {
		t.Fatalf("pull: %v\noutput: %s", err, out)
	}
	specPath = filepath.Join(projDir, ".docs", "current", "features", "F-0001-parcelamento", "spec.md")
	return projDir, origin, repoDir, specPath
}

// C2: pull 3-way merges a disjoint remote advance into .docs/, keeping the
// unsent local edit; the next push then sends that edit.
func TestPullMergesUnpushedEditDisjoint(t *testing.T) {
	projDir, origin, _, specPath := pulledFeatureProject(t, "Conteúdo original.")

	mine := "---\ncerne_id: F-0001\n---\n\n# Parcelamento\n\nEdição local não-enviada.\n"
	mustWrite(t, specPath, mine)

	// Another machine advances a DISJOINT document.
	clone2 := filepath.Join(t.TempDir(), "clone2")
	if err := gitback.Clone(origin, clone2); err != nil {
		t.Fatalf("Clone clone2: %v", err)
	}
	theirPlan := realDoc("F-0001", "Plano remoto disjunto.")
	mustWrite(t, filepath.Join(clone2, "content", "features", "F-0001", "plan.md"), theirPlan)
	if _, err := gitback.CommitAndPush(clone2, "remote disjoint plan"); err != nil {
		t.Fatalf("CommitAndPush clone2: %v", err)
	}

	// pull merges cleanly: no error, my edit kept, the remote plan brought in.
	if out, err := execCerne(t, "pull", "--dir", projDir); err != nil {
		t.Fatalf("disjoint pull should merge, got: %v\n%s", err, out)
	}
	if got, _ := os.ReadFile(specPath); string(got) != mine {
		t.Fatalf("pull discarded my unsent edit:\n%s", got)
	}
	planPath := filepath.Join(projDir, ".docs", "current", "features", "F-0001-parcelamento", "plan.md")
	if got, err := os.ReadFile(planPath); err != nil || string(got) != theirPlan {
		t.Fatalf("remote disjoint plan not merged in: %q err=%v", got, err)
	}

	// The kept edit is still unsent: the next push sends it.
	out, err := execCerne(t, "push", "--dir", projDir)
	if err != nil {
		t.Fatalf("push after merge: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Sent: 1 document(s)") {
		t.Fatalf("push should send the merged-kept edit:\n%s", out)
	}
}

// C3: a same-document change on both sides keeps MINE in .docs/, stashes THEIRS
// under .docs/.conflicts/, reports the conflict, and never silently discards.
func TestPullSameDocConflictNonDestructive(t *testing.T) {
	projDir, origin, _, specPath := pulledFeatureProject(t, "Conteúdo original.")

	mine := "---\ncerne_id: F-0001\n---\n\n# Parcelamento\n\nMinha edição não-enviada.\n"
	mustWrite(t, specPath, mine)

	// Another machine changes the SAME document.
	clone2 := filepath.Join(t.TempDir(), "clone2")
	if err := gitback.Clone(origin, clone2); err != nil {
		t.Fatalf("Clone clone2: %v", err)
	}
	theirSpec := realDoc("F-0001", "Versão remota do mesmo documento.")
	mustWrite(t, filepath.Join(clone2, "content", "features", "F-0001", "spec.md"), theirSpec)
	if _, err := gitback.CommitAndPush(clone2, "remote same-doc edit"); err != nil {
		t.Fatalf("CommitAndPush clone2: %v", err)
	}

	out, err := execCerne(t, "pull", "--dir", projDir)
	if err == nil {
		t.Fatalf("same-doc pull should report a conflict\n%s", out)
	}
	if !strings.Contains(out, "Conflict in 1 document(s)") || !strings.Contains(out, ".conflicts") {
		t.Fatalf("conflict output missing details:\n%s", out)
	}
	// Mine kept in place.
	if got, _ := os.ReadFile(specPath); string(got) != mine {
		t.Fatalf("conflict must keep my version in .docs/:\n%s", got)
	}
	// Theirs stashed for resolution.
	stash := filepath.Join(projDir, ".docs", ".conflicts", "current", "features", "F-0001-parcelamento", "spec.md")
	if got, err := os.ReadFile(stash); err != nil || string(got) != theirSpec {
		t.Fatalf("remote version not stashed under .conflicts: %q err=%v", got, err)
	}
}

// --force keeps its meaning: discard unsent edits and restore from the backend.
func TestPullForceDiscardsUnpushedEdits(t *testing.T) {
	projDir, _, _, specPath := pulledFeatureProject(t, "Conteúdo original.")
	mustWrite(t, specPath, "---\ncerne_id: F-0001\n---\n\n# Parcelamento\n\nEdição local não-enviada.\n")

	if out, err := execCerne(t, "pull", "--force", "--dir", projDir); err != nil {
		t.Fatalf("pull --force: %v\noutput: %s", err, out)
	}
	if got, _ := os.ReadFile(specPath); strings.Contains(string(got), "Edição local não-enviada") {
		t.Fatalf("pull --force should restore from backend; edit still present:\n%s", got)
	}
}

// gitProjectOnBranch turns projDir into a git repo with .cernerc/docs.lock
// committed and the given branch checked out (F-0102 pin_branch fixtures).
func gitProjectOnBranch(t *testing.T, projDir, branch string) {
	t.Helper()
	repo, err := git.PlainInit(projDir, false)
	if err != nil {
		t.Fatalf("PlainInit proj: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projDir, ".gitignore"), []byte(".docs/\n"), 0o644); err != nil {
		t.Fatalf("write .gitignore: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{".cernerc", ".gitignore", "docs.lock"} {
		if _, err := wt.Add(f); err != nil {
			t.Fatalf("add %s: %v", f, err)
		}
	}
	sig := &object.Signature{Name: "t", Email: "t@t", When: time.Now()}
	if _, err := wt.Commit("init proj", &git.CommitOptions{Author: sig, Committer: sig}); err != nil {
		t.Fatalf("commit init proj: %v", err)
	}
	if err := wt.Checkout(&git.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName(branch),
		Create: true,
	}); err != nil {
		t.Fatalf("checkout -b %s: %v", branch, err)
	}
}

// setPinBranch rewrites the project .cernerc with pin_branch set (F-0102).
func setPinBranch(t *testing.T, projDir, branch string) {
	t.Helper()
	cfg, _, err := project.LoadConfig(projDir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	cfg.PinBranch = branch
	if err := project.SaveConfig(projDir, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
}

// Criteria 2+4 (F-0102): pull off the pin branch materializes and records the
// session snapshot but leaves the committed docs.lock byte-identical.
func TestPullPinBranchLeavesLockUntouched(t *testing.T) {
	projDir, origin, repoDir := setupProject(t)
	id := newFeature(t, projDir, "Parcelamento", 1)
	writeDoc(t, repoDir, id, "spec.md", realDoc(id, "Dividir compras em parcelas."))
	if _, err := gitback.CommitAndPush(repoDir, "spec F-0001"); err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}
	gitProjectOnBranch(t, projDir, "feature/x")
	setPinBranch(t, projDir, "main")

	before, err := os.ReadFile(filepath.Join(projDir, "docs.lock"))
	if err != nil {
		t.Fatalf("read docs.lock before: %v", err)
	}
	if out, err := execCerne(t, "pull", "--dir", projDir); err != nil {
		t.Fatalf("pull: %v\noutput: %s", err, out)
	}
	after, err := os.ReadFile(filepath.Join(projDir, "docs.lock"))
	if err != nil {
		t.Fatalf("read docs.lock after: %v", err)
	}
	if string(before) != string(after) {
		t.Fatalf("docs.lock rewritten off the pin branch:\nbefore: %s\nafter: %s", before, after)
	}
	originHead, err := gitback.HeadSHA(origin)
	if err != nil {
		t.Fatalf("HeadSHA origin: %v", err)
	}
	snap, err := project.LoadSessionSnapshot(projDir)
	if err != nil {
		t.Fatalf("LoadSessionSnapshot: %v", err)
	}
	if snap != originHead {
		t.Fatalf("session snapshot = %q, want origin HEAD %q", snap, originHead)
	}
	// Materialization itself is unaffected.
	if _, err := os.Stat(filepath.Join(projDir, ".docs", "current", "features", "F-0001-parcelamento", "spec.md")); err != nil {
		t.Fatalf("spec not materialized: %v", err)
	}
}

// Criterion 3 (F-0102): pull ON the pin branch keeps advancing docs.lock.
func TestPullPinBranchOnPinAdvancesLock(t *testing.T) {
	projDir, origin, repoDir := setupProject(t)
	id := newFeature(t, projDir, "Parcelamento", 1)
	writeDoc(t, repoDir, id, "spec.md", realDoc(id, "Dividir compras em parcelas."))
	if _, err := gitback.CommitAndPush(repoDir, "spec F-0001"); err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}
	gitProjectOnBranch(t, projDir, "trunk")
	setPinBranch(t, projDir, "trunk")

	if out, err := execCerne(t, "pull", "--dir", projDir); err != nil {
		t.Fatalf("pull: %v\noutput: %s", err, out)
	}
	originHead, err := gitback.HeadSHA(origin)
	if err != nil {
		t.Fatalf("HeadSHA origin: %v", err)
	}
	lock, err := project.LoadLock(projDir)
	if err != nil {
		t.Fatalf("LoadLock: %v", err)
	}
	if lock.Snapshot != originHead {
		t.Fatalf("lock.Snapshot = %q, want origin HEAD %q on the pin branch", lock.Snapshot, originHead)
	}
}

func TestPullCIWriterLeavesLockUntouchedOnMain(t *testing.T) {
	projDir, origin, repoDir := setupProject(t)
	id := newFeature(t, projDir, "Parcelamento", 1)
	writeDoc(t, repoDir, id, "spec.md", realDoc(id, "Dividir compras em parcelas."))
	if _, err := gitback.CommitAndPush(repoDir, "spec F-0001"); err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}
	gitProjectOnBranch(t, projDir, "main")
	cfg, _, err := project.LoadConfig(projDir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	cfg.PinBranch = "main"
	cfg.PinWriter = "ci"
	if err := project.SaveConfig(projDir, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	before, err := os.ReadFile(filepath.Join(projDir, "docs.lock"))
	if err != nil {
		t.Fatalf("read docs.lock before: %v", err)
	}
	if out, err := execCerne(t, "pull", "--dir", projDir); err != nil {
		t.Fatalf("pull: %v\noutput: %s", err, out)
	}
	after, err := os.ReadFile(filepath.Join(projDir, "docs.lock"))
	if err != nil {
		t.Fatalf("read docs.lock after: %v", err)
	}
	if string(before) != string(after) {
		t.Fatalf("docs.lock rewritten by pull in CI writer mode:\nbefore: %s\nafter: %s", before, after)
	}
	originHead, err := gitback.HeadSHA(origin)
	if err != nil {
		t.Fatalf("HeadSHA origin: %v", err)
	}
	snap, err := project.LoadSessionSnapshot(projDir)
	if err != nil {
		t.Fatalf("LoadSessionSnapshot: %v", err)
	}
	if snap != originHead {
		t.Fatalf("session snapshot = %q, want origin HEAD %q", snap, originHead)
	}
}

// markedNoteProject sets up a project with one golden-path note and returns the
// generated-skill path a sync would produce for it.
func markedNoteProject(t *testing.T) (projDir, learnedSkill string) {
	t.Helper()
	projDir, _, _ = setupProject(t)
	if out, err := execCerne(t, "note", "new", "Deploy gotcha", "--component", "golden-path", "--dir", projDir); err != nil {
		t.Fatalf("note new: %v\noutput: %s", err, out)
	}
	return projDir, filepath.Join(projDir, ".claude", "skills", "learned-n-0001-deploy-gotcha", "SKILL.md")
}

// F-0178 criterion 1: an explicit --sync-skills reconciles learned-* as part
// of the pull and reports it in the output.
func TestPullSyncSkillsFlag(t *testing.T) {
	projDir, learnedSkill := markedNoteProject(t)

	out, err := execCerne(t, "pull", "--sync-skills", "--dir", projDir)
	if err != nil {
		t.Fatalf("pull --sync-skills: %v\noutput: %s", err, out)
	}
	if _, err := os.Stat(learnedSkill); err != nil {
		t.Fatalf("generated skill missing after pull: %v", err)
	}
	if !strings.Contains(out, "learned-n-0001-deploy-gotcha: created") {
		t.Fatalf("pull output missing sync line:\n%s", out)
	}
}

// F-0178 criterion 2: sync_skills = true in .cernerc makes the reconcile the
// pull's default, no flag needed.
func TestPullSyncSkillsConfig(t *testing.T) {
	projDir, learnedSkill := markedNoteProject(t)
	cfg, _, err := project.LoadConfig(projDir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	cfg.SyncSkills = true
	if err := project.SaveConfig(projDir, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	if out, err := execCerne(t, "pull", "--dir", projDir); err != nil {
		t.Fatalf("pull: %v\noutput: %s", err, out)
	}
	if _, err := os.Stat(learnedSkill); err != nil {
		t.Fatalf("generated skill missing after config-driven pull: %v", err)
	}
}

// F-0178 criterion 4: without the flag and without config, pull never touches
// the learned-* namespace.
func TestPullSyncSkillsDefaultOff(t *testing.T) {
	projDir, learnedSkill := markedNoteProject(t)

	if out, err := execCerne(t, "pull", "--dir", projDir); err != nil {
		t.Fatalf("pull: %v\noutput: %s", err, out)
	}
	if _, err := os.Stat(filepath.Dir(learnedSkill)); !os.IsNotExist(err) {
		t.Fatalf("learned skill generated without opt-in (err=%v)", err)
	}
}

// F-0178 criterion 3: a failing skill sync is a warning, not a pull failure —
// the materialization is the primary outcome.
func TestPullSyncSkillsFailOpen(t *testing.T) {
	projDir, _ := markedNoteProject(t)
	// Injection: .claude/skills as a FILE (replacing the dir init created) makes
	// the reconcile's MkdirAll fail.
	skillsDir := filepath.Join(projDir, ".claude", "skills")
	if err := os.RemoveAll(skillsDir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skillsDir, []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := execCerne(t, "pull", "--sync-skills", "--dir", projDir)
	if err != nil {
		t.Fatalf("pull must stay green on sync failure, got: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "Materialized:") {
		t.Fatalf("pull output missing materialization summary:\n%s", out)
	}
}

// F-0307 crit. 6: the empty-scope hint and the errata confirmation name
// commands that exist, and do not send the reader to 'cerne pull' for an
// erratum the materializer never copies.
func TestPullAndErrataMessagesNameRealPaths(t *testing.T) {
	projDir, _, repoDir := setupProject(t, "finance")
	now := time.Now().UTC()
	if err := store.SaveFeature(repoDir, store.Feature{ID: "F-0001", Title: "Boletos", Module: "finance", State: domain.StateImplemented, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	writeBackendDoc(t, repoDir, "content/features/F-0001/spec.md", realDoc("F-0001", "Boletos entregue."))
	if _, err := gitback.CommitAndPush(repoDir, "seed implemented"); err != nil {
		t.Fatal(err)
	}

	out, err := execCerne(t, "pull", "--scope", "finance", "--dir", projDir)
	if err != nil {
		t.Fatalf("pull --scope: %v\n%s", err, out)
	}
	_, errata := lint.Correction(domain.StateDone, "<id>", "result")
	if !strings.Contains(out, "'"+errata+"'") || !strings.Contains(out, "'cerne show <id>'") || strings.Contains(out, "'cerne errata") {
		t.Fatalf("pull hint should name %q and 'cerne show <id>', never 'cerne errata':\n%s", errata, out)
	}

	out, err = execCerne(t, "feature", "errata", "F-0001", "Correção.", "--dir", projDir)
	if err != nil {
		t.Fatalf("errata: %v\n%s", err, out)
	}
	if want := "erratum appended to F-0001 — already synced. Read it with 'cerne show F-0001'."; !strings.Contains(out, want) || strings.Contains(out, "cerne pull") {
		t.Fatalf("errata confirmation = %q, want %q and no 'cerne pull'", out, want)
	}
}
