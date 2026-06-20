# Case 0035 — Registrar a task no início (regra cerne-authoring + hook SessionStart)

tags: #spec #cerne #skill #hook #process

**Status:** Rascunho (2026-06-20) — spec para revisão; `plan.md`/`result.md` a seguir.
**Tipo:** Processo/regra. Edita a skill canônica (portável) + adiciona um hook `SessionStart` neste repo (dogfood). Sem mudança no domínio/CLI.
**Origem:** Observação do usuário — um agente já trabalhava numa task sem que ela aparecesse no kanban do cerne. Sem registro no início, o trabalho em voo fica invisível e some do acompanhamento.

---

## Problema

Quando um agente começa a implementar uma task, nada a registra no cerne até alguém rodar `cerne feature new` — o que hoje acontece tarde (perto do fim) ou nunca. Resultado: o trabalho em andamento não aparece no kanban (CANDIDATAS/EM DESENVOLVIMENTO ficam vazias enquanto há agente ativo) e não há acompanhamento visual desde o início. O `.docs/CONTEXT.md` e o `cerne status` só refletem o que foi registrado, então um agente "no escuro" é literalmente invisível para o guardião.

O medo legítimo do outro lado: registrar toda ideia polui o board com features que podem não ser importantes nem implementadas. Hoje não há regra que equilibre "registrar cedo" com "não poluir".

## Objetivo e critérios de aceitação

Objetivo: tornar regra que a task seja registrada no **início** do trabalho, mantendo o board limpo pelo gate natural ("trabalho começou", não "ideia surgiu") + `reject` + lint.

1. A skill `cerne-authoring` (portável, instalada por `cerne skill install`) declara a regra "registre a task no início" no ponto normativo (`## Hard rules`) e no fluxo (`## When to use` + `## Feature loop`).
2. QUANDO uma sessão de agente inicia neste repo, O SISTEMA DEVE injetar como contexto a instrução de registrar a task antes de implementar (hook `SessionStart` com saída em stdout).
3. O hook NÃO DEVE depender de rede/SSH/backend para rodar (estático), para não falhar quando o backend `cerne-docs` está inacessível.
4. A regra aponta o caminho anti-poluição: `candidate` no início → avança por spec/plan/result → `reject` se abandonada; candidatas esquecidas são pegas pelo lint `stale-candidate` (caso 0036).

## Decisões do design

| Fork | Decisão | Razão |
|---|---|---|
| Onde a regra vive | Skill (portável) + hook `SessionStart` (este repo) | A skill chega a todo cliente via `cerne skill install`; o hook é enforcement dogfood local. Cada cliente decide seu próprio enforcement. |
| Estado de registro | `candidate` como inbox de trabalho-em-voo | É o único estado sem gate de documento; aparece no board na hora. Spec/plan/result o empurram para a direita. |
| Anti-poluição | Gate = "trabalho começou" + `reject` + lint `stale-candidate` | Ideia especulativa nunca vira feature (ninguém trabalha nela); abandonada → `reject` (sai do board, vira história); esquecida → lint. Sem entidade nova. |
| Enforcement do hook | Lembrete estático (não checa `cerne status`) | Checar exigiria backend/SSH no start — frágil e lento. v1 estático; evoluir depois. |
| Dureza | `SessionStart` (soft, injeta contexto), não `PreToolUse` (bloqueia) | Bloquear Edit/Write até existir candidate é intrusivo; começar soft. |

## Desenho

### [internal/skills/cerne-authoring/SKILL.md](../../../../internal/skills/cerne-authoring/SKILL.md) (portável)
- `## When to use`: primeiro bullet reescrito para "o momento em que você pega uma task — registre primeiro".
- `## Feature loop`: parágrafo *"Register at the start, not the end"* — o passo 1 (`cerne feature new`) roda antes de codar; criar é barato e auto-limitante (só se mint quando trabalho real começa).
- `## Hard rules`: bullet normativo "Register the task at the start" + `reject` + ponteiro para o lint `stale-candidate`.

### `.claude/settings.json` (este repo)
Hook `SessionStart` → comando `bash "$CLAUDE_PROJECT_DIR/.claude/hooks/register-task-reminder.sh"`. Matcher omitido = todas as origens (startup/resume/clear).

### `.claude/hooks/register-task-reminder.sh` (este repo)
Emite em stdout (injetado como contexto) a regra: primeira ação = `cerne feature new "<title>"` (candidate, aparece em CANDIDATAS); avança spec→`approve`, plan→`start`, result→`complete`; abandonou → `reject`; esquecidas → `cerne lint` (`stale-candidate`). Estático, sem backend.

## Verificação (TDD)

- Processo/config, não código de domínio: sem teste Go novo. A verificação é a suíte existente seguir verde (`go vet ./... && go test ./...`) — a edição da `SKILL.md` não pode quebrar o golden test de embed da skill (caso 0028).
- Smoke manual do hook: `bash .claude/hooks/register-task-reminder.sh` imprime a regra; `settings.json` é JSON válido com o shape `SessionStart` aceito pelo Claude Code.
- Aceitação observável: numa nova sessão neste repo, o lembrete aparece como contexto inicial.

## Fora de escopo

- **Instalar o hook em projetos cliente** (ex.: um `cerne hooks install` análogo ao `skill install`): follow-up; aqui o hook é só dogfood deste repo.
- **Enforcement duro** (`PreToolUse` bloqueando Edit/Write sem candidate): só se a regra soft for ignorada na prática.
- **Hook inteligente** (checar `cerne status` e só lembrar quando não há trabalho ativo): exige backend no start; v1 é estático.
- **O guard de poluição** (lint `stale-candidate`): é o caso 0036.
