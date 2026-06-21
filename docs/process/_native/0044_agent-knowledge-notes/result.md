# Case 0044 — Resultado: note como entidade

**Spec:** [spec.md](spec.md) — fonte dos critérios.
**Data:** 2026-06-21.

## Resultado

Entregue. O conhecimento durável que um agente adquire virou entidade de primeira classe no cerne — `note` (`N-NNNN`, ciclo ativo/substituído), corpo freeform, projetada onde os agentes leem, indexada e linkável. Fecha o gap de origem: conhecimento de time que vazava para a memória local do Claude Code (por-usuário, por-máquina) agora tem lar no guardião, compartilhado entre máquinas e membros. Segundo tipo de entidade novo desde a fundação (após o runbook, F-0040), e mais enxuto: sem template, sem `draft`, sem importer.

## Aceitação

Cada critério da spec, binário, com evidência:

1. **ATENDIDO** — `cerne note new` minta `N-NNNN` ativo, persiste e materializa `content/notes/N-NNNN.md`; corpo vazio = stub (`doccheck.HasRealContent` falso): `internal/app/note.go:CreateNote`/`writeNoteStub`; `internal/cli/notecmd_test.go:TestNoteNew`.
2. **ATENDIDO** — `NoteState` (`active`/`superseded`) + `TransitionNote`, mirror exato da decisão (2 estados, sem `retired`): `internal/domain/note.go`; `domain/note_test.go:TestNoteTransitions`.
3. **ATENDIDO** — `cerne note substituir` move `active`→`superseded`, `--by` opcional (sem sucessor = retira): `internal/app/note.go:TransitionNote`; `notecmd_test.go:TestNoteSubstituir` (com `--by`) e `TestNoteSubstituirRetire` (sem).
4. **ATENDIDO** — projeção (`## Conhecimento` no CONTEXT + `.docs/current/notes/`), índice (FTS5, incl. `superseded`) e lint (órfã + menção a substituído): `internal/materialize`, `internal/index`, `internal/lint` atualizados; `e2e/note_test.go:TestNoteGoldenPath`.
5. **ATENDIDO** — backlink `--informs` (reusa o mecanismo do F-0020): `internal/app/note.go` (`validateInforms`) + `store.Note.Informs`; `notecmd_test.go:TestNoteInforms`.
6. **ATENDIDO** — skill orienta o roteamento de conhecimento: `internal/skills/cerne-authoring/SKILL.md` (seções "Notes" e "Knowledge routing: cerne note vs. local memory").

Suíte: **636 passed em 28 pacotes** (`go test ./... -count=1`); `go vet ./...` limpo. Verificado de forma independente após a implementação.

## O que mudou

- Entidade nova `note` (prefixo `N`, ciclo ativo/substituído) em domain/store/app: `internal/domain/note.go`, `internal/store/{entity,layout}.go`, `internal/app/note.go`.
- Comandos `cerne note new|substituir` (`internal/cli/notecmd.go`, `root.go`), `--by` opcional, corpo freeform (stdin/`--file`).
- Projeção (`materialize`: seção `## Conhecimento` + `.docs/current/notes/`), `push` (`backendRelPath`), `lint` (órfã + menção a substituído), `index` (busca/`ask`, incl. `superseded`).
- Skill `cerne-authoring`: seções "Notes" e "Knowledge routing", roteando conhecimento durável de time → `cerne note new` e pessoal/máquina → memória local (nunca empurrada).

## Débitos e follow-ups

- `cerne note import-memory`: classificar e subir o subconjunto compartilhável de `~/.claude/.../memory/`, separando durável de máquina-específico — follow-up; é o ponto sensível (paths de máquina).
- Writeback cerne → `MEMORY.md` local (projetar conhecimento de time de volta ao formato de memória do Claude Code) — follow-up.
- Superfície de note no servidor/UI (browsing read-only) — follow-up.
- Regra de `cerne lint` de staleness por idade da nota.

## Aprendizados e desvios

O molde foi a decisão (FSM mínimo de 2 estados), o que tornou o trabalho mecânico — e mais enxuto que o runbook: sem `*tmpl`, sem `draft`, sem `importer`, pois o corpo é freeform (não há convenção de doc-tree a varrer). `NextID` já era genérico (mapa `idSubdirs`), então o prefixo `N` entrou sem refactor.

Dois desvios mínimos e conscientes: (1) `Note.Informs []string` foi adicionado ao struct (decisão/runbook não têm) para persistir o backlink — campo mínimo para o critério 5; (2) `stdinPiped()` lê stdin só quando há pipe, porque o corpo da nota é opcional (ao contrário da reference, cujo conteúdo é obrigatório e sempre lê stdin). Sem desvios da spec; servidor/UI e import de memória conscientemente fora de escopo.
