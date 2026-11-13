package cli

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

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
	edits := stagedEdits{files: map[string]bool{}, entities: map[string]bool{}}
	for _, d := range docs {
		edits.files[d.backendRel] = true
		edits.entities[editEntity(d.backendRel)] = true
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

// stagedEdits is what the next push would send (F-0382): the backend paths of
// the edited documents and the ids of the entities they belong to.
type stagedEdits struct {
	files    map[string]bool
	entities map[string]bool
}

// editEntity is the entity id of a backend content path:
// content/features/F-1/spec.md -> F-1, content/notes/N-2.md -> N-2.
func editEntity(backendRel string) string {
	parts := strings.Split(backendRel, "/")
	if len(parts) >= 4 && parts[1] == "features" {
		return parts[2]
	}
	return strings.TrimSuffix(parts[len(parts)-1], ".md")
}

// classifyStaged splits findings into those born from the local edits and the
// rest. A finding is the edit's when its File is an edited document (a broken
// link in an edited doc counts wherever it points) or its Entity owns an edited
// document (whole-files: editing a spec owns the plan-older-than-spec on its
// plan, and an old finding on that same entity counts too).
func classifyStaged(findings []lint.Finding, e stagedEdits) (mine, other []lint.Finding) {
	for _, f := range findings {
		if (f.File != "" && e.files[f.File]) || (f.Entity != "" && e.entities[f.Entity]) {
			mine = append(mine, f)
		} else {
			other = append(other, f)
		}
	}
	return mine, other
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
