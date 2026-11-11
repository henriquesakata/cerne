package app

import (
	"errors"
	"fmt"
	"strings"

	"github.com/henriquesakata/cerne/internal/lint"
	"github.com/henriquesakata/cerne/internal/store"
)

// ErrResultIncomplete is the sentinel for a complete attempted over a result
// the lint would flag right after (F-0303). Enforced in the shared transition
// core, so CLI and UI complete alike, and NOT bypassed by --force: once
// implemented the result is frozen out of .docs/, so this is the last moment
// the author can still fix it.
var ErrResultIncomplete = errors.New("result incompleto")

// CheckResultRecord refuses when the feature's result would be flagged by the
// lint's result rules (evidence per criterion, Gauntlet record) right after
// completion. Both call lint.CheckResult, so they cannot drift apart.
func CheckResultRecord(repoDir string, f store.Feature) error {
	findings := lint.CheckResult(repoDir, f)
	if len(findings) == 0 {
		return nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d pendência(s):", len(findings))
	for _, fi := range findings {
		b.WriteString("\n  - " + fi.Detail)
	}
	return fmt.Errorf("%w — %s", ErrResultIncomplete, b.String())
}
