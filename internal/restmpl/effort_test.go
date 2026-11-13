package restmpl

import (
	"testing"

	"github.com/henriquesakata/cerne/internal/doclang"
)

// F-0391 crit. 7: the effort filter uses only what the agent declared on the
// result's Agente line — the shapes below are copied from real results.
func TestDeclaredEffort(t *testing.T) {
	cases := []struct{ name, result, want string }{
		{"declared", "# T\n\n**Agente:** claude-opus-5-5 · esforço high\n\n## Resultado\n", "high"},
		{"unknown said", "**Agente:** claude-opus-5-5 · esforço desconhecido\n", ""},
		{"no effort on the line", "**Agente:** claude-sonnet-5-5\n", ""},
		{"human", "**Agente:** humano\n", ""},
		{"inline after Spec and Data", "**Spec:** ./spec.md · **Data:** 2026-11-07 · **Agente:** claude-opus-5-5 · esforço high (Dev e crítico); Tech Lead claude-opus-5-5\n", "high"},
		{"word before esforço", "**Agente:** claude-sonnet-5-5 · dev, esforço médio\n", "médio"},
		{"first declared wins", "**Agente:** claude-sonnet-5-5 · esforço low (dev); claude-opus-5-5 · esforço desconhecido (tech lead)\n", "low"},
		{"decomposed cedilla", "**Agente:** claude-opus-5-5 · esforço xhigh\n", "xhigh"},
		{"English", "**Agent:** claude-opus-5-5 · effort Medium\n", "medium"},
		{"English unknown", "**Agent:** claude-opus-5-5 · effort unknown\n", ""},
		{"no line", "# T\n\n## Resultado\n\nfeito\n", ""},
		{"only metadata counts", "# T\n\n## Aceitação\n\n- linha **Agente:** x · esforço high no scaffold\n", ""},
		{"not a level: a word", "**Agente:** claude-opus-5-5 · esforço do dev\n", ""},
		{"not a level: n/d", "**Agente:** claude-opus-5-5 · esforço n/d\n", ""},
		{"not a level: a longer word", "**Agent:** claude-opus-5-5 · effort highest\n", ""},
		{"the first effort decides, even unknown", "**Agente:** x · esforço desconhecido (dev); y · esforço high (TL)\n", ""},
		{"max", "**Agente:** claude-opus-5-5 · esforço max\n", "max"},
		{"pt-BR levels stay as written", "**Agente:** claude-opus-5-5 · esforço Alto\n", "alto"},
		{"baixo", "**Agente:** claude-opus-5-5 · esforço baixo.\n", "baixo"},
		{"decomposed médio is médio", "**Agente:** claude-opus-5-5 · esforço me\u0301dio\n", "médio"},
		{"fresh stub", Stub("T"), ""},
		{"fresh English stub", StubIn(doclang.EN, "T"), ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := DeclaredEffort([]byte(c.result)); got != c.want {
				t.Errorf("DeclaredEffort = %q, want %q", got, c.want)
			}
		})
	}
}
