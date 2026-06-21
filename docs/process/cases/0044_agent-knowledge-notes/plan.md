# Case 0044 — Plano: note como entidade

**Spec:** [spec.md](spec.md) — fonte dos critérios de aceitação.
**Branch:** `claude/agent-knowledge-notes-f0044` (worktree).
**Status:** Rascunho (2026-06-21).

## Estratégia

MR único, TDD, espelhando a entidade **decisão** camada por camada (FSM de 2 estados, `active → superseded`, é o molde mínimo). É uma entidade nova no modelo de dados, então toca domain/store/app/cli/materialize/lint/index/push — porém **mais enxuto que o runbook (F-0040)**: sem pacote de template (`*tmpl`), sem comando `draft`, sem `importer` (o corpo é freeform; não há convenção de doc-tree a varrer). Ver [spec.md](spec.md) para o desenho e as decisões.

## Sequência de tarefas

1. **Entidade (domain + store).** `internal/domain/note.go` — `NoteState` (`active`/`superseded`) + `TransitionNote`, cópia 1:1 de `decision.go`/`runbook.go`. `internal/store/entity.go` — struct `Note` + CRUD (espelha `Decision`); `layout.go` — `idSubdirs["N"] = notesSubdir`, `notesSubdir` + `"content/notes"` em `layoutSubdirsInit`. Testes: `internal/domain/note_test.go`, CRUD em `store`. Verificação: `go build ./...`, `go test ./internal/domain/ ./internal/store/` — base dos critérios 1 e 2.
2. **app + cli.** `internal/app/note.go` — `CreateNote` (mint `N-NNNN`, persiste, escreve corpo de `--file`/stdin ou `writeNoteStub` mínimo), `TransitionNote`, backlink `--informs` (reusa o mecanismo do F-0020/reference). `internal/cli/notecmd.go` — `cerne note new|substituir` (`--by` opcional, `--reason`, `--informs`, `--file`/stdin) + registro em `root.go`. Teste: `internal/cli/notecmd_test.go` (`new`, `substituir` com/sem `--by`, `--informs`, stub lido como stub). Verificação: `go test ./internal/cli/ -run Note` — critérios 1, 3, 5.
3. **Projeção e busca.** `internal/materialize` — notes `active` em `.docs/current/notes/` + seção `## Conhecimento` no `CONTEXT.md`. `internal/index` — indexa notes na FTS5 (incl. `superseded`) → `search`/`ask`. `internal/lint` — paridade (nota órfã + menção a entidade substituída; reusa as regras existentes). `internal/cli/pushcmd.go` — `backendRelPath` cobre `content/notes`. Atualizar testes de `materialize`. Verificação: `go test ./...` — critério 4.
4. **e2e + skill + referências.** `e2e/note_test.go` — golden path: `note new` → `pull` → aparece no `CONTEXT.md` → `ask` cita. Seção de **roteamento de conhecimento** na skill `internal/skills/cerne-authoring` (durável de time → `cerne note new`; pessoal/`user`/máquina → memória local). `cerne reference new` para as fontes que embasaram o caso, com `--informs F-0044`. Commit final: `docs(cerne): complete F-0044 (note entity) — case + references`.

## Dependências e ordem

Sequencial: 1 → 2 → 3 → 4. Tarefa 2+ depende de 1. Tarefa 4 (refs) depende da feature existir no backend (`import`). Sem dependências externas.

## Riscos e rollback

- **Risco:** adicionar `notesSubdir`/`content/notes` a `layoutSubdirsInit` não retroage — backends já inicializados não têm os dirs. Detecção: `note new`/`materialize` num backend antigo. Mitigação: criação preguiçosa (`os.MkdirAll`) no `CreateNote` e na materialização, como o resto da store já faz.
- **Risco:** heurística de estado do importer (`status…superseded`) disparar ao importar a própria spec/plan deste caso. Mitigação: a prosa usa "estado"/"substituído", evitando "status"+"superseded" juntos nas primeiras linhas (mesma disciplina do F-0040).
- **Rollback:** cada tarefa é um commit isolado; `git revert` por tarefa. Nada destrutivo (entidade nova, sem migração de dados existentes).

## Restrições para o agente

- Espelhar a entidade **decisão** (FSM de 2 estados) — **NÃO** inventar estado `retired` nem `ActionRetire`; `substituir` sem `--by` = retirada (mesmo `superseded`).
- Note é **freeform** — **NÃO** criar pacote `*tmpl` nem comando `draft`.
- Reusar `--informs` do F-0020 e as regras de `lint` existentes (órfão/needle) — não duplicar.
- Sem `importer`, sem servidor/UI (decisões fechadas na spec).
- Strings de usuário em pt-BR; código, comentários e identificadores em inglês.

## Definição de pronto

Checklist binário (evidência `arquivo:linha` no result.md):
- [ ] Crit. 1 — `internal/app/note.go:CreateNote` + `writeNoteStub`; `notecmd_test.go:TestNoteNew` (mint + stub lido como stub).
- [ ] Crit. 2 — `internal/domain/note.go`; `domain/note_test.go` (FSM `active → superseded`).
- [ ] Crit. 3 — `notecmd_test.go:TestNoteSubstituir` (com e sem `--by`).
- [ ] Crit. 4 — `materialize`/`index`/`lint` atualizados; `e2e/note_test.go` (materializa em `.docs/current/notes/` + seção `## Conhecimento`).
- [ ] Crit. 5 — `--informs` persistido + backlink; `notecmd_test.go:TestNoteInforms`.
- [ ] Crit. 6 — seção de roteamento de conhecimento em `internal/skills/cerne-authoring`.
- [ ] Suíte completa: `go vet ./...` limpo, `go test ./...` verde.
- [ ] result.md + registro no backend (`cerne import` + `pull`, commit do `docs.lock`).
