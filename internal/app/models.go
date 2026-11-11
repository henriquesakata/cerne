package app

import (
	"context"
	"errors"
	"time"

	"github.com/henriquesakata/cerne/internal/llm"
	"github.com/henriquesakata/cerne/internal/models"
	"github.com/henriquesakata/cerne/internal/project"
)

// ErrNoModelProvider: the project reaches no provider (delegation, or no
// .cernerc at all) — the catalog refresh queries nothing and is not a failure
// (F-0311 criterion 17).
var ErrNoModelProvider = errors.New("no provider configured to list models")

// RefreshModels runs one catalog refresh for the project at projDir, shared by
// the ui boot and `cerne models refresh`: it picks the provider (persisted
// preference → the only configured one → none), lists its models and folds
// the listing into the global catalog. Any failure leaves the catalog as it
// was. The 24h window is the caller's call (the CLI's --force skips it).
func RefreshModels(ctx context.Context, projDir string, now time.Time) (models.Diff, error) {
	dir, err := project.ConfigDir()
	if err != nil {
		return models.Diff{}, err
	}
	cfg, _, err := project.LoadConfig(projDir)
	if err != nil && !errors.Is(err, project.ErrNoProject) {
		return models.Diff{}, err
	}
	cat, _ := models.Load(dir)
	provider := llm.PickProvider(cat.PreferredProvider, llm.ConfiguredProviders(cfg.LLM))
	if provider == "" {
		return models.Diff{}, ErrNoModelProvider
	}
	list := func(ctx context.Context) ([]models.Entry, error) { return llm.ListModels(ctx, cfg.LLM) }
	return models.Refresh(ctx, dir, provider, list, now)
}
