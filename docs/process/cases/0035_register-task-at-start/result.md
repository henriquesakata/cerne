# Case 0035 — Resultado: regra "registrar a task no início"

tags: #result #cerne #skill #hook #process

**Status:** Implementado — suíte verde (548). Skill + hook entregues; registro no backend pendente (outward).
**Entregue:** Regra portável na skill `cerne-authoring` (3 pontos) + hook `SessionStart` neste repo que injeta o lembrete de registrar a task antes de implementar.

---

## O que mudou

- **[internal/skills/cerne-authoring/SKILL.md](../../../../internal/skills/cerne-authoring/SKILL.md):** `## When to use` (1º bullet → "o momento em que você pega uma task"), `## Feature loop` (parágrafo *"Register at the start, not the end"*; criar é barato e auto-limitante), `## Hard rules` (bullet normativo register-at-start + `reject` + ponteiro lint `stale-candidate`). Portável — `cerne skill install` passa a entregar a regra a clientes.
- **`.claude/hooks/register-task-reminder.sh`:** novo. Emite em stdout a regra (candidate no início → spec/approve, plan/start, result/complete; `reject` se abandonar; lint pega esquecidas). Estático, sem backend.
- **`.claude/settings.json`:** novo. Hook `SessionStart` → `bash "$CLAUDE_PROJECT_DIR/.claude/hooks/register-task-reminder.sh"`.

## Aceitação

- `go vet ./...` limpo; **`go test ./... = 548 verdes`** — o golden de embed da skill (0028) sobreviveu à edição da `SKILL.md`.
- `bash .claude/hooks/register-task-reminder.sh` imprime a regra; `settings.json` é JSON válido (shape `SessionStart`). O efeito in-vivo (lembrete injetado) só se observa numa nova sessão.

## Efeito

- Trabalho em voo deixa de ser invisível: a regra (portável) + o hook (dogfood) empurram o registro para o início, então o kanban reflete o que está sendo feito desde o minuto um. O board fica honesto sem poluir — o gate é "trabalho começou", e o caso 0036 (lint `stale-candidate`) varre as esquecidas.

## Fora de escopo / follow-up

- **`cerne hooks install`** para levar o hook a clientes (hoje só dogfood) — follow-up aberto.
- **Enforcement duro** (`PreToolUse`) e **hook inteligente** (checa status) — só se a regra soft falhar na prática.

## Próximos passos

- Registrar no cerne: `cerne import` + `cerne pull`; commit `docs.lock`.
