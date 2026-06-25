package importer

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"gitlab.com/rockt/cerne/internal/domain"
)

// ErrInvalidPlan is the sentinel for an agent plan that does not satisfy the
// cerne model: an unknown kind, an illegal state for the kind, a missing
// required field, or a reference without a url.
var ErrInvalidPlan = errors.New("invalid plan")

// PlanItem is one entity the agent proposes from an arbitrary doc tree. It is
// the machine contract between the mapping agent (which classifies sources) and
// cerne (which validates and persists): the agent fills it, cerne is the
// authority on whether it is legal. Unlike a hub-import Candidate, the Body is
// synthesized markdown (not a file copy), so agent import works on any layout.
type PlanItem struct {
	Kind    string   `json:"kind"`              // feature|decision|reference|runbook|note
	Title   string   `json:"title"`             // entity title
	Module  string   `json:"module,omitempty"`  // optional subsystem (features)
	State   string   `json:"state"`             // must be valid for Kind (domain.ValidStates)
	Body    string   `json:"body"`              // synthesized markdown content
	URL     string   `json:"url,omitempty"`     // required for reference
	Informs []string `json:"informs,omitempty"` // F-/D- ids this backlinks (reference/note)
	Source  string   `json:"source"`            // originating file (provenance + idempotency)
}

// Plan is the agent's full proposal for a doc tree.
type Plan struct {
	Items []PlanItem `json:"items"`
}

// ParsePlan deserializes the agent's JSON plan.
func ParsePlan(data []byte) (Plan, error) {
	var p Plan
	if err := json.Unmarshal(data, &p); err != nil {
		return Plan{}, fmt.Errorf("parsing plan json: %w", err)
	}
	return p, nil
}

// ValidatePlan checks every item against the cerne model and returns
// ErrInvalidPlan (wrapped with the offending item) on the first violation, so
// ingest is all-or-nothing: the agent proposes, cerne is the authority. An
// empty plan is rejected — the agent mapped nothing, which the caller should
// surface rather than silently persist zero entities.
func ValidatePlan(p Plan) error {
	if len(p.Items) == 0 {
		return fmt.Errorf("%w: no items", ErrInvalidPlan)
	}
	for i, it := range p.Items {
		states, ok := domain.ValidStates(it.Kind)
		if !ok {
			return fmt.Errorf("%w: item %d: unknown kind %q", ErrInvalidPlan, i, it.Kind)
		}
		if !slices.Contains(states, it.State) {
			return fmt.Errorf("%w: item %d (%s): invalid state %q (want one of %v)", ErrInvalidPlan, i, it.Kind, it.State, states)
		}
		if strings.TrimSpace(it.Title) == "" {
			return fmt.Errorf("%w: item %d (%s): empty title", ErrInvalidPlan, i, it.Kind)
		}
		if strings.TrimSpace(it.Source) == "" {
			return fmt.Errorf("%w: item %d (%s): empty source", ErrInvalidPlan, i, it.Kind)
		}
		if strings.TrimSpace(it.Body) == "" {
			return fmt.Errorf("%w: item %d (%s): empty body", ErrInvalidPlan, i, it.Kind)
		}
		if it.Kind == "reference" && strings.TrimSpace(it.URL) == "" {
			return fmt.Errorf("%w: item %d: reference without url", ErrInvalidPlan, i)
		}
	}
	return nil
}

// Source is one documentation file CollectSources found: its slash path
// relative to the scanned root and its full content.
type Source struct {
	RelPath string
	Content string
}

// ignoredDirs are directories CollectSources never descends into: version
// control, dependency, build, and cerne's own projection.
var ignoredDirs = map[string]bool{
	".git": true, "node_modules": true, ".docs": true,
	"vendor": true, "dist": true,
}

// CollectSources walks root and returns its documentation sources for
// agent-assisted mapping: every markdown file plus canonical extensionless
// roots (HISTORY/CHANGELOG/README/CLAUDE), skipping ignoredDirs. This is what
// makes discovery independent of the hub convention — any layout yields its
// docs. Order is deterministic (RelPath ascending) so the prompt is stable.
func CollectSources(root string) ([]Source, error) {
	var sources []Source
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && ignoredDirs[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		if !isDocSource(d.Name()) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		sources = append(sources, Source{RelPath: filepath.ToSlash(rel), Content: string(data)})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].RelPath < sources[j].RelPath })
	return sources, nil
}

// isDocSource reports whether a file is a documentation source: any markdown
// file, or a canonical project root that may ship without an extension.
func isDocSource(name string) bool {
	if strings.HasSuffix(strings.ToLower(name), ".md") {
		return true
	}
	switch strings.ToUpper(name) {
	case "HISTORY", "CHANGELOG", "README", "CLAUDE":
		return true
	}
	return false
}

// planKinds is the set of entity kinds the agent may propose, in the order the
// prompt presents them.
var planKinds = []string{"feature", "decision", "reference", "runbook", "note"}

// sourceCap bounds how much of each source's content enters the prompt, so one
// large file cannot blow the model's context; the overflow is replaced with a
// truncation marker.
const sourceCap = 8000

// planSchemaJSON is the shape the agent must emit — one example item with every
// field, matching PlanItem's json tags.
const planSchemaJSON = `{
  "items": [
    {
      "kind": "feature|decision|reference|runbook|note",
      "title": "título da entidade",
      "module": "(opcional) subsistema, só para feature",
      "state": "estado válido para o kind (veja o modelo acima)",
      "body": "conteúdo markdown sintetizado a partir da(s) fonte(s)",
      "url": "(obrigatório quando kind=reference) link externo",
      "informs": ["F-0001", "D-0002"],
      "source": "caminho/relativo/da/fonte.md"
    }
  ]
}`

// BuildPlanPrompt assembles the pt-BR mapping prompt the agent fills: the cerne
// model (the five kinds and their lifecycle states), the JSON plan schema it
// must emit, and the discovered sources (each capped at sourceCap). The agent
// classifies each source into the right kind; cerne validates and persists.
func BuildPlanPrompt(sources []Source) string {
	var b strings.Builder
	b.WriteString("Você é um agente que conhece o modelo do cerne e vai mapear a documentação ")
	b.WriteString("deste projeto para entidades do cerne. Leia as fontes abaixo, descubra o padrão ")
	b.WriteString("de documentação e produza um plano de entidades. O cerne valida e persiste o seu plano — ")
	b.WriteString("você propõe, o cerne é a autoridade.\n\n")

	b.WriteString("## Modelo do cerne (kinds e estados válidos)\n\n")
	for _, kind := range planKinds {
		states, _ := domain.ValidStates(kind)
		fmt.Fprintf(&b, "- **%s** — estados: %s\n", kind, strings.Join(states, ", "))
	}
	b.WriteString("\nClassifique cada fonte no tipo certo: changelog/histórico cronológico → ")
	b.WriteString("decision/note/feature; doc de estado-atual (ex.: CLAUDE.md) → note/runbook; ")
	b.WriteString("procedimento operacional → runbook; link externo que informou o projeto → ")
	b.WriteString("reference (com url e informs). Sintetize o body em markdown; não copie a fonte cru.\n")

	b.WriteString("\n## Schema do plano (emita SÓ este JSON, sem cercas nem prosa)\n\n```json\n")
	b.WriteString(planSchemaJSON)
	b.WriteString("\n```\n")

	b.WriteString("\n## Fontes do projeto\n")
	for _, s := range sources {
		fmt.Fprintf(&b, "\n### %s\n\n", s.RelPath)
		b.WriteString(capContent(s.Content))
		b.WriteString("\n")
	}
	return b.String()
}

// capContent returns content truncated to sourceCap bytes with a marker when it
// overflows, so a single large file does not dominate the prompt.
func capContent(content string) string {
	if len(content) <= sourceCap {
		return content
	}
	return content[:sourceCap] + "\n… [truncado]"
}
