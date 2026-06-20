# Case 0039 — Resultado: template de result (restmpl)

**Spec:** [spec.md](spec.md) — fonte dos critérios.
**Data:** 2026-06-20.

## Resultado

Entregue. O resultado (result.md) ganhou o template canônico que faltava, completando a família spec/plano/ADR/result; este próprio arquivo é escrito no novo template (dogfood).

## Aceitação

Cada critério da spec, binário, com evidência:

1. **ATENDIDO** — scaffold no `cerne feature start` + stub fechado: `internal/app/transition.go` (`ActionStart` → `writeResultStub`), `internal/app/feature.go:writeResultStub`; provado por `internal/cli/transitioncmd_test.go:TestStartScaffoldsResultStub` e `internal/restmpl/restmpl_test.go:TestStubReadsAsStub`.
2. **ATENDIDO** — `internal/restmpl/restmpl.go` expõe as 5 seções em ordem + `Stub`; `restmpl_test.go:TestSectionsCanonical`/`TestBodyHasSectionsInOrder`.
3. **ATENDIDO** — `cerne result draft <id>` injeta spec+plano e ensina as regras: `internal/cli/resultcmd.go:BuildResultDraftPrompt`; `resultcmd_test.go:TestBuildResultDraftPrompt` (+ byo-key + unknown-feature).
4. **ATENDIDO** — `internal/skills/cerne-authoring/SKILL.md` ganhou a seção "Result structure" e o passo Result atualizado.
5. **ATENDIDO** — referências registradas no backend (`--informs F-0039`).

Suíte: **572 passed em 27 pacotes** (`go test ./... -count=1`); `go vet ./...` limpo.

## O que mudou

- Novo pacote `internal/restmpl` (template + `Stub`, lido como stub pelo `doccheck`).
- Novo comando `cerne result draft <id>` (`internal/cli/resultcmd.go`), registrado em `internal/cli/root.go`.
- `cerne feature start` agora materializa `result.md` do template (`writeResultStub` em `feature.go`, chamado em `transition.go`); a mensagem do `start` aponta o próximo passo.
- `internal/skills/cerne-authoring/SKILL.md`: seção "Result structure" + passo Result.

## Débitos e follow-ups

- `cerne lint` não valida completude das seções do result — follow-up.
- Heurística de estado do importer (`status…superseded`) ainda colide; rastreada em tarefa à parte (false-positive visto na F-0038).
- Contagens de teste automáticas no result ficam fora de escopo.

## Aprendizados e desvios

A análise bruta sugeria 8 seções com várias tabelas; reduzido a 5 enxutas para honrar o right-sizing da família (mais seções = cerimônia). O result é o doc menos padronizado do mercado — a melhor âncora foi fechar o loop de aceitação (binária, 1:1, com evidência), reusando spec-driven-eval e Claude Code "evidência não afirmação". Sem desvios da spec.
