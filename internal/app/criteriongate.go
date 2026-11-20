package app

import (
	"errors"
	"fmt"
	"strings"

	"github.com/henriquesakata/cerne/internal/lint"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
)

// ErrCriterionTestMissing is the sentinel for a complete attempted while a
// criterion has no marked test or a mark points at a missing criterion
// (F-0473). Like ErrResultIncomplete it is not bypassed by --force.
var ErrCriterionTestMissing = errors.New("criteria without a marked test")

// ScanProjectTests reads the test marks of the project at projDir under its
// .cernerc test_roots / test_patterns (F-0473).
func ScanProjectTests(projDir string, cfg project.Config) lint.TestIndex {
	return lint.ScanTestMarks(projDir, cfg.TestRoots, cfg.TestPatterns)
}

// CheckCriterionTests refuses when the entity has criterion-without-test or
// orphan-criterion-test findings (F-0473 criterion 6), listing what is pending
// with the form of the mark. Inert when lint.criterion_tests is off (criterion
// 10) or the scan finds no test file at all (criterion 8).
func CheckCriterionTests(repoDir, projDir string, f store.Feature) error {
	if !project.CriterionTestsEnabled(projDir) {
		return nil
	}
	events, err := store.ReadEvents(repoDir)
	if err != nil {
		return nil // the gate never blocks on an unreadable log
	}
	cfg, _, err := project.LoadConfig(projDir)
	if err != nil {
		cfg = project.Config{}
	}
	findings := lint.CheckCriterionTests(repoDir, f, events, ScanProjectTests(projDir, cfg))
	if len(findings) == 0 {
		return nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d problem(s):", len(findings))
	for _, fi := range findings {
		b.WriteString("\n  - " + fi.Detail)
	}
	b.WriteString("\n— mark each proving test with a comment `" + f.ID + " crit. N` (a bug: `" + f.ID + "`), or disable the rule with `cerne settings set lint.criterion_tests false`")
	return fmt.Errorf("%w — %s", ErrCriterionTestMissing, b.String())
}
