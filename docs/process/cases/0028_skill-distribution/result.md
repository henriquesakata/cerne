# Resultado — Case 0028 (skill canônica distribuída pela ferramenta)

**Status:** Implemented (2026-06-17) — suíte verde, `go vet` limpo, embed no binário; aguardando revisão. Caminho 2 (hub) é follow-up.

---

## O que foi entregue

O cerne passou a **ser dono** de uma skill `cerne-authoring` canônica e **agnóstica de projeto**, embutida no binário e entregue por um comando — acabando com as cópias divergentes por cliente.

### `internal/skills` (novo)
- `internal/skills/cerne-authoring/SKILL.md` — guia **cerne-native, agnóstico** (sem módulos/stack/skills do hub): loop `feature new [--module] [--parent] → pull → editar .docs/current → push → approve/start/complete`, decisões, referências (`--informs`), **épicos** (`--parent`/`reparent`, rollup em status/CONTEXT, lint) e hard rules.
- `skills.go`: `//go:embed` da skill; `All() []Skill`; `Install(projDir) ([]Result, error)` grava em `<proj>/.claude/skills/<RelPath>` e reporta `created`/`updated`/`unchanged` (artefato gerenciado — sobrescreve edição local para o canônico).

### CLI
- `cerne skill install [--dir]` (`internal/cli/skillcmd.go`, comando-pai `skill`): materializa as skills embutidas no projeto, imprimindo `cerne-authoring: criada|atualizada|já atual em .claude/skills/…`. Não exige `.cernerc`/git — instalável em qualquer pasta.

### Doc
- `CLAUDE.md`: nota de que o cerne entrega a própria skill via `cerne skill install`, e que o repo (em transição) ainda não a consome.

**5 commits:** docs (spec+plan) → `internal/skills` (embed+Install) → CLI (`skill install`) → nota no CLAUDE.md → result.

## Aceitação
- `go vet ./...` limpo; suíte **491 → 494** (skills: `All` com frontmatter/não-stub, `Install` created/updated/unchanged; CLI: install + idempotência). 23 pacotes (novo `internal/skills`).
- `go build` embute a skill (binário autocontido).

## Débitos e próximos passos
1. **Caminho 2 — hub (follow-up imediato):** o scaffolding `cli-rockt`/ai-kit roda `cerne skill install` na criação de projeto **e** o hub deleta sua cópia própria de `.claude/skills/cerne-authoring/SKILL.md`, passando a consumir a canônica. Depende deste case mesclado. (A cópia do hub já recebeu a orientação de épico em [hub!1059](https://gitlab.com/rockt/hub/-/merge_requests/1059); ao adotar a canônica, essa cópia some.)
2. **`cerne init` instalar a skill** automaticamente em projetos novos cerne-native — aditivo.
3. **`cerne skill list` / catálogo multi-skill** — o mecanismo (`[]Skill`) já é extensível; entram aditivamente.
4. **cerne consumir a própria skill** — só quando a transição (docs/ → guardião único) fechar.
