package cli

import (
	"errors"
	"strings"
	"testing"
)

// judgeSpec is a real spec with the given Verificação body.
func judgeSpec(id, verif string) string {
	return "---\ncerne_id: " + id + "\n---\n\n# Título\n\n## Problema\n\nProblema real descrito aqui.\n\n## Verificação\n\n" + verif
}

func judgeProject(t *testing.T, verif string) (projDir, id string) {
	t.Helper()
	projDir, _, repoDir := setupProject(t)
	id = newFeature(t, projDir, "Feature da pergunta", 1)
	writeDoc(t, repoDir, id, "spec.md", judgeSpec(id, verif))
	commitBackend(t, repoDir)
	return projDir, id
}

// F-0381 round 2, point 1: the check lists the accepted forms of the line.
func TestApproveCheckTeachesTheJudgeQuestionForms(t *testing.T) {
	projDir, id := judgeProject(t, "Barra: fixture de teste — referência nomeada\n")
	out, err := execCerne(t, "feature", "approve", id, "--check", "--dir", projDir)
	if !errors.Is(err, errCheckImpediments) {
		t.Fatalf("err = %v\n%s", err, out)
	}
	for _, want := range []string{"judge-question-missing", "Pergunta ao juiz: <a pergunta que o juiz cego responde>", "Question for the judge: <the question the blind judge answers>"} {
		if !strings.Contains(out, want) {
			t.Errorf("check output lacks %q:\n%s", want, out)
		}
	}
	out, err = execCerne(t, "feature", "approve", id, "--dir", projDir)
	if err == nil || !strings.Contains(err.Error(), `add under the bar, in ## Verificação: "Pergunta ao juiz: <a pergunta que o juiz cego responde>" — run 'cerne feature approve `+id+` --check'`) {
		t.Fatalf("approve err = %v\n%s", err, out)
	}
}

// Point 2: a question outside Verificação is a distinct refusal, in both.
func TestApproveCheckJudgeQuestionMisplaced(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	id := newFeature(t, projDir, "Feature fora do lugar", 1)
	writeDoc(t, repoDir, id, "spec.md", "---\ncerne_id: "+id+"\n---\n\n# Título\n\nPergunta ao juiz: qual é mais claro?\n\n## Verificação\n\nBarra: fixture de teste — referência nomeada\n")
	commitBackend(t, repoDir)
	const want = "judge question found outside ## Verificação (line 7) — move it under the bar"
	out, err := execCerne(t, "feature", "approve", id, "--check", "--dir", projDir)
	if !errors.Is(err, errCheckImpediments) || !strings.Contains(out, want) || strings.Contains(out, "no judge question") {
		t.Fatalf("check err = %v\n%s", err, out)
	}
	if out, err = execCerne(t, "feature", "approve", id, "--dir", projDir); err == nil || err.Error() != want {
		t.Fatalf("approve err = %v\n%s", err, out)
	}
}

// Point 3: an exemption says why no question is charged, in check and approve.
func TestApproveSaysWhyNoJudgeQuestion(t *testing.T) {
	const na = "no judge question is charged"
	t.Run("bar not applicable", func(t *testing.T) {
		projDir, id := judgeProject(t, "Barra: não aplicável (texto puro) — padrão alternativo: teste\n")
		out, err := execCerne(t, "feature", "approve", id, "--check", "--dir", projDir)
		if err != nil || !strings.Contains(out, "bar not applicable/waived: "+na) {
			t.Fatalf("check: %v\n%s", err, out)
		}
		out, err = execCerne(t, "feature", "approve", id, "--dir", projDir)
		if err != nil || !strings.Contains(out, "bar not applicable/waived: "+na) {
			t.Fatalf("approve: %v\n%s", err, out)
		}
	})
	// Light mode says it even when the spec happens to carry a bar.
	t.Run("light mode, spec with a bar", func(t *testing.T) {
		projDir, id := judgeProject(t, "Barra: fixture de teste — referência nomeada\n")
		setGauntlet(t, projDir, "off")
		out, err := execCerne(t, "feature", "approve", id, "--check", "--dir", projDir)
		if err != nil || !strings.Contains(out, "light mode: no Gauntlet bar or judge question is charged") || strings.Contains(out, "Approving accepts its bar") {
			t.Fatalf("check: %v\n%s", err, out)
		}
	})
	t.Run("light mode", func(t *testing.T) {
		projDir, id := judgeProject(t, "Só testes.\n")
		setGauntlet(t, projDir, "off")
		out, err := execCerne(t, "feature", "approve", id, "--check", "--dir", projDir)
		if err != nil || !strings.Contains(out, "light mode: no Gauntlet bar or judge question is charged") {
			t.Fatalf("check: %v\n%s", err, out)
		}
		out, err = execCerne(t, "feature", "approve", id, "--dir", projDir)
		if err != nil || !strings.Contains(out, "light mode: no Gauntlet bar or judge question is charged") {
			t.Fatalf("approve: %v\n%s", err, out)
		}
	})
}

// Point 4: the approver sees the accepted question, right after the bar.
func TestApproveShowsAcceptedJudgeQuestion(t *testing.T) {
	projDir, id := judgeProject(t, "Barra: fixture de teste — referência nomeada\nPergunta ao juiz: qual é mais claro?\n")
	out, err := execCerne(t, "feature", "approve", id, "--check", "--dir", projDir)
	if err != nil || !strings.Contains(out, "Approving accepts its judge question:\n  qual é mais claro?") {
		t.Fatalf("check: %v\n%s", err, out)
	}
	out, err = execCerne(t, "feature", "approve", id, "--dir", projDir)
	if err != nil || !strings.Contains(out, "Accepted bar: Barra: fixture de teste — referência nomeada\nAccepted judge question: qual é mais claro?\n") {
		t.Fatalf("approve: %v\n%s", err, out)
	}
}
