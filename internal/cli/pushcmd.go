package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/project"
)

func newPushCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "push",
		Short: "Send locally edited .docs/ documents back to the backend",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPush(cmd, dir)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.Flags().MarkHidden("dir")
	return cmd
}

// pushedDoc is one changed materialized document mapped back to its
// backend destination.
type pushedDoc struct {
	rel        string // manifest key, slash-separated relative to .docs/
	backendRel string // destination inside the backend clone, slash-separated
	body       []byte
	sum        string // sha256 hex of body
}

func runPush(cmd *cobra.Command, dir string) error {
	ctx, err := resolvePaths(dir)
	if err != nil {
		return err
	}
	lock, err := project.LoadLock(ctx.projDir)
	if errors.Is(err, project.ErrNoLock) {
		return fmt.Errorf("nenhum docs.lock encontrado — rode 'cerne pull' primeiro")
	}
	if err != nil {
		return err
	}

	// Drift detection against the REMOTE head, before any sync: the
	// write-back is only safe on top of the exact snapshot the user pulled.
	remoteHead, err := gitback.RemoteHeadSHA(ctx.repoDir)
	if err != nil {
		return err
	}
	if remoteHead != lock.Snapshot {
		return fmt.Errorf(
			"backend avançou desde seu último pull (snapshot local %s, remoto %s) — rode 'cerne pull' antes do push",
			shortSHA(lock.Snapshot), shortSHA(remoteHead))
	}

	docsDir := filepath.Join(ctx.projDir, ".docs")
	manifest, err := project.LoadManifest(ctx.projDir)
	if err != nil {
		return err
	}
	if err := checkContextUntouched(docsDir, manifest); err != nil {
		return err
	}
	docs, err := collectChangedDocs(docsDir, manifest)
	if err != nil {
		return err
	}
	if len(docs) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "Nada para enviar.")
		return nil
	}

	for _, d := range docs {
		dst := filepath.Join(ctx.repoDir, filepath.FromSlash(d.backendRel))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dst, d.body, 0o644); err != nil {
			return err
		}
	}
	for _, d := range docs {
		manifest[d.rel] = d.sum
	}
	sha, err := gitback.CommitAndPush(ctx.repoDir, fmt.Sprintf("cerne: push %d documento(s)", len(docs)))
	if errors.Is(err, gitback.ErrNothingToCommit) {
		// The edited copies already match the backend byte for byte; just
		// realign the manifest hashes.
		if err := project.SaveManifest(ctx.projDir, manifest); err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), "Nada para enviar.")
		return nil
	}
	if err != nil {
		return err
	}
	lock.Snapshot = sha
	if err := project.SaveLock(ctx.projDir, lock); err != nil {
		return err
	}
	if err := project.SaveManifest(ctx.projDir, manifest); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Enviado: %d documento(s) (snapshot %s).\n", len(docs), shortSHA(sha))
	return nil
}

// checkContextUntouched errors when the generated CONTEXT.md was edited
// locally; a missing CONTEXT.md is ignored (pull restores it).
func checkContextUntouched(docsDir string, manifest map[string]string) error {
	want, tracked := manifest["CONTEXT.md"]
	if !tracked {
		return nil
	}
	body, err := os.ReadFile(filepath.Join(docsDir, "CONTEXT.md"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if sha256Hex(body) != want {
		return fmt.Errorf("CONTEXT.md é gerado — não edite (rode 'cerne pull' para restaurar)")
	}
	return nil
}

// collectChangedDocs walks .docs/current/** and returns every tracked *.md
// whose content no longer matches the docs.lock manifest, mapped back to its
// backend path via the cerne_id frontmatter. Files in the manifest but
// missing on disk are ignored (deletion is not a push operation).
func collectChangedDocs(docsDir string, manifest map[string]string) ([]pushedDoc, error) {
	currentDir := filepath.Join(docsDir, "current")
	if _, err := os.Stat(currentDir); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	var docs []pushedDoc
	err := filepath.WalkDir(currentDir, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}
		relOS, err := filepath.Rel(docsDir, p)
		if err != nil {
			return err
		}
		rel := filepath.ToSlash(relOS)
		body, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		want, tracked := manifest[rel]
		sum := sha256Hex(body)
		switch {
		case tracked && sum == want:
			return nil // tracked and unchanged
		case !tracked && !acceptableNewDoc(rel, manifest):
			return fmt.Errorf("arquivo não rastreado: %s — rode 'cerne pull' para ressincronizar", rel)
		}
		id := parseCerneID(body)
		if id == "" {
			return fmt.Errorf("documento sem cerne_id no frontmatter: %s — novos documentos nascem com 'cerne feature new'", rel)
		}
		backendRel, err := backendRelPath(rel, id)
		if err != nil {
			return err
		}
		docs = append(docs, pushedDoc{rel: rel, backendRel: backendRel, body: body, sum: sum})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return docs, nil
}

// acceptableNewDoc reports whether rel is a net-new plan.md/result.md authored
// alongside an already-tracked feature — a sibling under the same feature folder
// is in the manifest. This lets the cerne-native flow push a plan/result after
// the feature-new spec stub, without accepting arbitrary untracked files.
func acceptableNewDoc(rel string, manifest map[string]string) bool {
	if !strings.HasPrefix(rel, "current/features/") {
		return false
	}
	switch path.Base(rel) {
	case "plan.md", "result.md":
	default:
		return false
	}
	folder := path.Dir(rel) + "/"
	for k := range manifest {
		if strings.HasPrefix(k, folder) {
			return true
		}
	}
	return false
}

// backendRelPath maps a materialized doc path back to its backend content
// path: feature docs keep their filename under content/features/<ID>/;
// decisions, runbooks and references collapse to content/<kind>/<ID>.md.
func backendRelPath(rel, id string) (string, error) {
	switch {
	case strings.HasPrefix(rel, "current/features/"):
		return "content/features/" + id + "/" + path.Base(rel), nil
	case strings.HasPrefix(rel, "current/decisions/"):
		return "content/decisions/" + id + ".md", nil
	case strings.HasPrefix(rel, "current/runbooks/"):
		return "content/runbooks/" + id + ".md", nil
	case strings.HasPrefix(rel, "current/references/"):
		return "content/references/" + id + ".md", nil
	}
	return "", fmt.Errorf("arquivo não rastreado: %s — rode 'cerne pull' para ressincronizar", rel)
}

// parseCerneID extracts the cerne_id value from the leading frontmatter
// block ("---" delimited); empty when the block or the key is absent.
func parseCerneID(data []byte) string {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	lines := strings.Split(string(data), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return ""
	}
	for _, line := range lines[1:] {
		trimmed := strings.TrimSpace(line)
		if trimmed == "---" {
			return ""
		}
		if v, ok := strings.CutPrefix(trimmed, "cerne_id:"); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// sha256Hex returns the sha256 hex digest of body, matching the manifest
// hashes written by materialize.
func sha256Hex(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// shortSHA returns the first 8 chars of sha (sha itself when shorter).
func shortSHA(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}
