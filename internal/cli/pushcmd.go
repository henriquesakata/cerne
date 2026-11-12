package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/lint"
	"github.com/henriquesakata/cerne/internal/llm"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
)

func newPushCmd() *cobra.Command {
	var dir string
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "push",
		Short: "Send locally edited .docs/ documents back to the backend",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPush(cmd, dir, dryRun)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.Flags().MarkHidden("dir")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "validate and show what would go to the backend, without committing or sending")
	return withFinalLine(cmd)
}

// pushedDoc is one changed materialized document mapped back to its
// backend destination.
type pushedDoc struct {
	rel        string // manifest key, slash-separated relative to .docs/
	backendRel string // destination inside the backend clone, slash-separated
	body       []byte
	sum        string // sha256 hex of body
}

func runPush(cmd *cobra.Command, dir string, dryRun bool) error {
	ctx, err := resolvePaths(dir)
	if err != nil {
		return err
	}
	// The reconcile base is the snapshot .docs/ was last synced to: the
	// session sidecar when present (with pin_branch set, the committed
	// docs.lock is stale on feature branches by design — F-0102), falling
	// back to docs.lock for a pre-0102 checkout.
	base, err := project.LoadSessionSnapshot(ctx.projDir)
	if errors.Is(err, project.ErrNoSessionSnapshot) {
		lock, lerr := project.LoadLock(ctx.projDir)
		if errors.Is(lerr, project.ErrNoLock) {
			return fmt.Errorf("no docs.lock found — run 'cerne pull' first")
		}
		if lerr != nil {
			return lerr
		}
		base = lock.Snapshot
	} else if err != nil {
		return err
	}

	docsDir := filepath.Join(ctx.projDir, ".docs")
	manifest, err := project.LoadManifest(ctx.projDir)
	if err != nil {
		return err
	}
	if err := checkContextUntouched(docsDir, manifest); err != nil {
		return err
	}
	endRead := startPhase(cmd.ErrOrStderr(), "reading changed documents")
	docs, err := collectChangedDocs(ctx.repoDir, docsDir, manifest)
	endRead(err == nil)
	if err != nil {
		return err
	}
	if len(docs) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "Nothing to send.")
		return nil
	}

	if dryRun {
		return reportDryRun(cmd, ctx, docs)
	}

	// Reconcile per document against the latest backend, starting from the
	// snapshot we pulled: a remote advance on a DIFFERENT document never blocks
	// this push (auto-rebase + retry); only the SAME document changed both ways
	// is a real conflict.
	changes := make([]app.FileChange, len(docs))
	for i, d := range docs {
		changes[i] = app.FileChange{Path: d.backendRel, Content: d.body}
	}
	staleBefore := pushedSpecStaleness(ctx.repoDir, docs)
	// The lock wait, the fetch and the push live in this one call and can each
	// take long: the phase beats on stderr so the wait never looks like a hang.
	endReconcile := startPhase(cmd.ErrOrStderr(), "reconciling with the backend")
	head, conflicts, remints, err := app.ReconcileCommitPushMutate(
		ctx.repoDir, ctx.projDir, base,
		fmt.Sprintf("cerne: push %d document(s)", len(docs)), changes,
		func() error { return app.OpenAutoWorkForPush(ctx.repoDir, ctx.projDir, changes) })
	endReconcile(err == nil && len(conflicts) == 0)
	if err != nil {
		return err
	}
	if len(conflicts) > 0 {
		return reportPushConflicts(cmd, conflicts)
	}
	for _, old := range slices.Sorted(maps.Keys(remints)) {
		fmt.Fprintf(cmd.ErrOrStderr(), "cerne: auto-work id %s was taken upstream; registered as %s\n", old, remints[old])
	}

	// The backend now matches our docs (applied or already present): realign the
	// manifest hashes. The snapshot pin was advanced inside the reconcile.
	for _, d := range docs {
		manifest[d.rel] = d.sum
	}
	if err := project.SaveManifest(ctx.projDir, manifest); err != nil {
		return err
	}
	if head == "" {
		fmt.Fprintln(cmd.OutOrStdout(), "Nothing to send.")
		return nil
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Sent: %d document(s) (snapshot %s).\n", len(docs), shortSHA(head))
	// A push that leaves an approved feature stale says so (F-0318): the
	// comparison is the one every surface reads, so a format-only push or a
	// revert to the approved text prints nothing.
	staleAfter := pushedSpecStaleness(ctx.repoDir, docs)
	for _, id := range slices.Sorted(maps.Keys(staleAfter)) {
		if staleAfter[id] && !staleBefore[id] {
			fmt.Fprintf(cmd.OutOrStdout(), "%s: approval is now stale — spec changed since approval; a human re-approves with 'cerne feature approve %s' (start is blocked until then)\n", id, id)
		}
	}
	maybeCompileOnPush(cmd, ctx)
	return nil
}

// pushedSpecStaleness maps the feature of each spec in docs to whether its
// approval is stale in the backend clone right now (lint.SpecChangedAfterApproval).
func pushedSpecStaleness(repoDir string, docs []pushedDoc) map[string]bool {
	out := map[string]bool{}
	for _, d := range docs {
		rest, ok := strings.CutPrefix(d.backendRel, "content/features/")
		id, isSpec := strings.CutSuffix(rest, "/spec.md")
		if !ok || !isSpec || strings.Contains(id, "/") {
			continue
		}
		if f, err := store.LoadFeature(repoDir, id); err == nil {
			out[id], _, _ = lint.SpecChangedAfterApproval(repoDir, f)
		}
	}
	return out
}

// maybeCompileOnPush (F-0117) recompiles the dirty wiki pages after a
// successful push when the project opts in (compile_on_push = true). It runs
// DETACHED: one page is one LLM call and routinely takes minutes, so doing it
// inline held `cerne push` — and whoever waited on it — for tens of minutes.
// The push now spawns `cerne compile` and returns; the wiki is a derived
// projection, so it may lag by seconds without anyone losing anything.
//
// Two pushes racing do not queue: the second compile finds the single-flight
// lock held and exits, because the holder re-reads the dirty set before
// releasing and absorbs that round too (compileSynchronous).
//
// wiki.enabled off (F-0316) silences it whatever compile_on_push says, the
// delegation warning included: auto-compile needs the wiki on AND the opt-in
// AND a callable mode. An explicit `cerne compile` is never blocked.
//
// Failures stay informational — a stderr warning, never a failed push.
// Delegation mode has no callable provider, so the hook warns instead of
// leaving surprise two-phase .pending files behind. `cerne sync` inherits the
// hook through runPush.
func maybeCompileOnPush(cmd *cobra.Command, ctx projCtx) {
	if !ctx.cfg.CompileOnPush || !wikiEnabled(cmd, ctx.projDir) {
		return
	}
	if llm.ResolveMode(ctx.cfg.LLM.Mode) == llm.ModeDelegation {
		fmt.Fprintln(cmd.ErrOrStderr(), "warning: compile_on_push requires a callable [llm] (claude-cli/byo-key) — run 'cerne compile' to recompile the wiki")
		return
	}
	if err := startBackgroundCompile(ctx); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not start the wiki recompilation (push OK) — run 'cerne compile': %v\n", err)
		return
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Wiki recompiling in background (log: %s).\n", compileLogPath(ctx.repoDir))
}

// reportPushConflicts prints each same-document conflict with its diff and a
// non-destructive resolution path, then returns a non-nil error so the command
// exits non-zero. Nothing was written to the backend.
func reportPushConflicts(cmd *cobra.Command, conflicts []app.Conflict) error {
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Conflict in %d document(s) — push aborted, nothing sent:\n", len(conflicts))
	for _, c := range conflicts {
		fmt.Fprintf(out, "\n  %s\n", c.Path)
		for _, line := range strings.Split(strings.TrimRight(c.Diff, "\n"), "\n") {
			fmt.Fprintf(out, "    %s\n", line)
		}
	}
	fmt.Fprintln(out, "\nThe same document changed in the backend. Reconcile and run 'cerne push' again, or 'cerne pull' to bring in the remote version.")
	return fmt.Errorf("%d document(s) in conflict", len(conflicts))
}

// reportDryRun validates a push without performing it: it lists the documents
// that would go to the backend and lints the staged tree with the same engine,
// committing and pushing nothing. It returns a non-zero error when the staged
// docs would lint dirty, so it is usable as a pre-push gate. Remote-drift was
// already enforced before this point, identical to a real push.
func reportDryRun(cmd *cobra.Command, ctx projCtx, docs []pushedDoc) error {
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Dry-run: %d document(s) would go to the backend (nothing committed/sent):\n", len(docs))
	for _, d := range docs {
		fmt.Fprintf(out, "  %s\n", d.backendRel)
	}
	root, cleanup, err := stagedLintRoot(ctx.repoDir, ctx.projDir)
	if err != nil {
		return err
	}
	defer cleanup()
	findings, err := app.ProjectLint(root, ctx.cfg)
	if err != nil {
		return err
	}
	lint.DowngradeWikiDebt(findings, wikiEnabled(cmd, ctx.projDir))
	if len(findings) == 0 {
		fmt.Fprintln(out, "Lint: no problems found.")
		return nil
	}
	printFindings(out, findings)
	return lintFailure(findings)
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
		return fmt.Errorf("CONTEXT.md is generated — don't edit it (run 'cerne pull' to restore)")
	}
	return nil
}

// collectChangedDocs walks .docs/current/** and returns every tracked *.md
// whose content no longer matches the docs.lock manifest, mapped back to its
// backend path via the cerne_id frontmatter. Files in the manifest but
// missing on disk are ignored (deletion is not a push operation). A changed
// document of a feature past the live states — read in the cache clone at
// repoDir — refuses the whole collection with app.ErrFeatureFrozen and the
// state's path (F-0305): between `complete` and the next pull the docs are
// still tracked, and push must not be the write that skips the boundary.
func collectChangedDocs(repoDir, docsDir string, manifest map[string]string) ([]pushedDoc, error) {
	currentDir := filepath.Join(docsDir, "current")
	if _, err := os.Stat(currentDir); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	var docs []pushedDoc
	var frozen []string // every frozen document's block: one refusal names them all
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
			if block, ok := untrackedFrozenBlock(repoDir, rel, body); ok {
				frozen = append(frozen, block)
				return nil
			}
			return fmt.Errorf("untracked file: %s — run 'cerne pull' to resync", rel)
		}
		id := parseCerneID(body)
		if id == "" {
			return fmt.Errorf("document without cerne_id in frontmatter: %s — new documents are born from 'cerne feature new'", rel)
		}
		backendRel, err := backendRelPath(rel, id)
		if err != nil {
			return err
		}
		if strings.HasPrefix(rel, "current/features/") {
			// ponytail: state read in the local cache clone before the push; its reconcile
			// fetches inside the lock but does not re-check state, so a complete made
			// elsewhere and not yet fetched slips through. Re-check there if that matters.
			f, err := store.LoadFeature(repoDir, id)
			if err != nil && !errors.Is(err, store.ErrNotFound) {
				return err
			}
			if err == nil && !domain.TitleEditable(f.State) {
				frozen = append(frozen, frozenBlock(rel, id, f.State))
				return nil
			}
		}
		docs = append(docs, pushedDoc{rel: rel, backendRel: backendRel, body: body, sum: sum})
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(frozen) > 0 {
		return nil, fmt.Errorf("%w: %s", app.ErrFeatureFrozen, strings.Join(frozen, "\n"))
	}
	return docs, nil
}

// frozenDirIDRe takes the feature id off the front of a materialized folder name.
var frozenDirIDRe = regexp.MustCompile(`^([A-Z]+-\d+)(?:-|$)`)

// untrackedFrozenBlock explains an untracked .docs/<rel> that belongs to a
// closed feature — the file an agent recreates by hand after a pull that did
// not materialize it (F-0307). The id comes from the cerne_id frontmatter, else
// from the folder name; the state from the cache clone, with no fetch, so a
// stale state only misleads the hint. nil when rel is not a spec/plan/result of
// a known closed feature: the caller keeps its untracked-file phrase (ok false).
func untrackedFrozenBlock(repoDir, rel string, body []byte) (string, bool) {
	doc := strings.TrimSuffix(path.Base(rel), ".md")
	if !strings.HasPrefix(rel, "current/features/") || (doc != "spec" && doc != "plan" && doc != "result") {
		return "", false
	}
	id := parseCerneID(body)
	if id == "" {
		if m := frozenDirIDRe.FindStringSubmatch(path.Base(path.Dir(rel))); m != nil {
			id = m[1]
		}
	}
	if id == "" {
		return "", false
	}
	f, err := store.LoadFeature(repoDir, id)
	if err != nil || domain.TitleEditable(f.State) {
		return "", false
	}
	return frozenBlock(rel, id, f.State), true
}

// frozenBlock is push's refusal of a .docs/<rel> of a closed feature, tracked
// and changed or recreated untracked alike: the state, the correction the table
// gives (lint.Correction), how to read the text and how to unblock. The local
// copy blocks every push until removed — pull never re-materializes it — so the
// block says to remove it; an amend reads it first (--file may point at it), so
// no edit is lost. Nothing is deleted for the user.
func frozenBlock(rel, id string, s domain.FeatureState) string {
	doc := strings.TrimSuffix(path.Base(rel), ".md")
	local := ".docs/" + rel
	kind, cmd := lint.Correction(s, id, doc)
	fix := strings.Replace(cmd, "<result.md>", local, 1)
	if kind == lint.CorrectNone {
		fix = app.NoCorrectionNote(id)
	}
	unblock := "remove " + local + " (it blocks every push until removed)"
	if kind == lint.CorrectByAmend {
		unblock = "remove " + local + " (it blocks every push until removed; the amend reads it, so run that first)"
	}
	// `show --doc` has no errata document: where the path is an erratum (or none),
	// plain `show <id>` is the read that includes it.
	read := "cerne show " + id
	if kind == lint.CorrectByAmend {
		read += " --doc " + doc
	}
	return fmt.Sprintf("frozen document: %s — %s is %s and 'cerne pull' does not materialize it.\n  correct it: %s\n  read it:    %s\n  unblock:    %s",
		rel, id, s, fix, read, unblock)
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
// decisions, runbooks, notes and references collapse to content/<kind>/<ID>.md.
func backendRelPath(rel, id string) (string, error) {
	switch {
	case strings.HasPrefix(rel, "current/features/"):
		return "content/features/" + id + "/" + path.Base(rel), nil
	case strings.HasPrefix(rel, "current/decisions/"):
		return "content/decisions/" + id + ".md", nil
	case strings.HasPrefix(rel, "current/runbooks/"):
		return "content/runbooks/" + id + ".md", nil
	case strings.HasPrefix(rel, "current/notes/"):
		return "content/notes/" + id + ".md", nil
	case strings.HasPrefix(rel, "current/references/"):
		return "content/references/" + id + ".md", nil
	}
	return "", fmt.Errorf("untracked file: %s — run 'cerne pull' to resync", rel)
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
