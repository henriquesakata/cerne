# Case 0037 — Resultado: template de plano (plantmpl)

tags: #result #cerne #cli #plantmpl #docs

**Status:** Entregue (2026-06-20) — código verde; referências a registrar no backend.

## O que entregou

- **`internal/plantmpl/plantmpl.go`** — template canônico de plano: 6 seções em ordem (`Estratégia`, `Sequência de tarefas`, `Dependências e ordem`, `Riscos e rollback`, `Restrições para o agente`, `Definição de pronto`), `Body` (lido como stub pelo `doccheck`) e `Stub(title)`. Reusa o marcador `[ESCLARECER:` da spec.
- **`internal/cli/plancmd.go`** — `cerne plan draft <id>`: injeta a spec aprovada como contexto e monta o prompt (seções em ordem + regras: teste-primeiro, rastreabilidade de critério, marcador de ambiguidade, right-sizing, definição de pronto binária). Registrado em `internal/cli/root.go`.
- **`internal/app/feature.go` + `internal/app/transition.go`** — `writePlanStub` materializado em `cerne feature approve` (no-op se já há plano real); o stub mantém o gate de `start` fechado.
- **`internal/cli/transitioncmd.go`** — pós-approve aponta o próximo passo (preencher o plano).
- **`internal/skills/cerne-authoring/SKILL.md`** — seção "Plan structure (the template cerne scaffolds)" + passo Plan do loop atualizado.

## Como foi embasado

Análise dos **125 planos do hub** (lacunas: rollback 7%, critérios espelhados 10%, guardrails/ambiguidade ausentes) + pesquisa em fontes confiáveis, registradas como referências `--informs` esta feature: Anthropic (Claude Code best practices; Effective context engineering; Building effective agents), GitHub Spec Kit (Spec→Plan→Tasks), Plan-and-Solve Prompting (ACL 2023), e as skills do Tech Lead's Club (tlc-spec-driven: right-sizing; spec-driven-eval: checagem binária). Ordenação por primacy/recency; right-sizing para evitar cerimônia; definição de pronto binária com rastreabilidade.

## Números de aceitação

- Suíte: **555 passed em 25 pacotes** (`go test ./... -count=1`), `go vet ./...` limpo.
- Critérios 1–4: cobertos por testes (`plantmpl_test.go`, `plancmd_test.go`, `transitioncmd_test.go:TestApproveScaffoldsPlanStub`). Critério 5: referências registradas no backend.
- `TestTransitionFullLifecycle` pré-existente segue verde — o scaffold no approve não afrouxa o gate de `start`.

## Débitos e próximos passos

- `cerne lint` não valida completude das seções do plano (nem parseia wiki-links) — follow-up.
- `cerne import` ainda não carrega `parent`.
- Avaliar um `cerne plan diagram` (espelhando `spec diagram`), se houver demanda.
