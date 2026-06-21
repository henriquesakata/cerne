# Case 0044 — Note: conhecimento de agente compartilhado como entidade

tags: #spec #cerne #domain #cli #materialize #docs #memory

**Status:** Rascunho (2026-06-21) — spec para revisão.
**Tipo:** Novo recurso — adiciona um tipo de entidade ao modelo de dados do cerne.
**Origem:** observação do usuário (2026-06-21): trabalhando em N computadores, os agentes gravam conhecimento adquirido na memória local do Claude Code (`~/.claude/projects/<proj>/memory/MEMORY.md`), que não é compartilhada entre máquinas do mesmo dev nem entre membros do time. Discussão de design nesta sessão concluiu: o gap real não é "a memória não sincroniza", e sim "conhecimento durável de projeto vaza para um scratchpad pessoal em vez de ir para o guardião".

## Problema

Agentes de IA acumulam conhecimento de projeto entre sessões: gotchas, invariantes, "por que X e não Y", convenções tácitas, armadilhas. O Claude Code persiste isso em `~/.claude/projects/<proj>/memory/MEMORY.md` — um arquivo **por-usuário, por-máquina, local**. Três falhas:

1. **Não compartilha entre as máquinas do mesmo dev.** Henrique alterna entre a box Windows e o Mac; cada uma tem sua memória.
2. **Não compartilha entre membros do time.** O conhecimento morre no `~/.claude` de quem o adquiriu.
3. **Foge do guardião.** Conhecimento durável de projeto vive fora do cerne, sem governança, podendo apodrecer — exatamente a doença que o cerne existe para matar (problema #3 do founding spec, *staleness*).

Mas a memória é **heterogênea** — não dá para sincronizar em bloco. O Claude Code já a tipa: `user` (quem é o dev, preferências), `feedback` (como trabalhar), `project` (trabalho em curso, constraints) e `reference` (fontes externas). Só `project` e `reference` são **duráveis e de time**. O resto não pode subir:

- `user`/`feedback` são pessoais — compartilhar impõe o estilo de um ao time.
- Conhecimento **de máquina** quebra ao ser compartilhado: a memória `cerne-darwin-toolchain` diz "Go em `C:\development\tools\go\bin`" — correto no Windows, **errado no Mac**. Empurrá-la é um ativo nocivo.
- `reference` **já é entidade no cerne** (case 0020), duplicada na memória.

Ou seja: parte da memória pertence ao cerne, parte deve ficar local, e a fronteira exige classificação — nunca sync cego.

## Objetivo e critérios de aceitação

Dar lar no cerne ao conhecimento durável de time que hoje vaza para a memória local — uma entidade **leve** `note`, espelhando o runbook (FSM simples, sem gate de aprovação), materializada onde os agentes já leem, indexada, com a skill orientando o roteamento. Sem tocar na memória pessoal/de-máquina.

1. QUANDO `cerne note new "<título>" [--informs F-…,D-…] [--file <corpo> | stdin]` roda O SISTEMA DEVE mintar `N-NNNN` ativo, persistir a entidade e materializar `content/notes/N-NNNN.md`; sem corpo, o scaffold DEVE ser lido como stub (`doccheck.HasRealContent` falso).
2. O pacote `internal/domain` DEVE expor `NoteState` (`active`/`superseded`) e `TransitionNote`, espelhando o FSM da decisão/runbook (2 estados, sem gate), com teste fixando-o.
3. QUANDO `cerne note substituir <id> [--by <id>] --reason "<r>"` roda O SISTEMA DEVE mover o ciclo de `active` para `superseded`. Com `--by`, a nota sucessora materializa no lugar; sem `--by`, a nota é apenas retirada (superseded sem sucessor). A nota antiga vira história pesquisável; o conhecimento corrigido materializa.
4. Notes `active` DEVEM materializar em `.docs/current/notes/` e na seção `## Conhecimento` do `CONTEXT.md`, ser indexadas (`cerne search`/`ask`) e cobertas pelo `cerne lint` (órfã + menção a substituído).
5. QUANDO `--informs` é dado O SISTEMA DEVE fazer o backlink da nota para a feature/decisão que ela informa (reusa a máquina do 0020).
6. A skill `cerne-authoring` DEVE orientar agentes a rotear conhecimento durável de time para `cerne note new` (não para a memória local), e a deixar memória pessoal/de-máquina onde está.

## Decisões do design

| Fork | Decisão | Razão |
|---|---|---|
| Modelar conhecimento de agente | Entidade nova leve `note` (não reusar reference nem feature) | reference é proveniência externa (URL); feature/decision é trabalho gated. Note é conhecimento interno adquirido — sem URL, sem gate. Lacuna real, sem casa hoje. |
| Onde mora | No cerne (backend git = source of truth), não na memória local | É o compartilhamento entre máquinas + membros que falta; o backend já sincroniza, indexa e versiona. |
| Ciclo de vida | `active` → `superseded` via `substituir` (`--by` opcional: com sucessor = substitui, sem = retira); **sem gate** | Espelha decisão/runbook **exatamente** (2 estados — o ciclo mais simples que serve); não inventa FSM novo. Supersessão preserva a história pesquisável = a proposta-núcleo do cerne aplicada à memória do agente. |
| Sem template/draft | Corpo freeform (stdin/`--file`), sem pacote `*tmpl` | Note é soft knowledge de baixo atrito; seções canônicas matariam o propósito. Mais leve que o runbook (que tem `runtmpl` + `draft`). |
| Escopo da memória | Só conhecimento `project`/`reference`-class sobe; `user`/`feedback`/máquina fica local | Sync em bloco quebra (vide path `C:\` errado no Mac). Classificação é obrigatória; v1 = manual via skill, não auto-import. |
| Direção do fluxo | v1 = **subir** (manual via `note new`) + **descer** (materializar em `.docs/`) | Conhecimento de time chega a toda máquina/membro pela materialização, que os agentes já leem. Writeback para o `MEMORY.md` local fica para follow-up. |
| Import de `MEMORY.md` | Fora de escopo (follow-up) | Auto-classificar a memória local é o ponto perigoso (separar durável de máquina-específico). v1 prova o conceito com fluxo manual e evita corromper o backend com paths de máquina. |
| Prefixo de id | `N-NNNN` | "N" está livre; `NextID` é genérico (mapa `idSubdirs`). |
| Decisão arquitetural | Registrada como ADR no backend (dogfood do template de ADR) | Adicionar entidade muda o modelo de dados; merece registro. |

## Desenho

Espelha a entidade decisão/runbook, camada por camada — porém **mais enxuto** (sem template, sem importer):

- `internal/domain/note.go` — `NoteState` (`active`/`superseded`) + `TransitionNote` (espelha `decision.go`/`runbook.go`).
- `internal/store/entity.go` + `layout.go` — `Note`, `notesSubdir`, CRUD, `idSubdirs["N"]`, dirs de layout (`notesSubdir` + `content/notes`).
- `internal/app/note.go` — `CreateNote`, `TransitionNote`, `writeNoteStub` (scaffold mínimo: título + corpo opcional).
- `internal/cli/notecmd.go` — `cerne note new|substituir` (+ registro em `root.go`).
- `internal/materialize` — notes `active` em `.docs/current/notes/` + seção `## Conhecimento` no `CONTEXT.md`.
- `internal/index` — indexa notes na FTS5 (incl. `superseded`/`retired`, como o resto do histórico) → `search`/`ask`.
- `internal/lint` — paridade: nota órfã, nota que menciona entidade substituída (reusa as regras existentes).
- `internal/skills/cerne-authoring` — seção nova de roteamento de conhecimento (durável de time → `note`; pessoal/máquina → memória local).
- Sem `internal/*tmpl` equivalente; sem `internal/importer` (import de memória é follow-up).

## Verificação

Plano de testes (TDD), 1:1 com os critérios: `internal/domain/note_test.go` (FSM), `internal/cli/notecmd_test.go` (`new`/`substituir` com/sem `--by` + stub materializado + `--informs`), testes de `materialize` atualizados (seção `## Conhecimento` + `.docs/current/notes/`), `internal/index` (note aparece em `search`), `e2e/note_test.go` (golden path: `note new` → `pull` → aparece no `CONTEXT.md` → `ask` cita). Suíte completa verde.

## Fora de escopo

- `cerne note import-memory` — classificar e subir o subconjunto compartilhável de `~/.claude/.../memory/` (separando durável de máquina-específico). É o ponto sensível; follow-up.
- Writeback cerne → `MEMORY.md` local (projetar conhecimento de time de volta para o formato de memória do Claude Code). v1 materializa em `.docs/`, que os agentes já leem; follow-up.
- Superfície de note no servidor/UI (browsing read-only) — follow-up.
- Memória pessoal (`user`/`feedback`) e de-máquina — fica local por design, nunca sobe.
- Regra de `cerne lint` de qualidade/staleness por idade da nota.
