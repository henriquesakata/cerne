# Case 0028 — Plano de implementação

Commits TDD (teste vermelho → impl mínima → verde → commit), uma preocupação por commit. Suíte verde a cada commit. Caso pequeno: pacote de embed + comando CLI + nota no doc.

Convenções: Conventional Commits; corpo termina com `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`. Strings de usuário pt-BR; código/identificadores em inglês.

---

## 1. docs — spec + plan
- `spec.md` + `plan.md` + linha no índice `docs/process/CLAUDE.md` (status: Spec).
- `docs(cerne): case 0028 — skill distribution (spec + plan)`

## 2. internal/skills — conteúdo canônico embutido + Install
- **Conteúdo:** `internal/skills/cerne-authoring/SKILL.md` — guia cerne-native **agnóstico** (sem módulos/stack/skills do hub): loop `feature new [--module] [--parent] → pull → editar .docs/current → push → approve/start/complete`, decisões, referências externas (`--informs`), **épicos** (`--parent`/`reparent`, rollup em status/CONTEXT, lint), hard rules (não editar gerados; commitar docs.lock; `.docs/` gitignored). Frontmatter `name`/`description`.
- **Teste:** `All()` não-vazia; cada skill com frontmatter e sem stub; `Install(tmp)` cria `<tmp>/.claude/skills/cerne-authoring/SKILL.md` == embed (`created`); 2ª chamada `unchanged`; arquivo divergente → `updated`.
- **Impl:** `internal/skills/skills.go` — `//go:embed`, `Skill{Name,RelPath,Content}`, `All()`, `Result{Skill,Status}`, `Install(projDir)` (escrita atômica, status por comparação).
- `feat(skills): embed canonical cerne-authoring skill + Install`

## 3. CLI — `cerne skill install`
- **Teste:** `cerne skill install --dir <tmp>` grava a skill e imprime `cerne-authoring: criada em .claude/skills/…`; re-rodar imprime `já atual`.
- **Impl:** `internal/cli/skillcmd.go` (comando-pai `skill` + sub `install`, flag `--dir` default cwd) + registro em `root.go`; mapeia `Result.Status` → pt-BR (`criada`/`atualizada`/`já atual`).
- `feat(cli): add 'cerne skill install'`

## 4. doc — CLAUDE.md
- Nota na seção *Documentation source of truth*: cerne entrega a própria skill via `cerne skill install`; o repo do cerne (em transição) ainda não a consome.
- `docs(cerne): note 'cerne skill install' in the guide`

## 5. Fechamento
- `go vet ./... && go test ./... -count=1` verdes; `go build` ok.
- `result.md` + índice → Implemented.
- **Entrega:** push + MR no `rockt/cerne`. Após merge: follow-up no hub (caminho 2 — cli-rockt roda `cerne skill install` no scaffold e deleta a cópia própria).
