package app

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/store"
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

// F-0381 crit. 6: start and complete never look at the question — a feature
// approved before the gate existed moves on without it.
func TestStartAndCompleteIgnoreJudgeQuestion(t *testing.T) {
	t.Run("start", func(t *testing.T) {
		repoDir, projDir, _ := newFeatureBackend(t)
		if err := store.SaveFeature(repoDir, store.Feature{ID: "F-0001", Title: "t", State: domain.StateApproved}); err != nil {
			t.Fatal(err)
		}
		writeSpecFixture(t, repoDir, "F-0001", specBarOnly)
		copyPath := store.ApprovedSpecPath(repoDir, "F-0001")
		if err := os.MkdirAll(filepath.Dir(copyPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(copyPath, []byte(specBarOnly), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(FeatureDocPath(repoDir, "F-0001", "plan.md"), []byte("# p\n\nPlano real.\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := gitback.CommitAndPush(repoDir, "seed"); err != nil {
			t.Fatal(err)
		}
		if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionStart, "", "", nil); err != nil {
			t.Fatalf("start: %v", err)
		}
	})
	t.Run("complete", func(t *testing.T) {
		repoDir, projDir, _ := newFeatureBackend(t)
		seedCompletable(t, repoDir, specNoBar+"\n## Verificação\n\nBarra: ref — acesso\n", resultBody(gateCite1234, testGauntletTail))
		if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionComplete, "", "", nil); err != nil {
			t.Fatalf("complete: %v", err)
		}
	})
}

const specMisplaced = "# t\n\nProblema real.\n\nPergunta ao juiz: qual é mais claro?\n\n## Verificação\n\nBarra: referência nomeada — acesso\n"

// Round 2, point 1: the refusal says where the line goes and how to see the
// form — in the project's documents language.
func TestJudgeQuestionRefusalSaysHowToFix(t *testing.T) {
	for name, tc := range map[string]struct{ lang, spec, want string }{
		"pt": {"", specBarOnly, `spec has a bar but no judge question — add under the bar, in ## Verificação: "Pergunta ao juiz: <a pergunta que o juiz cego responde>" — run 'cerne feature approve F-0001 --check'`},
		"en": {"en", "# t\n\nReal problem.\n\n## Verification\n\nBar: reference — access\n", `spec has a bar but no judge question — add under the bar, in ## Verification: "Question for the judge: <the question the blind judge answers>" — run 'cerne feature approve F-0001 --check'`},
	} {
		t.Run(name, func(t *testing.T) {
			repoDir, projDir, _ := newFeatureBackend(t)
			if tc.lang != "" {
				if err := store.SaveProject(repoDir, store.Project{Name: "Demo", DocsLanguage: tc.lang}); err != nil {
					t.Fatal(err)
				}
			}
			seedCandidateSpec(t, repoDir, "F-0001", tc.spec)
			_, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionApprove, "", "", nil)
			if !errors.Is(err, ErrSpecMissingJudgeQuestion) || err.Error() != tc.want {
				t.Fatalf("err = %v\nwant %s", err, tc.want)
			}
			imps := hardOnly(ApproveReadiness(repoDir, projDir, "F-0001"))
			if len(imps) != 1 || imps[0].Code != ImpedimentJudgeQuestionMissing || strings.Contains(imps[0].Message, "run 'cerne") || !strings.Contains(imps[0].Message, "add under the bar") {
				t.Fatalf("impediments = %+v", imps)
			}
		})
	}
}

// Round 2, point 2: a question outside Verificação is told apart from a
// missing one, in the approve and in the readiness.
func TestJudgeQuestionMisplaced(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	seedCandidateSpec(t, repoDir, "F-0001", specMisplaced)
	const want = "judge question found outside ## Verificação (line 5) — move it under the bar"
	_, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionApprove, "", "", nil)
	if !errors.Is(err, ErrSpecMisplacedJudgeQuestion) || err.Error() != want || strings.Contains(err.Error(), "no judge question") {
		t.Fatalf("err = %v, want %q", err, want)
	}
	imps := hardOnly(ApproveReadiness(repoDir, projDir, "F-0001"))
	if len(imps) != 1 || imps[0].Code != ImpedimentJudgeQuestionMisplaced || imps[0].Message != want || !imps[0].Hard {
		t.Fatalf("impediments = %+v", imps)
	}
}

// Round 2, point 4: what the approve accepts is readable and on the event.
func TestAcceptedJudgeQuestion(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	seedCandidateSpec(t, repoDir, "F-0001", specBarAndQ)
	if q, exempt, err := AcceptedJudgeQuestion(repoDir, "F-0001"); err != nil || exempt || q != "qual dos dois é mais claro?" {
		t.Fatalf("AcceptedJudgeQuestion = %q, %v, %v", q, exempt, err)
	}
	out, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionApprove, "", "", nil)
	if err != nil || out.JudgeQuestion != "qual dos dois é mais claro?" {
		t.Fatalf("outcome = %+v, %v", out, err)
	}
	evs, _ := store.ReadEvents(repoDir)
	var got string
	for _, e := range evs {
		if e.Type == "feature.approved" {
			got = e.Payload["judge_question"]
		}
	}
	if got != "qual dos dois é mais claro?" {
		t.Fatalf("approve event judge_question = %q", got)
	}

	repo2, _, _ := newFeatureBackend(t)
	seedCandidateSpec(t, repo2, "F-0001", specBarExempt)
	if q, exempt, err := AcceptedJudgeQuestion(repo2, "F-0001"); err != nil || !exempt || q != "" {
		t.Fatalf("exempt: %q, %v, %v", q, exempt, err)
	}
}

// hardOnly drops the card's soft impediments (untagged fixtures).
func hardOnly(imps []ApproveImpediment, err error) []ApproveImpediment {
	var out []ApproveImpediment
	for _, im := range imps {
		if im.Hard && err == nil {
			out = append(out, im)
		}
	}
	return out
}
