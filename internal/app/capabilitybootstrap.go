package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/llm"
	"github.com/henriquesakata/cerne/internal/slug"
	"github.com/henriquesakata/cerne/internal/store"
)

// CapabilityBootstrapActor is stamped on entities/links created by --apply,
// so the event log distinguishes the bulk import from hand curation.
const CapabilityBootstrapActor = "capability-bootstrap"

// Sentinels for the bootstrap flow (F-0188 crit. 4-5).
var (
	ErrEmptyProposal = errors.New("provider replied empty — proposal not written")
	ErrNoProposal    = errors.New("no capability proposal found — run 'cerne capability bootstrap' first")
	// errNothingToApply short-circuits the snapshot when a re-apply finds the
	// whole proposal already imported (idempotence) — not an error to callers.
	errNothingToApply = errors.New("nothing to apply")
)

// CapabilityProposalNode is one node of the proposed functional tree; Features
// lists the F- ids the model attached to THIS node.
type CapabilityProposalNode struct {
	Title    string                   `json:"title"`
	Children []CapabilityProposalNode `json:"children,omitempty"`
	Features []string                 `json:"features,omitempty"`
}

// CapabilityProposal is the reviewable artifact the bootstrap writes — never
// applied to the backend until an explicit --apply.
type CapabilityProposal struct {
	GeneratedAt time.Time                `json:"generated_at"`
	Tree        []CapabilityProposalNode `json:"tree"`
}

// proposalDir is where the reviewable proposal lives, inside the (gitignored)
// .docs sidecar of the user's project.
func proposalDir(projDir string) string {
	return filepath.Join(projDir, ".docs", ".capability-proposal")
}

// specExcerptLimit bounds how much of each spec feeds the prompt — hundreds of
// features (hub: 465) with full specs would blow the context for nothing.
const specExcerptLimit = 300

// CapabilityBootstrapPrompt builds the pt-BR prompt (doc-authoring convention)
// asking the provider to propose the product-capability tree from the live +
// delivered features (terminal ones carry no product surface).
func CapabilityBootstrapPrompt(repoDir string) (string, error) {
	features, err := store.LoadAllFeatures(repoDir)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("Você é o curador da documentação viva de um produto. A partir das features abaixo (id, título e trecho da spec), proponha a ÁRVORE FUNCIONAL do produto — as capacidades como o usuário as navega (ex.: Studio → Projetos → Issues → Relatar bugs), NÃO a organização dos registros de trabalho.\n\n")
	b.WriteString("Regras:\n- Responda APENAS com JSON válido, sem cercas de código e sem preâmbulo.\n- Schema: {\"tree\": [{\"title\": string, \"children\": [...], \"features\": [\"F-…\"]}]}.\n- Cada feature aparece nos nós (folhas de preferência) a que contribui; uma feature pode aparecer em mais de um nó.\n- Títulos curtos, em pt-BR, orientados a capacidade (verbo/substantivo do produto).\n- 2 a 4 níveis de profundidade; agrupe com parcimônia.\n\nFeatures:\n")
	for _, f := range features {
		if f.State == domain.StateRejected || f.State == domain.StateSuperseded {
			continue
		}
		b.WriteString(fmt.Sprintf("- %s [%s] %s\n", f.ID, f.State, f.Title))
		if excerpt := specExcerpt(repoDir, f.ID); excerpt != "" {
			b.WriteString("  spec: " + excerpt + "\n")
		}
	}
	return b.String(), nil
}

// specExcerpt returns the first specExcerptLimit characters of a feature's
// spec body, single-line; empty when the spec is absent or a stub.
func specExcerpt(repoDir, id string) string {
	data, err := os.ReadFile(filepath.Join(repoDir, "content", "features", id, "spec.md"))
	if err != nil {
		return ""
	}
	text := strings.Join(strings.Fields(string(data)), " ")
	if len(text) > specExcerptLimit {
		text = text[:specExcerptLimit] + "…"
	}
	return text
}

// GenerateCapabilityProposal asks the provider for the tree and writes the
// reviewable proposal files (json + human-readable md) under
// .docs/.capability-proposal/ — the backend is never touched here (crit. 4).
func GenerateCapabilityProposal(ctx context.Context, repoDir, projDir string, p llm.Provider) (CapabilityProposal, error) {
	prompt, err := CapabilityBootstrapPrompt(repoDir)
	if err != nil {
		return CapabilityProposal{}, err
	}
	reply, err := p.Complete(ctx, prompt)
	if err != nil {
		return CapabilityProposal{}, err
	}
	text := strings.TrimSpace(reply)
	if text == "" {
		return CapabilityProposal{}, ErrEmptyProposal
	}
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	var prop CapabilityProposal
	if err := json.Unmarshal([]byte(strings.TrimSpace(text)), &prop); err != nil {
		return CapabilityProposal{}, fmt.Errorf("provider reply is not the expected JSON schema: %w", err)
	}
	prop.GeneratedAt = time.Now().UTC()

	dir := proposalDir(projDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return CapabilityProposal{}, err
	}
	raw, err := json.MarshalIndent(prop, "", "  ")
	if err != nil {
		return CapabilityProposal{}, err
	}
	if err := os.WriteFile(filepath.Join(dir, "proposal.json"), raw, 0o644); err != nil {
		return CapabilityProposal{}, err
	}
	if err := os.WriteFile(filepath.Join(dir, "proposal.md"), []byte(renderProposalMD(prop)), 0o644); err != nil {
		return CapabilityProposal{}, err
	}
	return prop, nil
}

// renderProposalMD renders the human-review view of the proposal.
func renderProposalMD(p CapabilityProposal) string {
	var b strings.Builder
	b.WriteString("# Proposta de taxonomia de capacidades\n\n")
	b.WriteString("Revise (edite o proposal.json ao lado) e aplique com `cerne capability bootstrap --apply`.\n\n")
	var walk func(nodes []CapabilityProposalNode, depth int)
	walk = func(nodes []CapabilityProposalNode, depth int) {
		for _, n := range nodes {
			b.WriteString(strings.Repeat("  ", depth) + "- " + n.Title)
			if len(n.Features) > 0 {
				b.WriteString(" (" + strings.Join(n.Features, ", ") + ")")
			}
			b.WriteString("\n")
			walk(n.Children, depth+1)
		}
	}
	walk(p.Tree, 0)
	return b.String()
}

// ApplyResult summarizes what an apply changed.
type ApplyResult struct {
	Created int // capabilities minted
	Linked  int // feature→capability links added
}

// ApplyCapabilityProposal imports the reviewed proposal into the backend as a
// single commit: capabilities are matched by normalized title (slug) so a
// re-apply neither duplicates nodes nor re-links features (crit. 5). Unknown
// feature ids in the proposal are skipped.
func ApplyCapabilityProposal(repoDir, projDir string) (ApplyResult, error) {
	raw, err := os.ReadFile(filepath.Join(proposalDir(projDir), "proposal.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return ApplyResult{}, ErrNoProposal
		}
		return ApplyResult{}, err
	}
	var prop CapabilityProposal
	if err := json.Unmarshal(raw, &prop); err != nil {
		return ApplyResult{}, fmt.Errorf("proposal.json: %w", err)
	}

	var res ApplyResult
	err = MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		res = ApplyResult{}
		existing, err := store.LoadAllCapabilities(repoDir)
		if err != nil {
			return "", err
		}
		bySlug := map[string]store.Capability{}
		for _, c := range existing {
			bySlug[slug.Make(c.Title)] = c
		}
		features, err := store.LoadAllFeatures(repoDir)
		if err != nil {
			return "", err
		}
		featByID := map[string]store.Feature{}
		for _, f := range features {
			featByID[f.ID] = f
		}
		now := time.Now().UTC()

		ensure := func(title, parent string) (store.Capability, error) {
			if c, ok := bySlug[slug.Make(title)]; ok {
				return c, nil
			}
			id, err := store.NextID(repoDir, "C")
			if err != nil {
				return store.Capability{}, err
			}
			c := store.Capability{
				ID: id, Title: title, Parent: parent,
				State: domain.CapabilityActive, CreatedAt: now, UpdatedAt: now,
			}
			if err := store.SaveCapability(repoDir, c); err != nil {
				return store.Capability{}, err
			}
			bySlug[slug.Make(title)] = c
			res.Created++
			return c, nil
		}
		dirtyFeatures := map[string]store.Feature{}
		var walk func(nodes []CapabilityProposalNode, parent string) error
		walk = func(nodes []CapabilityProposalNode, parent string) error {
			for _, n := range nodes {
				c, err := ensure(strings.TrimSpace(n.Title), parent)
				if err != nil {
					return err
				}
				for _, fid := range n.Features {
					f, ok := dirtyFeatures[fid]
					if !ok {
						f, ok = featByID[fid]
					}
					if !ok {
						continue // unknown feature id in the proposal — skip
					}
					already := false
					for _, cid := range f.Capabilities {
						if cid == c.ID {
							already = true
							break
						}
					}
					if already {
						continue
					}
					f.Capabilities = append(f.Capabilities, c.ID)
					f.UpdatedAt = now
					dirtyFeatures[fid] = f
					res.Linked++
				}
				if err := walk(n.Children, c.ID); err != nil {
					return err
				}
			}
			return nil
		}
		if err := walk(prop.Tree, ""); err != nil {
			return "", err
		}
		for _, f := range dirtyFeatures {
			if err := store.SaveFeature(repoDir, f); err != nil {
				return "", err
			}
		}
		if res.Created == 0 && res.Linked == 0 {
			return "", errNothingToApply
		}
		return fmt.Sprintf("cerne: capability bootstrap apply (%d capabilities, %d links) [%s]",
			res.Created, res.Linked, CapabilityBootstrapActor), nil
	})
	if errors.Is(err, errNothingToApply) {
		return res, nil
	}
	return res, err
}
