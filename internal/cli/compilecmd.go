package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/compile"
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
			".docs/wiki/.pending/ para o agente hospedeiro processar; depois rode 'cerne compile --ingest'.",
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
	if mode := llm.ResolveMode(ctx.cfg.LLM.Mode); mode != llm.ModeDelegation {
		return fmt.Errorf("modo LLM %q ainda não suportado (apenas 'delegation')", mode)
	}
	pages, err := compile.Pages(ctx.repoDir)
	if err != nil {
		return err
	}
	pages = compilablePages(pages, scope)
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
	return preparePrompts(cmd, ctx, dirty)
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

// preparePrompts (delegation phase 1) writes one prompt bundle per dirty page
// under .docs/wiki/.pending/ and tells the agent how to finish.
func preparePrompts(cmd *cobra.Command, ctx projCtx, dirty []compile.Page) error {
	out := cmd.OutOrStdout()
	if len(dirty) == 0 {
		fmt.Fprintln(out, "Nada a recompilar.")
		return nil
	}
	pendingRoot := filepath.Join(ctx.projDir, ".docs", "wiki", ".pending")
	for _, p := range dirty {
		path := filepath.Join(pendingRoot, filepath.FromSlash(p.Rel)+".prompt.md")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(compile.BuildPagePrompt(p)), 0o644); err != nil {
			return err
		}
	}
	fmt.Fprintf(out, "%d página(s) para compilar (prompts em .docs/wiki/.pending/):\n", len(dirty))
	for _, p := range dirty {
		fmt.Fprintf(out, "  %s → content/wiki/%s.md\n", p.Name, p.Rel)
	}
	fmt.Fprintln(out, "Processe cada prompt, escreva a página no backend e rode 'cerne compile --ingest'.")
	return nil
}

// ingestPending (delegation phase 2) stamps provenance on the pages the agent
// wrote for this round (one per pending prompt), commits and clears pending.
func ingestPending(cmd *cobra.Command, ctx projCtx, pages []compile.Page) error {
	out := cmd.OutOrStdout()
	pendingRoot := filepath.Join(ctx.projDir, ".docs", "wiki", ".pending")
	byRel := map[string]compile.Page{}
	for _, p := range pages {
		byRel[p.Rel] = p
	}
	head, err := gitback.HeadSHA(ctx.repoDir)
	if err != nil {
		return err
	}
	var done []compile.Page
	for _, p := range pages {
		promptPath := filepath.Join(pendingRoot, filepath.FromSlash(p.Rel)+".prompt.md")
		if _, err := os.Stat(promptPath); err != nil {
			continue // not requested this round
		}
		pagePath := filepath.Join(ctx.repoDir, "content", "wiki", filepath.FromSlash(p.Rel)+".md")
		body, err := os.ReadFile(pagePath)
		if err != nil {
			return fmt.Errorf("página %s não foi escrita pelo agente em content/wiki/%s.md: %w", p.Name, p.Rel, err)
		}
		prov := wiki.Provenance{Page: p.Name, Sources: sourceIDs(p), CompiledFrom: head, InputHash: p.InputHash()}
		stamped := wiki.RenderProvenance(prov) + "\n" + string(stripLeadingFrontmatter(body))
		if err := os.WriteFile(pagePath, []byte(stamped), 0o644); err != nil {
			return err
		}
		done = append(done, p)
	}
	if len(done) == 0 {
		fmt.Fprintln(out, "Nenhuma página pendente para ingerir.")
		return nil
	}
	if err := app.CommitAndSnapshot(ctx.repoDir, ctx.projDir, fmt.Sprintf("cerne: compile %d página(s)", len(done))); err != nil {
		return err
	}
	_ = os.RemoveAll(pendingRoot)
	fmt.Fprintf(out, "Compilado: %d página(s).\n", len(done))
	for _, p := range done {
		fmt.Fprintf(out, "  content/wiki/%s.md\n", p.Rel)
	}
	return nil
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
