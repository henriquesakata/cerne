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
