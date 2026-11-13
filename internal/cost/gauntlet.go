package cost

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/henriquesakata/cerne/internal/doccheck"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/stats"
	"github.com/henriquesakata/cerne/internal/store"
)

// MinGauntletSample is how many measured features a median needs before the
// advice quotes it (F-0390): below it, no number.
const MinGauntletSample = 3

// GauntletSample is the measured Gauntlet cost over a set of features: how
// many, and — only when Enough — the median critic + judge spend in weighted
// tokens and the median share of the feature's own cost it took.
type GauntletSample struct {
	Features     int
	Enough       bool
	MedianTokens float64
	MedianShare  float64
}

// GauntletStats is the sample over every measured feature, and per spec level
// (leve, padrão, avançado; a spec without one counts only overall).
type GauntletStats struct {
	GauntletSample
	ByLevel map[string]GauntletSample
}

// GauntletCostStats measures what the Gauntlet cost in this project (F-0390):
// over every implemented or done feature whose stamped "## Custo" anchor
// carries a critic or judge role (F-0384), their weighted tokens (the hub
// weights of the process panel) and that spend's share of the feature's own.
// It reads the anchors only — never the transcripts — so the approve check
// stays cheap.
// ponytail: every implemented/done result is read per call (~400 results per
// --check on cerne's own backend); cache the stats by backend HEAD if the
// check gets slow.
func GauntletCostStats(repoDir string) (GauntletStats, error) {
	features, err := store.LoadAllFeatures(repoDir)
	if err != nil {
		return GauntletStats{}, err
	}
	type point struct{ tokens, share float64 }
	var all []point
	byLevel := map[string][]point{}
	for _, f := range features {
		if f.State != domain.StateImplemented && f.State != domain.StateDone {
			continue
		}
		a, ok := readAnchor(repoDir, f.ID)
		critic, hasCritic := a.ByRole["critic"]
		judge, hasJudge := a.ByRole["judge"]
		total := Weighted(a.Counters)
		if !ok || (!hasCritic && !hasJudge) || total == 0 {
			continue
		}
		p := point{Weighted(critic.add(judge)), 0}
		p.share = p.tokens / total
		all = append(all, p)
		spec, err := os.ReadFile(filepath.Join(repoDir, "content", "features", f.ID, "spec.md"))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return GauntletStats{}, err
		}
		if level, ok := doccheck.SpecLevel(spec); ok {
			byLevel[level] = append(byLevel[level], p)
		}
	}
	sample := func(ps []point) GauntletSample {
		s := GauntletSample{Features: len(ps), Enough: len(ps) >= MinGauntletSample}
		if s.Enough {
			tokens, shares := make([]float64, len(ps)), make([]float64, len(ps))
			for i, p := range ps {
				tokens[i], shares[i] = p.tokens, p.share
			}
			s.MedianTokens, s.MedianShare = stats.Median(tokens), stats.Median(shares)
		}
		return s
	}
	out := GauntletStats{GauntletSample: sample(all), ByLevel: map[string]GauntletSample{}}
	for level, ps := range byLevel {
		out.ByLevel[level] = sample(ps)
	}
	return out, nil
}

// Weighted is c in input-token units, by the hub weights (stats.Weighted).
func Weighted(c Counters) float64 {
	return stats.Weighted([4]int64{c.Input, c.CacheCreation, c.CacheRead, c.Output})
}
