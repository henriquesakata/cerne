# Case 0029 — Plano de implementação

Commits TDD (teste vermelho → impl mínima → verde → commit), uma preocupação por commit. Suíte verde a cada commit. Caso pequeno e aditivo: reusa o embed + `skills.Install` do 0028; só liga ao `cerne init`.

Convenções: Conventional Commits; corpo termina com `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`. Strings de usuário pt-BR; código/identificadores em inglês. cerne **não** tem commitlint.

---

## 1. docs — spec + plan
- `spec.md` + `plan.md` + linha no índice `docs/process/CLAUDE.md` (status: Em desenvolvimento).
- `docs(cerne): case 0029 — init installs skills (spec + plan)`

## 2. cli — `init` instala a skill canônica
- **Teste (vermelho):** `TestInitInstallsSkills` em `internal/cli/initcmd_test.go` — após `cerne init` contra um backend bare local, `<projDir>/.claude/skills/cerne-authoring/SKILL.md` existe e a saída contém `cerne-authoring: criada em .claude/skills/cerne-authoring`.
- **Impl (verde):**
  - `internal/cli/skillcmd.go`: extrair `printSkillResults(out io.Writer, results []skills.Result)` e fazer `runSkillInstall` chamá-lo (refactor sem mudança de comportamento, coberto por `TestSkillInstall`).
  - `internal/cli/initcmd.go`: ao final de `runInit`, após a linha "Initialized/Joined", chamar `skills.Install(targetDir)`; sucesso → `printSkillResults`; erro → aviso em `cmd.ErrOrStderr()` e **continuar** (não-fatal). Importar `gitlab.com/rockt/cerne/internal/skills`.
- `feat(cli): cerne init installs the canonical cerne-authoring skill`

## 3. Fechamento
- `export PATH="/c/development/tools/go/bin:$PATH"; go vet ./... && go test ./... -count=1` verdes; `go build ./...` ok.
- `result.md` + índice → Implemented.
- **Entrega:** push `feat/init-installs-skills` + MR no `rockt/cerne` via `glab`. Após merge: `scripts/update-cerne.ps1` para atualizar o cerne global.
