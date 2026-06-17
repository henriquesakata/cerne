# Case 0029 — `cerne init` instala a skill canônica em projetos novos

tags: #spec #cerne #skill #init #dx

**Status:** Rascunho (2026-06-17) — spec para revisão; `plan.md`/`result.md` a seguir.
**Tipo:** Aditivo. O `cerne init`, ao terminar de provisionar um projeto, passa a **instalar a skill canônica** (`cerne-authoring`) no `.claude/skills/` do projeto, reusando o mesmo embed e a mesma `skills.Install` do case 0028. Um projeto novo nasce já sabendo ensinar o agente a usar o cerne — sem cópia manual, sem passo extra.
**Origem:** Débito #2 do [case 0028](../0028_skill-distribution/result.md) ("**`cerne init` instalar a skill** automaticamente em projetos novos cerne-native — aditivo"). O 0028 entregou o embed + `cerne skill install` (caminho 1, projetos existentes) e deixou explícito que integrar ao `init` seria aditivo. Este case fecha esse débito **do lado do cerne**, sem acoplar a nenhum scaffolder externo.

---

## Problema

Depois do case 0028, a skill canônica viaja **com o binário**, mas um projeto recém-criado só a recebe se alguém lembrar de rodar `cerne skill install` à mão. O momento natural para o agente ganhar seu guia é o **nascimento do projeto** — é quando o `.claude/` ainda nem existe e o onboarding começa. Exigir um segundo comando:

- **Reintroduz o passo manual** que o 0028 queria matar: o projeto novo fica sem guia até alguém se lembrar.
- **Desalinha a fonte da verdade da adoção:** "como um projeto cerne-native começa" deveria incluir o guia de uso, não deixá-lo a cargo de cada um.

### Por que `cerne init` e não o scaffolder (cli-rockt)

O `cli-rockt` (em `rockt/hub`, `packages/cli-rockt`) é o scaffolder de **plugins/apps Rockt** — não tem integração com cerne e suas skills ai-kit são específicas de plugin. Logo, a casa natural de "todo projeto cerne novo ganha a skill" é o **próprio `cerne init`**. (Se mais tarde quisermos que projetos plugin/app gerados pela Rockt também a recebam, isso é uma integração hub-side cli-rockt↔cerne separada, só válida se esses projetos forem geridos por cerne — fora deste case.)

## Objetivo e critérios de sucesso

1. **`init` instala:** ao final de `cerne init` (depois de `.cernerc`/`docs.lock`/`.docs/` prontos), chamar `skills.Install(projDir)` e gravar `<projDir>/.claude/skills/cerne-authoring/SKILL.md`, reportando por skill `criada`/`atualizada`/`já atual` (mesmas labels pt-BR de `cerne skill install`).
2. **Skills não derrubam o `init`:** uma falha ao instalar skills é **não-fatal** — o `init` já provisionou o projeto (backend, lock, .docs), então um erro de skill vira **aviso** e o comando **conclui com sucesso**. Provisão de projeto é a prioridade; o guia é um add-on.
3. **Fonte única:** reusa o embed e a `skills.Install` do 0028 — **nenhum** conteúdo de skill novo, **nenhuma** cópia. A mesma função alimenta `cerne skill install` e `cerne init`.
4. **Sem regressão:** os caminhos existentes do `init` (fresh / join / double-init) seguem idênticos; o passo de skills é puramente aditivo e ao final.
5. `go vet` + suíte verdes; `go build` ok.

## Decisões do design

| Fork | Decisão | Razão |
|---|---|---|
| **Onde instalar** | `cerne init` (lado cerne) | O scaffolder Rockt não conhece cerne; "projeto cerne novo ganha a skill" pertence ao `init`. Sem acoplamento cli-rockt↔cerne. |
| **Reuso** | Chama `skills.Install` (0028), não duplica | Fonte única do conteúdo e da lógica created/updated/unchanged. |
| **Falha de skill** | **Não-fatal** (aviso + segue) | O projeto já está provisionado quando o passo roda; init bem-sucedido é a prioridade. Skill é add-on. |
| **Posição no fluxo** | **Depois** de lock/.docs, no bloco de saída | Não interfere em backend/lock; se falhar, tudo que importa já foi gravado. |
| **Saída** | Mesmas labels pt-BR (`criada`/`atualizada`/`já atual`) | Consistência com `cerne skill install`; reusa o helper de impressão. |
| **`--dir`** | Instala em `targetDir` (o dir do projeto) | A skill é do projeto, não do cwd; respeita `--dir`. |

---

## Desenho

### `internal/cli/initcmd.go`
- Ao final de `runInit`, após gravar `.cernerc`, `docs.lock` e `.docs/`, e após imprimir a linha "Initialized/Joined": chamar `skills.Install(targetDir)`.
  - Sucesso → imprimir, por skill, `cerne-authoring: criada|atualizada|já atual em .claude/skills/…` (mesmo formato de `runSkillInstall`).
  - Erro → `fmt.Fprintf(cmd.ErrOrStderr(), "aviso: …: %v\n", err)` e **continuar** (não retornar o erro).
- Reusa `skillStatusLabel` (já em `skillcmd.go`, mesmo pacote). Para não duplicar o `for`+formato, extrair um helper `printSkillResults(io.Writer, []skills.Result)` em `skillcmd.go` e usá-lo nos dois sítios.

### `internal/cli/skillcmd.go`
- Extrair `printSkillResults(out io.Writer, results []skills.Result)` do corpo de `runSkillInstall`; `runSkillInstall` passa a chamá-lo. Sem mudança de comportamento (refactor coberto pelo teste de `skill install`).

---

## Verificação
- **cli (TDD):** novo `TestInitInstallsSkills` em `initcmd_test.go` — após `cerne init` (com backend bare local, como os demais testes de init), `<projDir>/.claude/skills/cerne-authoring/SKILL.md` **existe** e a saída contém `cerne-authoring: criada em .claude/skills/cerne-authoring`.
- **Regressão:** `TestInitFresh`/`join`/`double-init` seguem verdes (passo aditivo ao final).
- `go vet ./...` + suíte verdes; `go build ./...` ok.

## Fora de escopo
- **cli-rockt/ai-kit (hub):** scaffolder Rockt rodar `cerne skill install`/consumir o embed — outro repo, só se esses projetos forem cerne-native. Permanece como integração hub-side separada (débito #1 do 0028, já em curso).
- **`cerne skill list` / catálogo multi-skill** — débito #3 do 0028; o mecanismo (`[]Skill`) já é extensível.
- **Adicionar `.claude/skills/` aos "next steps" / `.gitignore`** — a skill é artefato versionável do projeto; orientação de commit fica fora deste case.
- **cerne consumir a própria skill** — só quando a transição (docs/ → guardião único) fechar.
