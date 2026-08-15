// Package spectmpl holds the canonical spec template that cerne scaffolds into
// a new feature and asks the LLM to fill. It is the single source of truth for
// the spec structure, shared by the stub writer (internal/app) and the
// spec-draft prompt (internal/cli) so the two never drift.
//
// The section order is load-bearing, not cosmetic: the Problema and the
// acceptance criteria sit at the top and Verificação near the end, so the most
// testable, decision-bearing content lands where long-context models attend
// best (primacy/recency) rather than buried in the middle. The rationale and
// its sources are registered in the cerne backend as references (R-0001…).
package spectmpl

import (
	"strings"

	"gitlab.com/rockt/cerne/internal/lang"
)

// Sections are the canonical spec sections, in template order.
var Sections = []string{
	"Problema",
	"Objetivo e critérios de aceitação",
	"Decisões do design",
	"Desenho",
	"Verificação",
	"Execução recomendada (agente)",
	"Fora de escopo",
}

// AmbiguityMarker is the token an author (or the LLM) leaves in place of a
// guess when something is unresolved, so it surfaces instead of becoming a
// silent wrong assumption.
const AmbiguityMarker = "[ESCLARECER:"

// Levels is the generic suggested-capability vocabulary for the **Nível:**
// metadata line (F-0194): the model class the implementation likely needs,
// never a vendor model name (users run different LLMs; names rot). leve =
// mechanical change, existing pattern, one package; padrão = multi-file with
// tests over established patterns; avançado = new subsystem, open design
// decisions, cross-cutting change. Shared by the template Body and the spec
// draft prompt so the two never drift.
var Levels = []string{"leve", "padrão", "avançado"}

// Body is the spec template skeleton: the canonical section headings in order,
// each with inline authoring guidance as HTML comments. It carries no
// frontmatter and no title heading — callers prepend those. doccheck treats
// markdown headings and HTML-comment guidance as scaffold, so a freshly
// scaffolded spec still reads as a stub (gates stay closed) until a section is
// filled with real content.
const Body = `<!-- COMO USAR: preencha cada seção e remova estas linhas-guia. Marque qualquer
incerteza com [ESCLARECER: pergunta específica] e não prossiga adivinhando. Antes de
cerne feature approve: nenhum [ESCLARECER] restante; critérios testáveis; nada ambíguo. -->

<!-- METADADOS (preencha como linhas **Status:** / **Tipo:** / **Origem:** / **Nível:**):
Status: Rascunho (AAAA-MM-DD) — spec para revisão.
Tipo: Novo recurso | Corretivo | Refactor — o caráter da mudança em uma linha.
Origem: o que disparou (relato de agente, dor recorrente, decisão do usuário + data).
Nível: leve | padrão | avançado — capacidade de modelo sugerida para implementar + 1 frase
de porquê (leve = mecânica, padrão existente, 1 pacote; padrão = multi-arquivo com testes;
avançado = design novo / cross-cutting). Genérico — nunca nome de modelo/vendor. -->

## Problema
<!-- O que falta ou quebra HOJE, ancorado em código real (arquivo.go:linha). Por que agora;
o custo de não resolver. Só o problema — a solução vem depois. -->

## Objetivo e critérios de aceitação
<!-- Uma frase de objetivo; depois critérios NUMERADOS e testáveis (1 critério : 1 teste).
EARS para comportamento: QUANDO <condição> O SISTEMA DEVE <comportamento observável>.
Invariantes estruturais entram como asserção direta e mensurável. -->

## Decisões do design
<!-- Tabela | Fork (a questão) | Decisão | Razão |; registre também as alternativas rejeitadas. -->

## Desenho
<!-- Por módulo/pacote, ancorado em arquivos reais, com contratos explícitos (assinaturas,
schemas, endpoints, eventos). Inclua o HOW — não o omita. -->

## Verificação
<!-- Plano de testes (TDD), 1:1 com os critérios de aceitação. Padrão de parada (Gauntlet),
uma das três formas: Barra (referência nomeada + acesso) + Método (comparação cega, crítico
fresco) + Saída (vencer/empatar; parar perdendo só por encerramento humano); "Barra: não
aplicável (<justificativa confirmada>) — padrão alternativo: <teste/benchmark/invariante>";
ou "Barra: dispensada por decisão humana". -->


## Execução recomendada (agente)
<!-- Capacidade de modelo sugerida (vocabulário genérico, coerente com a linha **Nível:**) e esforço.
Tarefas a manter no mesmo agente (contexto compartilhado) e por quê; tarefas separáveis. -->

## Fora de escopo
<!-- O que NÃO será feito agora e por quê (ou o que vira follow-up). -->
`

// Stub returns the spec body cerne scaffolds for a new feature: the title
// heading followed by the template Body. Callers prepend the cerne_id
// frontmatter.
func Stub(title string) string {
	return "# " + strings.TrimSpace(title) + "\n\n" + Body
}

// enMarkers swaps the marker words the guidance cites for the en vocabulary.
// Only the marker words change — the guidance prose stays pt-BR (translating
// the templates is its own feature; the words are what the approve gate and
// the filling agent key on).
var enMarkers = strings.NewReplacer("[ESCLARECER", "[CLARIFY", "[ESCLARECIDO", "[CLARIFIED")

// StubWithContext returns Stub(title) with the Problema section seeded with
// context — the creation-time summary that must survive the session that
// minted the feature (F-0120). The rest of the template stays for the author
// to fill. An empty context degrades to the plain Stub.
//
// The context is also the language signal (F-0182): an English context gets
// the guidance citing [CLARIFY: …], so the agent filling the spec authors
// markers in the doc's own language. Detection is scaffold-only — the parser
// accepts the whole catalog regardless (internal/clarify).
func StubWithContext(title, context string) string {
	context = strings.TrimSpace(context)
	if context == "" {
		return Stub(title)
	}
	next := "\n## " + Sections[1] + "\n"
	s := strings.Replace(Stub(title), next, "\n"+context+"\n"+next, 1)
	if lang.Detect(context) == "en" {
		s = enMarkers.Replace(s)
	}
	return s
}
