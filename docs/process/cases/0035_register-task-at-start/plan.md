# Case 0035 — Plano

tags: #plan #cerne #skill #hook

1. **spec + plan (registro)** — este `spec.md` + `plan.md`.
2. **Skill: regra register-at-start** — edita [SKILL.md](../../../../internal/skills/cerne-authoring/SKILL.md) em 3 pontos (`When to use` / `Feature loop` / `Hard rules`). Roda `go test ./...` (golden de embed da skill verde). Um commit.
3. **Hook SessionStart (dogfood)** — adiciona `.claude/hooks/register-task-reminder.sh` (chmod +x) + `.claude/settings.json` com o hook. Smoke-test do script. Um commit.
4. **result.md** — registra o entregue.
5. **Fechamento** — `cerne import` + `cerne pull`; commit `docs.lock`. (Outward: backend `cerne-docs`.)

## Notas / riscos

- A `SKILL.md` é embed (`go:embed`, caso 0028) — a edição muda o que `cerne skill install` escreve nos clientes; o golden test de embed precisa ficar verde.
- Shape do hook `SessionStart`: array de matcher-objects com `hooks: [{type, command}]`; matcher omitido = todas as origens. `$CLAUDE_PROJECT_DIR` é provido pelo Claude Code.
- `.claude/` não está no `.gitignore` → `settings.json`/hook são versionados (viram regra de time no merge para `main`).
