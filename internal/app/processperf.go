package app

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/henriquesakata/cerne/internal/cost"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/lint"
	"github.com/henriquesakata/cerne/internal/stats"
	"github.com/henriquesakata/cerne/internal/store"
)

// ProcessPerf collects the process panel (F-0321) from the backend clone at
// repoDir and computes it: one sample per entity with a feature.completed
// (dated by the first), from its result's own cost anchor and Gauntlet record;
// the fixes links as escaped defects; decisions with facet process and
// component metric, in any state, as markers. A delivery with no anchor (or an
// all-zero one) is excluded and counted. Read-only. now decides the pending
// escaped guard.
// ponytail: reads every completed result per call (~600 small files on the hub); an index if the panel gets slow.
func ProcessPerf(repoDir string, now time.Time) (stats.ProcessReport, error) {
	events, err := store.ReadEvents(repoDir)
	if err != nil {
		return stats.ProcessReport{}, err
	}
	ids, completed := completions(events)
	feats, err := store.LoadAllFeatures(repoDir)
	if err != nil {
		return stats.ProcessReport{}, err
	}
	fixes := map[string][]time.Time{}
	epic := map[string]bool{}
	for _, f := range feats {
		if f.Parent != "" {
			epic[f.Parent] = true
		}
		// A rejected or superseded "fix" (not a bug after all) is no escaped
		// defect of what it named (C16).
		if f.Fixes != "" && f.State != domain.StateRejected && f.State != domain.StateSuperseded {
			fixes[f.Fixes] = append(fixes[f.Fixes], f.CreatedAt)
		}
	}
	decs, err := store.LoadAllDecisions(repoDir)
	if err != nil {
		return stats.ProcessReport{}, err
	}
	var markers []stats.Marker
	for _, d := range decs {
		if slices.Contains(d.Facets, "process") && slices.Contains(d.Components, "metric") {
			markers = append(markers, stats.Marker{ID: d.ID, Title: d.Title, At: d.CreatedAt})
		}
	}
	var samples []stats.ProcessSample
	for _, id := range ids {
		if epic[id] { // its own anchor is orchestration only: out, counted (C12)
			samples = append(samples, stats.ProcessSample{ID: id, CompletedAt: completed[id], Excluded: "epic"})
			continue
		}
		body, err := os.ReadFile(filepath.Join(repoDir, "content", "features", id, "result.md"))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return stats.ProcessReport{}, fmt.Errorf("process panel: read the result of %s: %w", id, err)
		}
		c, u, ok := cost.AnchorCounters(string(body))
		if !ok || c == (cost.Counters{}) {
			samples = append(samples, stats.ProcessSample{ID: id, CompletedAt: completed[id], Excluded: "no_cost"})
			continue
		}
		has, derrotas := lint.GauntletRounds(body)
		samples = append(samples, stats.ProcessSample{
			ID:           id,
			CompletedAt:  completed[id],
			Raw:          [4]int64{c.Input, c.CacheCreation, c.CacheRead, c.Output},
			Unattributed: [4]int64{u.Input, u.CacheCreation, u.CacheRead, u.Output},
			HasGauntlet:  has,
			Returned:     derrotas > 0,
			Fixes:        fixes[id],
		})
	}
	return stats.ComputeProcess(samples, markers, now), nil
}
