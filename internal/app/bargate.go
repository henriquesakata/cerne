package app

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/henriquesakata/cerne/internal/doccheck"
)

// ErrSpecMissingBar is the sentinel for an approve attempted over a spec that
// names no Gauntlet bar (F-0545). It is enforced in the shared transition core,
// so CLI and UI approve alike (F-0295), and is deliberately NOT bypassed by
// --force: a silent absence of a bar is the exact failure this gate exists to stop.
var ErrSpecMissingBar = errors.New("spec has no valid Gauntlet bar")

// CheckSpecBar refuses when the feature's spec names no Gauntlet bar in one of
// the three literal forms (F-0545 crit. 1). A missing spec passes — doc
// presence is the document gate's concern. The predicate is doccheck.SpecBar,
// shared with the lint rule and the approve readiness (F-0308).
func CheckSpecBar(repoDir, id string) error {
	data, err := os.ReadFile(filepath.Join(repoDir, "content", "features", id, "spec.md"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	// A stub spec is the document gate's refusal ("fill in the spec"), not this
	// one's — the bar error only makes sense over a spec someone actually wrote.
	if !doccheck.HasRealContent(data) {
		return nil
	}
	if _, err := doccheck.SpecBar(data); err != nil {
		// One line (F-0308 crit. 11): the three literal forms are taught by the
		// check, not repeated in every refusal.
		return fmt.Errorf("%w: %v — run 'cerne feature approve %s --check' for the accepted forms", ErrSpecMissingBar, err, id)
	}
	return nil
}

// ErrSpecMissingJudgeQuestion is the sentinel for an approve over a spec that
// names a bar but no "Pergunta ao juiz:" line (F-0381). Like ErrSpecMissingBar
// it lives in the shared transition core and --force does not cross it.
var ErrSpecMissingJudgeQuestion = errors.New(`spec has a bar but no judge question — add a "Pergunta ao juiz:" line under the bar`)

// CheckSpecJudgeQuestion refuses when the feature's spec names a bar that is
// not exempt and holds no written judge question (F-0381). Missing and stub
// specs pass, as in CheckSpecBar; a spec with no valid bar passes too — that is
// the bar gate's refusal. The predicate is doccheck.SpecJudgeQuestion, shared
// with the approve readiness.
func CheckSpecJudgeQuestion(repoDir, id string) error {
	data, err := os.ReadFile(filepath.Join(repoDir, "content", "features", id, "spec.md"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !doccheck.HasRealContent(data) {
		return nil
	}
	if _, err := doccheck.SpecJudgeQuestion(data); err != nil {
		return ErrSpecMissingJudgeQuestion
	}
	return nil
}
