// Package materialize projects the current state of a cerne backend into a
// local .docs/ tree: only live entities are written, so AI agents never see
// superseded or rejected history.
package materialize

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/slug"
	"gitlab.com/rockt/cerne/internal/store"
)

// featureDocFilenames are the lifecycle documents copied per feature, in
// the order their links appear in the CONTEXT.md Docs column.
var featureDocFilenames = []string{"spec.md", "plan.md", "result.md"}

// contextGroupOrder is the fixed display order of the CONTEXT.md feature
// table; terminal states (rejected, superseded) are never listed.
var contextGroupOrder = []domain.FeatureState{
	domain.StateInDevelopment,
	domain.StateApproved,
	domain.StateCandidate,
	domain.StateImplemented,
}

// materializedStates are the feature states whose content documents are
// copied into .docs/. Implemented features stay listed in CONTEXT.md but
// their content is not materialized (promotion-on-completion rule).
var materializedStates = map[domain.FeatureState]bool{
	domain.StateCandidate:     true,
	domain.StateApproved:      true,
	domain.StateInDevelopment: true,
}

const contextTemplate = `# {{.ProjectName}} — estado atual

> Gerado por cerne. Não edite: rode 'cerne pull' para atualizar.
> Histórico e documentos supersedidos: 'cerne search'.

## Features

{{if .Features}}| ID | Estado | Módulo | Título | Docs |
|---|---|---|---|---|
{{range .Features}}| {{.ID}} | {{.State}} | {{.Module}} | {{.Title}} | {{.Docs}} |
{{end}}{{else}}_Nenhuma._
{{end}}
## Decisões ativas

{{if .Decisions}}{{range .Decisions}}- {{.}}
{{end}}{{else}}_Nenhuma._
{{end}}
## Referências

{{if .References}}{{range .References}}- {{.}}
{{end}}{{else}}_Nenhuma._
{{end}}`

// contextData feeds contextTemplate; Decisions and References carry the
// already-rendered bullet text (markdown link or plain title).
type contextData struct {
	ProjectName string
	Features    []featureRow
	Decisions   []string
	References  []string
}

type featureRow struct {
	ID, State, Module, Title, Docs string
}

// Materialize wipes and rebuilds docsDir from the backend at repoDir,
// writing only current-state entities plus a generated CONTEXT.md. When
// scope is non-empty only features of that module are included
// (decisions and references are project-wide). It returns the manifest of
// written files: rel slash path -> sha256 hex (CONTEXT.md included,
// .gitkeep excluded).
func Materialize(repoDir, docsDir, scope string) (map[string]string, error) {
	proj, err := store.LoadProject(repoDir)
	if err != nil {
		return nil, err
	}
	features, err := store.LoadAllFeatures(repoDir)
	if err != nil {
		return nil, err
	}
	decisions, err := store.LoadAllDecisions(repoDir)
	if err != nil {
		return nil, err
	}
	references, err := store.LoadAllReferences(repoDir)
	if err != nil {
		return nil, err
	}

	if err := os.RemoveAll(docsDir); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(docsDir, ".gitkeep"), nil, 0o644); err != nil {
		return nil, err
	}

	manifest := map[string]string{}
	data := contextData{ProjectName: proj.Name}

	for _, state := range contextGroupOrder {
		for _, f := range features { // already sorted by ID
			if f.State != state {
				continue
			}
			if scope != "" && f.Module != scope {
				continue
			}
			row := featureRow{ID: f.ID, State: string(f.State), Module: f.Module, Title: f.Title}
			if materializedStates[f.State] {
				docs, err := copyFeatureDocs(repoDir, docsDir, f, manifest)
				if err != nil {
					return nil, err
				}
				row.Docs = docs
			}
			data.Features = append(data.Features, row)
		}
	}

	for _, d := range decisions {
		if d.State != domain.DecisionActive {
			continue
		}
		line, err := copyEntry(repoDir, docsDir, "decisions", d.ID, d.Title, manifest)
		if err != nil {
			return nil, err
		}
		data.Decisions = append(data.Decisions, line)
	}

	for _, r := range references {
		if r.State != domain.ReferenceCurrent {
			continue
		}
		line, err := copyEntry(repoDir, docsDir, "references", r.ID, r.Title, manifest)
		if err != nil {
			return nil, err
		}
		data.References = append(data.References, line)
	}

	tmpl, err := template.New("context").Parse(contextTemplate)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, err
	}
	sum, err := writeFile(docsDir, "CONTEXT.md", buf.Bytes())
	if err != nil {
		return nil, err
	}
	manifest["CONTEXT.md"] = sum
	return manifest, nil
}

// copyFeatureDocs copies the feature's existing lifecycle documents into
// current/features/<ID>-<slug>/ and returns the rendered Docs column:
// "[spec](path), [plan](path)" for the docs that exist.
func copyFeatureDocs(repoDir, docsDir string, f store.Feature, manifest map[string]string) (string, error) {
	dirName := entryName(f.ID, f.Title)
	var links []string
	for _, filename := range featureDocFilenames {
		src := filepath.Join(repoDir, "content", "features", f.ID, filename)
		body, err := os.ReadFile(src)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		rel := "current/features/" + dirName + "/" + filename
		sum, err := writeFile(docsDir, rel, body)
		if err != nil {
			return "", err
		}
		manifest[rel] = sum
		links = append(links, fmt.Sprintf("[%s](%s)", strings.TrimSuffix(filename, ".md"), rel))
	}
	return strings.Join(links, ", "), nil
}

// copyEntry copies content/<kind>/<ID>.md into current/<kind>/<ID>-<slug>.md
// and returns the CONTEXT.md bullet text: a markdown link when the content
// document exists, the plain "<ID> — <title>" otherwise.
func copyEntry(repoDir, docsDir, kind, id, title string, manifest map[string]string) (string, error) {
	label := fmt.Sprintf("%s — %s", id, title)
	body, err := os.ReadFile(filepath.Join(repoDir, "content", kind, id+".md"))
	if errors.Is(err, os.ErrNotExist) {
		return label, nil
	}
	if err != nil {
		return "", err
	}
	rel := "current/" + kind + "/" + entryName(id, title) + ".md"
	sum, err := writeFile(docsDir, rel, body)
	if err != nil {
		return "", err
	}
	manifest[rel] = sum
	return fmt.Sprintf("[%s](%s)", label, rel), nil
}

// entryName joins ID and slugified title; just the ID when the title
// slugifies to nothing.
func entryName(id, title string) string {
	s := slug.Make(title)
	if s == "" {
		return id
	}
	return id + "-" + s
}

// writeFile writes body to <docsDir>/<rel> (rel slash-separated), creating
// parent dirs, and returns the sha256 hex of body.
func writeFile(docsDir, rel string, body []byte) (string, error) {
	path := filepath.Join(docsDir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}
