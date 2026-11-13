package doclang

import "testing"

func TestJudgeQuestionLine(t *testing.T) {
	for _, tt := range []struct {
		line   string
		inCode bool
		want   string
		ok     bool
	}{
		{"Pergunta ao juiz: qual é melhor?", false, "qual é melhor?", true},
		{"Question for the judge: which is better?", false, "which is better?", true},
		{"**Pergunta ao juiz:** qual?", false, "qual?", true},
		{"- Pergunta ao juiz: qual?", false, "qual?", true},
		{"Pergunta ao juiz:", false, "", true},
		{"Pergunta ao juiz: qual?", true, "", false},
		{"    Pergunta ao juiz: qual?", false, "", false},
		{"A pergunta ao juiz: qual?", false, "", false},
	} {
		got, ok := JudgeQuestionLine(tt.line, tt.inCode)
		if got != tt.want || ok != tt.ok {
			t.Errorf("JudgeQuestionLine(%q, %v) = %q, %v; want %q, %v", tt.line, tt.inCode, got, ok, tt.want, tt.ok)
		}
	}
}

func TestIsVerificationSection(t *testing.T) {
	for h, want := range map[string]bool{"Verificação": true, "Verification": true, " verification ": true, "Desenho": false} {
		if got := IsVerificationSection(h); got != want {
			t.Errorf("IsVerificationSection(%q) = %v, want %v", h, got, want)
		}
	}
}
