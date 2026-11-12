package app

import (
	"github.com/henriquesakata/cerne/internal/lint"
	"github.com/henriquesakata/cerne/internal/project"
)

// ProjectLint is the lint pass under the project's settings — the windows,
// the source ignores and the Gauntlet mode (F-0364) — the one entry point
// every lint surface shares (lint, lint --staged, push --dry-run, next).
func ProjectLint(repoDir string, cfg project.Config) ([]lint.Finding, error) {
	findings, err := lint.RunConfig(repoDir, cfg.StaleReadyWindowDays(), cfg.StaleWorkWindowHours(), cfg.SourceIgnore()...)
	if err != nil {
		return nil, err
	}
	return LintMode(findings, cfg.Gates), nil
}

// LintMode applies the project's Gauntlet mode to lint findings: in light
// mode spec-missing-bar and result-missing-gauntlet are dropped. The server
// calls it after its cache (the cache key carries no settings); everything
// else reaches it through ProjectLint.
func LintMode(findings []lint.Finding, g project.GatesConfig) []lint.Finding {
	return lint.DropGauntletRules(findings, g.GauntletEnabled())
}
