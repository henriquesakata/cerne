package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/dectmpl"
	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/llm"
	"gitlab.com/rockt/cerne/internal/slug"
	"gitlab.com/rockt/cerne/internal/spectmpl"
	"gitlab.com/rockt/cerne/internal/store"
	"gitlab.com/rockt/cerne/internal/verbs"
)

func newDecisionCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "decision",
		Short: "Create and manage decisions (ADRs) in the backend",
	}
	cmd.PersistentFlags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.PersistentFlags().MarkHidden("dir")
	cmd.AddCommand(verbs.MarkChat(newDecisionNewCmd(&dir)))
	cmd.AddCommand(verbs.MarkChat(newDecisionListCmd(&dir)))
	cmd.AddCommand(verbs.MarkLocal(newDecisionDraftCmd(&dir)))
	cmd.AddCommand(verbs.MarkChat(newDecisionRetagCmd(&dir)))
	cmd.AddCommand(newDecisionSubstituirCmd(&dir))
	cmd.AddCommand(verbs.MarkChat(newErrataCmd(&dir)))
	return cmd
}

func newDecisionListCmd(dir *string) *cobra.Command {
	var all, asJSON bool
	var facet, component string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List decisions (ADRs) — active by default; --all includes history",
		Long: "Lists decisions read straight from the backend (store), not from the '.docs/' projection. By default shows only " +
			"the active ones; --all includes the superseded (history). Each line carries id, state, title and markers " +
			"(facet/component, successor). Filter by --facet and/or --component (logical AND). --json for structured output.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDecisionList(cmd, *dir, all, asJSON, facet, component)
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "include superseded decisions (history)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "machine-readable JSON output")
	cmd.Flags().StringVar(&facet, "facet", "", "filter by facet (e.g. auth)")
	cmd.Flags().StringVar(&component, "component", "", "filter by component (e.g. keycloak)")
	return cmd
}

func runDecisionList(cmd *cobra.Command, dir string, all, asJSON bool, facet, component string) error {
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	decisions, err := store.LoadAllDecisions(ctx.repoDir)
	if err != nil {
		return err
	}
	// Normalize the filters to the stored form so '--facet Auth'/'--component
	// Keycloak' match the lower/slug values persisted on the decision.
	facetQ := strings.ToLower(strings.TrimSpace(facet))
	compQ := slug.Make(component)
	w := cmd.OutOrStdout()
	shown := make([]store.Decision, 0, len(decisions))
	for _, d := range decisions {
		if !all && d.State != domain.DecisionActive {
			continue
		}
		if facetQ != "" && !slices.Contains(d.Facets, facetQ) {
			continue
		}
		if compQ != "" && !slices.Contains(d.Components, compQ) {
			continue
		}
		shown = append(shown, d)
	}

	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(struct {
			Decisions []store.Decision `json:"decisions"`
		}{shown})
	}

	for _, d := range shown {
		line := fmt.Sprintf("%s  [%s]  %s", d.ID, d.State, d.Title)
		var tags []string
		if len(d.Facets) > 0 {
			tags = append(tags, "facet "+strings.Join(d.Facets, ","))
		}
		if len(d.Components) > 0 {
			tags = append(tags, "comp "+strings.Join(d.Components, ","))
		}
		if d.SupersededBy != "" {
			tags = append(tags, "→ "+d.SupersededBy)
		}
		if len(tags) > 0 {
			line += "  · " + strings.Join(tags, " · ")
		}
		fmt.Fprintln(w, line)
	}
	if len(shown) == 0 {
		if all {
			fmt.Fprintln(w, "No decisions registered. Use 'cerne decision new \"<title>\"' to register an ADR.")
		} else {
			fmt.Fprintln(w, "No active decisions. Use 'cerne decision new \"<title>\"', or 'cerne decision list --all' to see history.")
		}
	}
	return nil
}

func newDecisionRetagCmd(dir *string) *cobra.Command {
	var facet, component string
	cmd := &cobra.Command{
		Use:   "retag <id>",
		Short: "Reset the tags (facet/component) of an active decision",
		Long: "Sets or replaces the tag axes of an active decision, without superseding — preserves state and body. " +
			"Changes only the axis passed: 'retag D-0001 --component keycloak' keeps the facets; '--facet \"\"' clears the " +
			"facets. A facet outside the closed set (infra,code,data,process,auth) is rejected.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDecisionRetag(cmd, *dir, args[0], facet, component,
				cmd.Flags().Changed("facet"), cmd.Flags().Changed("component"))
		},
	}
	cmd.Flags().StringVar(&facet, "facet", "", "new facets (CSV; closed set) — empty clears")
	cmd.Flags().StringVar(&component, "component", "", "new components (CSV) — empty clears")
	return cmd
}

func runDecisionRetag(cmd *cobra.Command, dir, id, facet, component string, setFacets, setComponents bool) error {
	if !setFacets && !setComponents {
		return fmt.Errorf("provide at least --facet or --component")
	}
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	d, err := app.RetagDecision(ctx.repoDir, ctx.projDir, id,
		splitInforms(facet), splitInforms(component), setFacets, setComponents)
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s retagged — facets: [%s] · components: [%s]\n",
		d.ID, strings.Join(d.Facets, ", "), strings.Join(d.Components, ", "))
	return nil
}

func newDecisionNewCmd(dir *string) *cobra.Command {
	var file, facet, component string
	var force bool
	cmd := &cobra.Command{
		Use:   "new <title>",
		Short: "Create a decision (active); one-shot with a --file/stdin body",
		Long: "Creates an active decision. With a body (--file or piped stdin) it is one-shot: the ADR is born with " +
			"real content, already committed and pushed — no draft+push cycle. Without a body, it is born as a stub of the " +
			"template to fill in. Use --facet (closed axis: infra,code,data,process,auth) and --component " +
			"(subsystem/tool, e.g. keycloak,gitea) to organize — both multi-valued, comma-separated. " +
			"A live decision with a similar title refuses the creation (F-0324); --force overrides it.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDecisionNew(cmd, *dir, args[0], file, facet, component, force)
		},
	}
	cmd.Flags().StringVar(&file, "file", "", "file with the ADR body (defaults to piped stdin); one-shot, skips draft+push")
	cmd.Flags().StringVar(&facet, "facet", "", "facets (comma-separated; closed set: infra,code,data,process,auth)")
	cmd.Flags().StringVar(&component, "component", "", "components (comma-separated subsystem/tool tags, e.g. keycloak,gitea)")
	cmd.Flags().BoolVar(&force, "force", false, "bypass the duplicate-registration guard (F-0324) for a consciously distinct decision")
	return cmd
}

func runDecisionNew(cmd *cobra.Command, dir, title, file, facet, component string, force bool) error {
	title = strings.TrimSpace(title)
	if title == "" {
		return fmt.Errorf("the decision title cannot be empty")
	}
	body, err := readOptionalBody(cmd, file)
	if err != nil {
		return err
	}
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	d, err := app.CreateDecision(ctx.repoDir, ctx.projDir, title, body, splitInforms(facet), splitInforms(component), force)
	if err != nil {
		return err
	}
	// A decision created with a body is already real content, committed and pushed
	// by CreateDecision (CommitAndSnapshot) — nothing to draft or push. A bodyless
	// decision is a stub: keep the draft + push next step.
	if strings.TrimSpace(body) != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "✓ %s created (active) with a body — already synced. Run 'cerne pull' to materialize locally.\n", d.ID)
		return nil
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s created (active). Next step: fill in the ADR ('cerne decision draft %s' scaffolds it) and run 'cerne push'.\n", d.ID, d.ID)
	return nil
}

func newDecisionSubstituirCmd(dir *string) *cobra.Command {
	var by, reason string
	cmd := &cobra.Command{
		Use:   "substituir <id>",
		Short: "Supersede one decision with another (requires --by and --reason)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDecisionSubstituir(cmd, *dir, args[0], by, reason)
		},
	}
	cmd.Flags().StringVar(&by, "by", "", "id of the decision that supersedes")
	cmd.Flags().StringVar(&reason, "reason", "", "reason for the supersession")
	return verbs.MarkLifecycle(cmd)
}

func runDecisionSubstituir(cmd *cobra.Command, dir, id, by, reason string) error {
	// Validate flags before resolveProject so bad invocations fail fast
	// without touching origin.
	if err := app.ValidateTransitionFlags(domain.ActionSupersede, by, reason); err != nil {
		return err
	}
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	out, err := app.TransitionDecision(ctx.repoDir, ctx.projDir, id, by, reason)
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s: %s → %s\n", out.ID, out.From, out.To)
	return nil
}

func newDecisionDraftCmd(dir *string) *cobra.Command {
	return &cobra.Command{
		Use:   "draft <id>",
		Short: "AI-draft a decision's ADR",
		Long: "Drafts a decision's ADR using the configured provider. In delegation mode (default) it " +
			"prints the assembled prompt for the host agent to write the ADR; in byo-key and claude-cli modes " +
			"it generates the text, writes it to content/decisions/<id>.md and pushes.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDecisionDraft(cmd, *dir, args[0])
		},
	}
}

func runDecisionDraft(cmd *cobra.Command, dir, id string) error {
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	d, err := store.LoadDecision(ctx.repoDir, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("decision %s not found in the backend", id)
		}
		return err
	}
	current := readDecisionCurrent(ctx.repoDir, id)
	prompt := BuildDecisionDraftPrompt(d, current)
	out := cmd.OutOrStdout()

	// delegation (default): print the prompt for the host agent to draft inline.
	if llm.ResolveMode(ctx.cfg.LLM.Mode) == llm.ModeDelegation {
		fmt.Fprint(out, prompt)
		fmt.Fprintf(out, "\n\nWrite the result to content/decisions/%s.md (keeping the cerne_id frontmatter) and run 'cerne push'.\n", id)
		return nil
	}

	// byo-key / claude-cli: synthesize, write the ADR, commit and push.
	prov, err := llm.NewProvider(ctx.cfg.LLM)
	if err != nil {
		return err
	}
	body, err := prov.Complete(cmd.Context(), prompt)
	if err != nil {
		return err
	}
	// Write and commit inside the backend lock (F-0122); the provider call
	// above stays outside it.
	err = app.MutateAndSnapshot(ctx.repoDir, ctx.projDir, func() (string, error) {
		if err := app.WriteStampedDraft(app.DecisionDocPath(ctx.repoDir, id), id, "decision", body); err != nil {
			return "", err
		}
		return "cerne: draft decision " + id, nil
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "%s's ADR drafted and pushed — run 'cerne pull' to review.\n", id)
	return nil
}

// readDecisionCurrent returns the current content/decisions/<id>.md, or "" when
// it is missing.
func readDecisionCurrent(repoDir, id string) string {
	data, err := os.ReadFile(app.DecisionDocPath(repoDir, id))
	if err != nil {
		return ""
	}
	return string(data)
}

// BuildDecisionDraftPrompt assembles the pt-BR prompt instructing the LLM to write
// a COMPLETE ADR from the decision's id/title and the current stub or content. It
// requests the canonical dectmpl.Sections in order and the authoring rules: named
// forces in the context, the decision stated imperatively, negative consequences
// always listed, alternatives with why-rejected, the [ESCLARECER] ambiguity
// marker, and right-sizing (status is cerne data, never a prose section).
func BuildDecisionDraftPrompt(d store.Decision, currentContent string) string {
	var b strings.Builder
	b.WriteString("Você está escrevendo um registro de decisão de arquitetura (ADR) de software, em português do Brasil.\n\n")
	b.WriteString("Decisão:\n")
	fmt.Fprintf(&b, "- id: %s\n", d.ID)
	fmt.Fprintf(&b, "- título: %s\n\n", d.Title)
	b.WriteString("Conteúdo atual do ADR (stub ou rascunho parcial; pode estar vazio):\n")
	b.WriteString("---\n")
	b.WriteString(strings.TrimSpace(currentContent))
	b.WriteString("\n---\n\n")
	b.WriteString("Escreva um ADR COMPLETO e coerente, em Markdown, com exatamente estas seções nesta ordem:\n")
	for _, s := range dectmpl.Sections {
		b.WriteString("## " + s + "\n")
	}
	b.WriteString("\nRegras de autoria:\n")
	b.WriteString("- No Contexto, nomeie as forças e restrições em tensão e o gatilho (por que agora); ancore no real (código, caso).\n")
	b.WriteString("- Enuncie a Decisão no imperativo (\"Vamos X\" / \"Usaremos X\") com as 2-3 razões que a fizeram vencer.\n")
	b.WriteString("- Em Consequências, liste SEMPRE as negativas/trade-offs e as restrições/invariantes, não só os benefícios.\n")
	b.WriteString("- Em Alternativas consideradas, dê 2+ opções com prós/contras e por que foram rejeitadas.\n")
	b.WriteString("- Marque qualquer incerteza com \"" + spectmpl.AmbiguityMarker + " pergunta específica]\" em vez de supor.\n")
	b.WriteString("- Dimensione pela decisão: se trivial, Alternativas/Implementação podem ser \"N/A\". Não escreva seção de status (o cerne gerencia active/superseded).\n")
	b.WriteString("- O título registra a decisão tomada, não a pergunta (\"Usar X\", não \"Devemos usar X?\"). Um ADR aceito é registro histórico — para mudar de rumo, supersede; não reescreva.\n\n")
	b.WriteString("Não inclua frontmatter (o cerne carimba o cerne_id). " + app.DraftOutputContract + "\n")
	return b.String()
}
