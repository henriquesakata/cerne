package cli

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/henriquesakata/cerne/internal/lint"
	"github.com/henriquesakata/cerne/internal/project"
)

// stagedLintRoot assembles a backend-shaped tree for linting local edits without
// touching the backend: it copies the cache clone's entities/ and content/ (the
// pulled snapshot — entity STATE plus markdown) into a temp dir, then overlays
// the staged .docs/ edits onto their content/ destinations (via the same
// collectChangedDocs→backendRel mapping push uses). lint.Run runs over this tree
// UNCHANGED, so the validation modes share the backend's single source of rules.
// The returned cleanup removes the temp tree. No .git, no git calls, no writes to
// the cache clone — the validation never mutates the backend.
func stagedLintRoot(repoDir, projDir string) (string, stagedEdits, func(), error) {
	root, err := os.MkdirTemp("", "cerne-stagedlint-")
	if err != nil {
		return "", stagedEdits{}, nil, err
	}
	cleanup := func() { _ = os.RemoveAll(root) }
	for _, sub := range []string{"entities", "content"} {
		if err := copyTree(filepath.Join(repoDir, sub), filepath.Join(root, sub)); err != nil {
			cleanup()
			return "", stagedEdits{}, nil, err
		}
	}
	docsDir := filepath.Join(projDir, ".docs")
	manifest, err := project.LoadManifest(projDir)
	if err != nil {
		cleanup()
		return "", stagedEdits{}, nil, err
	}
	docs, err := collectChangedDocs(repoDir, docsDir, manifest)
	if err != nil {
		cleanup()
		return "", stagedEdits{}, nil, err
	}
	// project.json carries the docs language: without it the staged tree reads
	// as pt-BR whatever the project is.
	if data, err := os.ReadFile(filepath.Join(repoDir, "project.json")); err == nil {
		if err := os.WriteFile(filepath.Join(root, "project.json"), data, 0o644); err != nil {
			cleanup()
			return "", stagedEdits{}, nil, err
		}
	}
	edits := stagedEdits{count: len(docs), base: repoDir}
	for _, d := range docs {
		dst := filepath.Join(root, filepath.FromSlash(d.backendRel))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			cleanup()
			return "", stagedEdits{}, nil, err
		}
		if err := os.WriteFile(dst, d.body, 0o644); err != nil {
			cleanup()
			return "", stagedEdits{}, nil, err
		}
	}
	return root, edits, cleanup, nil
}

// stagedEdits describes the local edits (F-0382): how many documents differ
// from the manifest (the push edit set) and the pulled snapshot they are
// compared against.
type stagedEdits struct {
	count int
	base  string
}

// findingKey identifies a finding across the two lint runs: the line is left
// out because an edit shifts it.
func findingKey(f lint.Finding) string {
	return f.Rule + "\x00" + f.Entity + "\x00" + f.File + "\x00" + f.Detail
}

// diffFindings compares the staged lint with the base lint (the snapshot
// without the edits): added are the edit's findings, kept the old debt, and
// fixed counts the base findings the edit cleared.
func diffFindings(staged, base []lint.Finding) (added, kept []lint.Finding, fixed int) {
	left := map[string]int{}
	for _, f := range base {
		left[findingKey(f)]++
	}
	for _, f := range staged {
		if k := findingKey(f); left[k] > 0 {
			left[k]--
			kept = append(kept, f)
		} else {
			added = append(added, f)
		}
	}
	for _, n := range left {
		fixed += n
	}
	return added, kept, fixed
}

// copyTree recursively copies the directory src into dst. A missing src is a
// no-op (a snapshot may lack a subdir, e.g. no content/ yet).
func copyTree(src, dst string) error {
	info, err := os.Stat(src)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return nil
	}
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}
