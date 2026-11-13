package app

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/henriquesakata/cerne/internal/doccheck"
	"github.com/henriquesakata/cerne/internal/doclang"
	"github.com/henriquesakata/cerne/internal/store"
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
// names a bar but no "Pergunta ao juiz:" line (F-0381); ErrSpecMisplacedJudgeQuestion,
// for one whose line sits outside the Verificação section. Like ErrSpecMissingBar
// they live in the shared transition core and --force does not cross them.
var (
	ErrSpecMissingJudgeQuestion   = errors.New("spec has a bar but no judge question")
	ErrSpecMisplacedJudgeQuestion = errors.New("judge question found outside the verification section")
)

// judgeQuestionError carries a sentinel behind a message written for the
// author: where the line goes, in the documents' language.
type judgeQuestionError struct {
	sentinel error
	msg      string
}

func (e *judgeQuestionError) Error() string { return e.msg }
func (e *judgeQuestionError) Unwrap() error { return e.sentinel }

// judgeQuestionProblem turns doccheck's verdict on a spec into the author's
// message (F-0381): how to add the line, or that it is in the wrong place. id
// non-empty ends the missing message with the pointer to the check, which
// lists the accepted forms; the readiness leaves it out (it IS the check).
func judgeQuestionProblem(err error, lang doclang.Lang, id string) error {
	heading := doclang.Sections(doclang.Spec, lang)[4]
	var misplaced *doccheck.MisplacedJudgeQuestion
	if errors.As(err, &misplaced) {
		return &judgeQuestionError{ErrSpecMisplacedJudgeQuestion, fmt.Sprintf("judge question found outside ## %s (line %d) — move it under the bar", heading, misplaced.Line)}
	}
	label, slot := "Pergunta ao juiz", "a pergunta que o juiz cego responde"
	if lang == doclang.EN {
		label, slot = "Question for the judge", "the question the blind judge answers"
	}
	msg := fmt.Sprintf(`%s — add under the bar, in ## %s: "%s: <%s>"`, ErrSpecMissingJudgeQuestion, heading, label, slot)
	if id != "" {
		msg += fmt.Sprintf(" — run 'cerne feature approve %s --check'", id)
	}
	return &judgeQuestionError{ErrSpecMissingJudgeQuestion, msg}
}

// CheckSpecJudgeQuestion refuses when the feature's spec names a bar that is
// not exempt and holds no written judge question under Verificação (F-0381).
// Missing and stub specs pass, as in CheckSpecBar; a spec with no valid bar
// passes too — that is the bar gate's refusal. The predicate is
// doccheck.SpecJudgeQuestion, shared with the approve readiness.
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
		return judgeQuestionProblem(err, store.DocsLanguage(repoDir), id)
	}
	return nil
}

// AcceptedJudgeQuestion returns the judge question an approve of feature id
// accepts (F-0381): "" with exempt true when the bar is "não aplicável" or
// waived (none is charged), "" when the spec is missing or holds none.
func AcceptedJudgeQuestion(repoDir, id string) (question string, exempt bool, err error) {
	spec, err := os.ReadFile(FeatureDocPath(repoDir, id, "spec.md"))
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	question, _ = doccheck.SpecJudgeQuestion(spec)
	return question, doccheck.BarExempt(spec), nil
}
