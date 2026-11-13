package app

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
)

const (
	specBarOnly   = "# t\n\nProblema real.\n\n## Verificação\n\nBarra: referência nomeada — acesso\nMétodo: m\nSaída: s\n"
	specBarAndQ   = specBarOnly + "Pergunta ao juiz: qual dos dois é mais claro?\n"
	specBarExempt = "# t\n\nProblema real.\n\n## Verificação\n\nBarra: não aplicável (texto puro) — padrão alternativo: teste\n"
)

func impCodes(imps []ApproveImpediment) []string {
	var c []string
	for _, im := range imps {
		c = append(c, im.Code)
	}
	return c
}

// F-0381 crit. 1, 2: the gate refuses a bar with no question, accepts it with
// one, and owes nothing to an exempt bar; the readiness lists the same.
func TestCheckSpecJudgeQuestion(t *testing.T) {
	for name, tc := range map[string]struct {
		spec string
		bad  bool
	}{"no question": {specBarOnly, true}, "question": {specBarAndQ, false}, "exempt bar": {specBarExempt, false}} {
		t.Run(name, func(t *testing.T) {
			repoDir, projDir, _ := newFeatureBackend(t)
			seedCandidateSpec(t, repoDir, "F-0001", tc.spec)
			imps, err := ApproveReadiness(repoDir, projDir, "F-0001")
			if err != nil {
				t.Fatal(err)
			}
			if got := slices.Contains(impCodes(imps), ImpedimentJudgeQuestionMissing); got != tc.bad {
				t.Fatalf("impediments %v: judge-question-missing = %v, want %v", impCodes(imps), got, tc.bad)
			}
			_, err = TransitionFeature(repoDir, projDir, "F-0001", domain.ActionApprove, "", "", nil)
			if tc.bad {
				if !errors.Is(err, ErrSpecMissingJudgeQuestion) || strings.Contains(err.Error(), "\n") {
					t.Fatalf("approve err = %v, want one-line ErrSpecMissingJudgeQuestion", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("approve: %v", err)
			}
		})
	}
}

// F-0381 crit. 3: light mode charges neither the bar nor the question.
func TestJudgeQuestion_LightMode(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	saveGauntlet(t, projDir, "off")
	seedCandidateSpec(t, repoDir, "F-0001", specBarOnly)
	imps, err := ApproveReadiness(repoDir, projDir, "F-0001")
	if err != nil || slices.Contains(impCodes(imps), ImpedimentJudgeQuestionMissing) {
		t.Fatalf("readiness = %v, %v; want no judge-question impediment", impCodes(imps), err)
	}
	if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionApprove, "", "", nil); err != nil {
		t.Fatalf("light approve: %v", err)
	}
}

// A stub spec is the document gate's refusal, not this one's.
func TestCheckSpecJudgeQuestion_StubAndMissingPass(t *testing.T) {
	repo := t.TempDir()
	if err := CheckSpecJudgeQuestion(repo, "F-0001"); err != nil {
		t.Fatalf("missing spec: %v", err)
	}
	writeSpecFixture(t, repo, "F-0001", "# t\n\n## Verificação\n<!-- guia -->\n")
	if err := CheckSpecJudgeQuestion(repo, "F-0001"); err != nil {
		t.Fatalf("stub spec: %v", err)
	}
}

// F-0381 crit. 6: the gate is on every approve, so re-approving an approved
// feature whose spec changed asks for the question; a spec already approved is
// untouched until then.
func TestReapproveNeedsJudgeQuestion(t *testing.T) {
	repoDir, projDir, _ := approveFixture(t)
	if err := SaveFeatureDoc(repoDir, projDir, "F-0001", "spec", approvedSpecA+"\nCritério novo.\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionApprove, "", "", nil); err != nil {
		t.Fatalf("re-approve with question: %v", err)
	}
	noQ := strings.Replace(approvedSpecA, "Pergunta ao juiz:", "Nota:", 1) + "\nOutro critério.\n"
	if err := SaveFeatureDoc(repoDir, projDir, "F-0001", "spec", noQ); err != nil {
		t.Fatal(err)
	}
	if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionApprove, "", "", nil); !errors.Is(err, ErrSpecMissingJudgeQuestion) {
		t.Fatalf("re-approve without question err = %v, want ErrSpecMissingJudgeQuestion", err)
	}
}
