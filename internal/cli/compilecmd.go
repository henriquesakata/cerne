package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/compile"
	"gitlab.com/rockt/cerne/internal/doccheck"
	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/llm"
	"gitlab.com/rockt/cerne/internal/wiki"
)

func newCompileCmd() *cobra.Command {
	var dir, scope string
	var force, ingest bool
	cmd := &cobra.Command{
		Use:   "compile",
		Short: "Compile per-module synthesis wiki pages (LLM layer)",
		Long: "Compila páginas de síntese por módulo em content/wiki/, de forma incremental " +
			"(só o que mudou, por input-hash). No modo delegação (default) emite prompts em " +
			pendingRelDir + "/ para o agente hospedeiro processar; depois rode 'cerne compile --ingest'. " +
			"O contrato de máquina (onde escrever cada resposta e como confirmar) está em " +
			pendingRelDir + "/manifest.json — um response_path por página.",
		Example: "  # 1) emitir os prompts (modo delegação, default)\n" +
			"  cerne compile\n" +
			"  # 2) para cada página, escreva a resposta no response_path declarado em\n" +
			"  #    " + pendingRelDir + "/manifest.json (campos: page, prompt_path, response_path, run_id)\n" +
			"  # 3) ingerir: carimba a provenance e commita só as respostas desta rodada\n" +
			"  cerne compile --ingest",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCompile(cmd, dir, scope, force, ingest)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.Flags().MarkHidden("dir")
	cmd.Flags().StringVar(&scope, "scope", "", "restrict to a single module page")
	cmd.Flags().BoolVar(&force, "force", false, "recompile every page, ignoring the input-hash")
	cmd.Flags().BoolVar(&ingest, "ingest", false, "ingest agent-written pages: stamp provenance and commit")
	return cmd
}

func runCompile(cmd *cobra.Command, dir, scope string, force, ingest bool) error {
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	pages, err := compile.Pages(ctx.repoDir)
	if err != nil {
		return err
	}
	pages = compilablePages(pages, scope)

	// byo-key: one-phase synchronous synthesis via the configured Provider.
	if llm.ResolveMode(ctx.cfg.LLM.Mode) != llm.ModeDelegation {
		if ingest {
			return fmt.Errorf("--ingest é exclusivo do modo delegação; no modo byo-key a compilação é síncrona")
		}
		return compileSynchronous(cmd, ctx, pages, force)
	}

	// delegation (default): two-phase via prompt files for the host agent.
	if ingest {
		return ingestPending(cmd, ctx, pages)
	}
	if err := reportModulelessSkipped(cmd, ctx.repoDir); err != nil {
		return err
	}
	dirty, err := compile.Dirty(ctx.repoDir, pages, force)
	if err != nil {
		return err
	}
	return preparePrompts(cmd, ctx, dirty, scope)
}

// compileSynchronous is the byo-key path: it builds the configured Provider and,
// for each dirty page, calls Complete with the deterministic page prompt, writes
// the synthesized body with provenance frontmatter (same stamping as ingest),
// and finally commits+pushes once when at least one page was written.
func compileSynchronous(cmd *cobra.Command, ctx projCtx, pages []compile.Page, force bool) error {
	out := cmd.OutOrStdout()
	if err := reportModulelessSkipped(cmd, ctx.repoDir); err != nil {
		return err
	}
	done, err := compileDirtyPages(cmd.Context(), ctx, pages, force)
	if err != nil {
		return err
	}
	if len(done) == 0 {
		fmt.Fprintln(out, "Nada a recompilar.")
		return nil
	}
	fmt.Fprintf(out, "Compiladas %d página(s) (modo byo-key).\n", len(done))
	for _, p := range done {
		fmt.Fprintf(out, "  content/wiki/%s.md\n", p.Rel)
	}
	return nil
}

// compileDirtyPages runs the one-phase synthesis over the dirty subset of pages:
// Complete per dirty page through the configured callable provider, provenance-
// stamped write, one commit+push when anything was written. Returns the compiled
// pages — empty means nothing was dirty, so no provider is built and no LLM is
// called. Shared by `cerne compile` (byo-key/claude-cli) and the compile_on_push
// hook (F-0117).
func compileDirtyPages(goCtx context.Context, ctx projCtx, pages []compile.Page, force bool) ([]compile.Page, error) {
	dirty, err := compile.Dirty(ctx.repoDir, pages, force)
	if err != nil {
		return nil, err
	}
	if len(dirty) == 0 {
		return nil, nil
	}
	prov, err := llm.NewProvider(ctx.cfg.LLM)
	if err != nil {
		return nil, err
	}
	head, err := gitback.HeadSHA(ctx.repoDir)
	if err != nil {
		return nil, err
	}
	var done []compile.Page
	for _, p := range dirty {
		body, err := prov.Complete(goCtx, compile.BuildPagePrompt(p))
		if err != nil {
			return nil, fmt.Errorf("sintetizando a página %s: %w", p.Name, err)
		}
		if err := writeStampedPage(ctx.repoDir, p, head, []byte(body)); err != nil {
			return nil, err
		}
		done = append(done, p)
	}
	if err := app.CommitAndSnapshot(ctx.repoDir, ctx.projDir, fmt.Sprintf("cerne: compile %d página(s)", len(done))); err != nil {
		return nil, err
	}
	return done, nil
}

// reportModulelessSkipped tells the user, on the prepare phase, how many live
// features the wiki omits for lacking a declared module — no silent drop. The
// deterministic index (CONTEXT.md) and search still cover them.
func reportModulelessSkipped(cmd *cobra.Command, repoDir string) error {
	n, err := compile.ModulelessSkipped(repoDir)
	if err != nil {
		return err
	}
	if n == 0 {
		return nil
	}
	fmt.Fprintf(cmd.OutOrStdout(),
		"%d feature(s) sem módulo não entram na wiki (veja CONTEXT.md / cerne search); atribua módulos para sintetizá-las.\n", n)
	return nil
}

// compilablePages keeps the pages worth compiling: those with at least one
// source, optionally restricted to a single module by scope.
func compilablePages(pages []compile.Page, scope string) []compile.Page {
	var out []compile.Page
	for _, p := range pages {
		if len(p.Sources) == 0 {
			continue
		}
		if scope != "" && p.Name != scope {
			continue
		}
		out = append(out, p)
	}
	return out
}

// pendingRelDir is the project-relative directory (forward-slash, portable)
// holding the delegation prompts, responses and manifest for the current round.
const pendingRelDir = ".docs/wiki/.pending"

// preparePrompts (delegation phase 1) writes one prompt bundle per dirty page
// under .docs/wiki/.pending/ plus a manifest.json declaring, per page, the
// response_path the agent must write and the run id it must echo. The prompt
// announces that run id so the ingest can verify freshness. It then tells the
// agent how to finish.
func preparePrompts(cmd *cobra.Command, ctx projCtx, dirty []compile.Page, scope string) error {
	out := cmd.OutOrStdout()
	if len(dirty) == 0 {
		fmt.Fprintln(out, "Nada a recompilar.")
		return nil
	}
	pendingRoot := filepath.Join(ctx.projDir, filepath.FromSlash(pendingRelDir))
	if err := os.MkdirAll(pendingRoot, 0o755); err != nil {
		return err
	}
	runID := compile.NewRunID(scope, time.Now().UTC())
	manifest := compile.Manifest{Version: compile.ManifestVersion, RunID: runID}
	for _, p := range dirty {
		promptRel := path.Join(pendingRelDir, p.Rel+".prompt.md")
		responseRel := path.Join(pendingRelDir, p.Rel+".md")
		promptPath := filepath.Join(ctx.projDir, filepath.FromSlash(promptRel))
		if err := os.MkdirAll(filepath.Dir(promptPath), 0o755); err != nil {
			return err
		}
		header := fmt.Sprintf("<!-- cerne-run-id: %s — ecoe como 'run_id:' no frontmatter da resposta em %s -->\n\n", runID, responseRel)
		if err := os.WriteFile(promptPath, []byte(header+compile.BuildPagePrompt(p)), 0o644); err != nil {
			return err
		}
		manifest.Entries = append(manifest.Entries, compile.ManifestEntry{
			Page:         p.Name,
			PromptPath:   promptRel,
			ResponsePath: responseRel,
			SourceHash:   p.InputHash(),
			RunID:        runID,
		})
	}
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(pendingRoot, compile.ManifestName), manifestData, 0o644); err != nil {
		return err
	}
	fmt.Fprintf(out, "%d página(s) para compilar (prompts em %s/):\n", len(dirty), pendingRelDir)
	for _, p := range dirty {
		fmt.Fprintf(out, "  %s → %s\n", p.Name, path.Join(pendingRelDir, p.Rel+".md"))
	}
	fmt.Fprintf(out, "Contrato em %s/%s: escreva cada response_path (ecoando run_id: <id> no frontmatter) e rode 'cerne compile --ingest'.\n", pendingRelDir, compile.ManifestName)
	return nil
}

// ErrPendingResponseMissing is returned when a manifest entry's declared
// response_path was not written by the agent for this round. It is a hard error
// (the exact expected path is reported) — the indiscoverable "write somewhere
// and hope" path is gone.
var ErrPendingResponseMissing = errors.New("resposta pendente ausente")

// ErrPendingResponseStale is the marker for a response that does not belong to
// the current run (a stale page from a previous round). Such a page is reported
// and skipped, never committed as success — the bug this contract fixes.
var ErrPendingResponseStale = errors.New("resposta pendente obsoleta")

// ingestPending (delegation phase 2) reads the round's manifest and, per
// declared entry, validates the response the agent wrote at the declared
// response_path and stamps provenance on the fresh ones, commits them, and
// reports an honest per-page status (ingerido / obsoleto / pulado). A stale
// response is skipped (not committed); a missing one keeps pending and returns
// a hard error naming the exact path (crit. 2). "Compilado: N" counts only the
// pages actually ingested this round.
func ingestPending(cmd *cobra.Command, ctx projCtx, pages []compile.Page) error {
	out := cmd.OutOrStdout()
	pendingRoot := filepath.Join(ctx.projDir, filepath.FromSlash(pendingRelDir))
	manifest, err := readManifest(pendingRoot)
	if errors.Is(err, fs.ErrNotExist) {
		fmt.Fprintln(out, "Nenhuma página pendente para ingerir.")
		return nil
	}
	if err != nil {
		return err
	}
	byName := make(map[string]compile.Page, len(pages))
	for _, p := range pages {
		byName[p.Name] = p
	}
	head, err := gitback.HeadSHA(ctx.repoDir)
	if err != nil {
		return err
	}

	type pageStatus struct{ page, status string }
	var statuses []pageStatus
	var done []compile.Page
	var missing []string
	for _, e := range manifest.Entries {
		responsePath := filepath.Join(ctx.projDir, filepath.FromSlash(e.ResponsePath))
		promptPath := filepath.Join(ctx.projDir, filepath.FromSlash(e.PromptPath))
		body, err := os.ReadFile(responsePath)
		if errors.Is(err, fs.ErrNotExist) {
			statuses = append(statuses, pageStatus{e.Page, fmt.Sprintf("pulado (sem resposta em %s)", e.ResponsePath)})
			missing = append(missing, e.ResponsePath)
			continue
		}
		if err != nil {
			return err
		}
		p, ok := byName[e.Page]
		if !ok {
			return fmt.Errorf("página %s do manifesto não corresponde a nenhuma página compilável", e.Page)
		}
		if !isFresh(e, body, promptPath, responsePath) {
			statuses = append(statuses, pageStatus{e.Page, "obsoleto"})
			continue
		}
		if err := writeStampedPage(ctx.repoDir, p, head, body); err != nil {
			return err
		}
		done = append(done, p)
		statuses = append(statuses, pageStatus{e.Page, "ingerido"})
	}

	if len(done) > 0 {
		if err := app.CommitAndSnapshot(ctx.repoDir, ctx.projDir, fmt.Sprintf("cerne: compile %d página(s)", len(done))); err != nil {
			return err
		}
	}
	for _, s := range statuses {
		fmt.Fprintf(out, "  %s: %s\n", s.page, s.status)
	}
	fmt.Fprintf(out, "Compilado: %d página(s).\n", len(done))

	if len(missing) > 0 {
		// Keep pending so the agent can write the missing responses and re-ingest.
		return fmt.Errorf("%d resposta(s) declarada(s) ausente(s) (ex.: %s): %w", len(missing), missing[0], ErrPendingResponseMissing)
	}
	// A fully-consumed round (every declared page ingested) clears pending so a
	// re-run is a clean no-op; an incomplete round (some obsolete) keeps it so
	// the agent can rewrite and re-ingest without re-preparing.
	if len(done) == len(manifest.Entries) {
		_ = os.RemoveAll(pendingRoot)
	}
	return nil
}

// isFresh reports whether the agent's response belongs to the current round. The
// run id echoed in the response frontmatter is the source of truth: present and
// equal → fresh; present but different → stale. When the response carries no
// frontmatter run id at all, fall back to mtime (response no older than its
// prompt) — never accept a present-but-divergent run id on mtime grounds.
func isFresh(e compile.ManifestEntry, body []byte, promptPath, responsePath string) bool {
	if echoed := doccheck.FrontmatterValue(body, "run_id"); echoed != "" {
		return echoed == e.RunID
	}
	rp, err := os.Stat(responsePath)
	if err != nil {
		return false
	}
	pp, err := os.Stat(promptPath)
	if err != nil {
		return false
	}
	return !rp.ModTime().Before(pp.ModTime())
}

// readManifest loads the delegation manifest from a pending directory. A missing
// manifest is returned as fs.ErrNotExist so callers can treat "nothing pending"
// distinctly from a malformed one.
func readManifest(pendingRoot string) (compile.Manifest, error) {
	data, err := os.ReadFile(filepath.Join(pendingRoot, compile.ManifestName))
	if err != nil {
		return compile.Manifest{}, err
	}
	var m compile.Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return compile.Manifest{}, fmt.Errorf("lendo o manifesto de delegação em %s: %w", pendingRoot, err)
	}
	return m, nil
}

// writeStampedPage writes content/wiki/<rel>.md for the page: cerne's provenance
// frontmatter (computed from the page sources and the backend HEAD sha) followed
// by body, with any leading frontmatter the body already carried stripped first.
// Shared by the delegation ingest phase and the byo-key synchronous path so both
// emit the exact same frontmatter format.
func writeStampedPage(repoDir string, p compile.Page, head string, body []byte) error {
	pagePath := filepath.Join(repoDir, "content", "wiki", filepath.FromSlash(p.Rel)+".md")
	if err := os.MkdirAll(filepath.Dir(pagePath), 0o755); err != nil {
		return err
	}
	prov := wiki.Provenance{Page: p.Name, Sources: sourceIDs(p), CompiledFrom: head, InputHash: p.InputHash()}
	stamped := wiki.RenderProvenance(prov) + "\n" + string(stripLeadingFrontmatter(body))
	return os.WriteFile(pagePath, []byte(stamped), 0o644)
}

// sourceIDs lists a page's source entity IDs in order.
func sourceIDs(p compile.Page) []string {
	ids := make([]string, 0, len(p.Sources))
	for _, s := range p.Sources {
		ids = append(ids, s.ID)
	}
	return ids
}

// stripLeadingFrontmatter removes a leading "---" block the agent may have
// added, so cerne's stamped provenance is the only frontmatter.
func stripLeadingFrontmatter(data []byte) []byte {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	if !bytes.HasPrefix(bytes.TrimLeft(data, " \t\n"), []byte("---")) {
		return data
	}
	lines := strings.SplitN(string(data), "\n", -1)
	// find the first "---" then the closing "---"
	start := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == "---" {
			start = i
			break
		}
	}
	if start == -1 {
		return data
	}
	for i := start + 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			return []byte(strings.TrimLeft(strings.Join(lines[i+1:], "\n"), "\n"))
		}
	}
	return data
}
