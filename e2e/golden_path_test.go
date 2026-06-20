// Package e2e_test drives the cerne CLI end to end through its public
// surface only: the cobra root command (cli.Root()) plus filesystem and
// fresh-clone assertions. No store internals are touched — the backend is
// always verified the way a second machine would see it.
package e2e_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/cli"
	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/project"
)

// cerne executes the root command with args, returning combined output.
func cerne(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := cli.Root()
	out := &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

// mustCerne executes the root command and fails the test on error.
func mustCerne(t *testing.T, args ...string) string {
	t.Helper()
	out, err := cerne(t, args...)
	if err != nil {
		t.Fatalf("cerne %s: %v\noutput: %s", strings.Join(args, " "), err, out)
	}
	return out
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(body)
}

// assertLockParity asserts docs.lock Snapshot == origin HEAD at a stage.
func assertLockParity(t *testing.T, projDir, origin, stage string) {
	t.Helper()
	lock, err := project.LoadLock(projDir)
	if err != nil {
		t.Fatalf("[%s] LoadLock: %v", stage, err)
	}
	head, err := gitback.HeadSHA(origin)
	if err != nil {
		t.Fatalf("[%s] HeadSHA origin: %v", stage, err)
	}
	if lock.Snapshot != head {
		t.Fatalf("[%s] docs.lock snapshot = %s, want origin HEAD %s", stage, lock.Snapshot, head)
	}
}

// TestGoldenPath walks the full product loop in one shared temp world:
// init → feature new → pull → edit spec in .docs/ → push → approve →
// pull → substituir → pull, asserting the .docs/ projection, the backend
// (via fresh clones of origin) and docs.lock parity along the way.
func TestGoldenPath(t *testing.T) {
	t.Setenv("CERNE_CACHE_DIR", t.TempDir())
	origin := filepath.Join(t.TempDir(), "origin.git")
	if err := gitback.InitBare(origin); err != nil {
		t.Fatalf("InitBare: %v", err)
	}
	proj := t.TempDir()
	docsDir := filepath.Join(proj, ".docs")
	ctxPath := filepath.Join(docsDir, "CONTEXT.md")
	specDir := filepath.Join(docsDir, "current", "features", "F-0001-carrinho-persistente")
	specPath := filepath.Join(specDir, "spec.md")

	t.Log("stage 1: init — project skeleton + backend layout pushed to origin")
	mustCerne(t, "init", "--name", "Demo Loja", "--backend", origin,
		"--module", "catalog", "--module", "checkout", "--dir", proj)
	for _, rel := range []string{".cernerc", "docs.lock", filepath.Join(".docs", ".gitkeep")} {
		if _, err := os.Stat(filepath.Join(proj, rel)); err != nil {
			t.Fatalf("init artifact %s: %v", rel, err)
		}
	}
	assertLockParity(t, proj, origin, "init")

	t.Log("stage 2: feature new — F-0001 born candidate")
	out := mustCerne(t, "feature", "new", "Carrinho persistente", "--module", "checkout", "--dir", proj)
	if !strings.Contains(out, "F-0001 criada (candidate)") {
		t.Fatalf("feature new output missing F-0001 confirmation:\n%s", out)
	}
	assertLockParity(t, proj, origin, "feature new")

	t.Log("stage 3: pull — CONTEXT.md lists the candidate, spec stub materialized")
	mustCerne(t, "pull", "--dir", proj)
	ctx := mustRead(t, ctxPath)
	if !strings.Contains(ctx, "| F-0001 | candidate |  | checkout | Carrinho persistente |") {
		t.Fatalf("CONTEXT.md missing candidate row for F-0001:\n%s", ctx)
	}
	stub := mustRead(t, specPath)
	if !strings.Contains(stub, "## Problema") || !strings.Contains(stub, "[ESCLARECER:") {
		t.Fatalf("materialized spec is not the scaffolded template stub:\n%s", stub)
	}
	assertLockParity(t, proj, origin, "pull")

	t.Log("stage 4: write the real spec in .docs/ and push it back to origin")
	const frontmatter = "---\ncerne_id: F-0001\n---\n"
	if !strings.HasPrefix(stub, frontmatter) {
		t.Fatalf("spec stub no longer starts with the expected frontmatter:\n%s", stub)
	}
	realSpec := frontmatter +
		"\n# Carrinho persistente\n\nO carrinho sobrevive a refresh e novo login.\n\n" +
		"## Critérios de aceite\n\n- Itens persistem por 30 dias.\n"
	if err := os.WriteFile(specPath, []byte(realSpec), 0o644); err != nil {
		t.Fatalf("write real spec: %v", err)
	}
	out = mustCerne(t, "push", "--dir", proj)
	if !strings.Contains(out, "Enviado: 1 documento(s)") {
		t.Fatalf("push output missing send summary:\n%s", out)
	}
	verify := filepath.Join(t.TempDir(), "verify")
	if err := gitback.Clone(origin, verify); err != nil {
		t.Fatalf("Clone origin for verification: %v", err)
	}
	pushed := mustRead(t, filepath.Join(verify, "content", "features", "F-0001", "spec.md"))
	if pushed != realSpec {
		t.Fatalf("origin spec = %q, want the pushed content verbatim", pushed)
	}
	assertLockParity(t, proj, origin, "push")

	t.Log("stage 5: approve — gate satisfied by the pushed spec")
	out = mustCerne(t, "feature", "approve", "F-0001", "--dir", proj)
	if !strings.Contains(out, "candidate") || !strings.Contains(out, "approved") {
		t.Fatalf("approve output missing old→new state:\n%s", out)
	}
	out = mustCerne(t, "status", "--dir", proj)
	if !strings.Contains(out, "APROVADAS (1)\n  F-0001  checkout  Carrinho persistente\n") {
		t.Fatalf("status missing F-0001 under APROVADAS:\n%s", out)
	}
	assertLockParity(t, proj, origin, "approve")

	t.Log("stage 6: pull again — CONTEXT.md row moved to approved")
	mustCerne(t, "pull", "--dir", proj)
	ctx = mustRead(t, ctxPath)
	if !strings.Contains(ctx, "| F-0001 | approved |  | checkout | Carrinho persistente |") {
		t.Fatalf("CONTEXT.md row not approved after pull:\n%s", ctx)
	}

	t.Log("stage 7: substituir F-0001 by F-0002 — gone from .docs/, kept in history")
	out = mustCerne(t, "feature", "new", "Carrinho v2", "--module", "checkout", "--dir", proj)
	if !strings.Contains(out, "F-0002 criada (candidate)") {
		t.Fatalf("second feature new output missing F-0002 confirmation:\n%s", out)
	}
	mustCerne(t, "feature", "substituir", "F-0001", "--by", "F-0002", "--reason", "redesign", "--dir", proj)
	mustCerne(t, "pull", "--dir", proj)
	if _, err := os.Stat(specDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("superseded feature dir still materialized (stat err = %v)", err)
	}
	ctx = mustRead(t, ctxPath)
	if strings.Contains(ctx, "F-0001") {
		t.Fatalf("CONTEXT.md still mentions superseded F-0001:\n%s", ctx)
	}
	if !strings.Contains(ctx, "| F-0002 | candidate |  | checkout | Carrinho v2 |") {
		t.Fatalf("CONTEXT.md missing successor F-0002:\n%s", ctx)
	}
	out = mustCerne(t, "status", "--all", "--dir", proj)
	if !strings.Contains(out, "  F-0001  checkout  Carrinho persistente → F-0002\n") {
		t.Fatalf("status --all missing superseded F-0001 → F-0002:\n%s", out)
	}

	t.Log("stage 8: lock parity — docs.lock snapshot == origin HEAD at the end")
	assertLockParity(t, proj, origin, "final")
}
