# Resultado — Case 0029 (`cerne init` instala a skill canônica)

**Status:** Implemented (2026-06-17) — suíte verde, `go vet` limpo, `go build` ok; aguardando revisão.

---

## O que foi entregue

`cerne init` passou a **instalar a skill canônica** (`cerne-authoring`) no final do provisionamento, fechando o débito #2 do [case 0028](../0028_skill-distribution/result.md). Um projeto novo nasce já com o guia do agente em `.claude/skills/`, sem segundo comando nem cópia manual.

### CLI — `internal/cli/initcmd.go`
- Ao fim de `runInit` (depois de `.cernerc`, `docs.lock` e `.docs/` gravados e da linha "Initialized/Joined"): chama `skills.Install(targetDir)` — a **mesma** função e embed do 0028 (zero conteúdo de skill novo).
- **Falha não-fatal:** o projeto já está provisionado quando o passo roda; um erro de skill vira `aviso: …` em `stderr` e o `init` **conclui com sucesso**. Provisão é a prioridade, skill é add-on.
- Saída por skill com as labels pt-BR de sempre (`criada`/`atualizada`/`já atual`), entre a confirmação e os "Next steps":
  ```
  Initialized project "demo-project" with backend ….
  cerne-authoring: criada em .claude/skills/cerne-authoring
  Next steps:
    …
  ```

### CLI — `internal/cli/skillcmd.go` (refactor)
- Extraído `printSkillResults(io.Writer, []skills.Result)` do corpo de `runSkillInstall`; agora `cerne skill install` e `cerne init` imprimem **identicamente** pelo mesmo helper. Sem mudança de comportamento (coberto por `TestSkillInstall`, inalterado).

**3 commits:** docs (spec+plan) → feat (init instala + refactor + teste) → result.

## Aceitação
- `go vet ./...` limpo; `go build ./...` ok (embed presente, binário autocontido).
- Suíte **verde em todos os pacotes** (23 pacotes; `internal/skills` já existia desde o 0028 — nenhum pacote novo aqui).
- **+1 teste de CLI:** `TestInitInstallsSkills` — após `cerne init` contra um backend bare local, `.claude/skills/cerne-authoring/SKILL.md` existe e a saída traz `cerne-authoring: criada em .claude/skills/cerne-authoring`. Sem regressão em `TestInitFresh`/`join`/`double-init`.
- **Sanity com o binário:** `cerne init` real grava a skill e imprime o status na ordem certa; o caminho *join* (segundo dir) também instala a skill (cada dir de projeto recebe a sua).

## Débitos e próximos passos
1. **cli-rockt/ai-kit (hub) — débito #1 do 0028:** o scaffolder Rockt rodar `cerne skill install`/consumir o embed e o hub deletar a cópia própria. Outro repo, só vale se esses projetos forem cerne-native — integração hub-side separada, fora do lado cerne.
2. **`cerne skill list` / catálogo multi-skill — débito #3 do 0028:** o mecanismo (`[]Skill`) já é extensível; entram aditivamente. Quando houver >1 skill, `init` as instala todas (o loop já é genérico).
3. **cerne consumir a própria skill** — só quando a transição (docs/ → guardião único) fechar.
