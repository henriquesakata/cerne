# Case 0028 — Skill canônica `cerne-authoring` distribuída pela ferramenta

tags: #spec #cerne #skill #distribution #dx

**Status:** Rascunho (2026-06-17) — spec para revisão; `plan.md`/`result.md` a seguir.
**Tipo:** O cerne passa a **ser dono** de uma skill `cerne-authoring` **canônica e agnóstica de projeto**, embutida no binário (`go:embed`) e entregue por um comando novo `cerne skill install`. Projetos existentes instalam/atualizam pelo comando; projetos novos recebem via scaffolding `cli-rockt`/ai-kit (que funila no mesmo comando — follow-up no hub). Núcleo determinístico, sem rede.
**Origem:** Discussão (2026-06-17) ao decidir se valia copiar a skill `cerne-authoring` do hub para o cerne. Conclusão: **não copiar** — cada cliente manter sua própria cópia é a divergência que o cerne existe para matar (a cópia do hub já tinha derivado: não conhecia épicos até ser corrigida à mão em [hub!1059](https://gitlab.com/rockt/hub/-/merge_requests/1059)). Como o cerne é a **ferramenta/guardião**, o guia de uso canônico deve viajar **com** ele. Decisão do usuário: caminho 1 (`cerne skill install`) para projetos existentes + caminho 2 (scaffolding) para projetos novos.

---

## Problema

A skill que ensina um agente a usar o cerne (`cerne-authoring`) vive hoje **dentro de cada cliente** — no hub, em `.claude/skills/cerne-authoring/SKILL.md`, mantida à mão. Consequências:

- **Divergência garantida.** Quando o cerne ganha um recurso (ex.: épicos no case 0027), cada cópia precisa ser atualizada manualmente, projeto a projeto. A cópia do hub **de fato** ficou para trás e só foi corrigida num MR avulso. Isso é exatamente a obsolescência de documentação que o cerne combate — só que aplicada ao guia do próprio cerne.
- **Sem dono claro.** Não há uma fonte única da verdade para "como se usa o cerne"; a do hub é específica do hub (módulos `finance/identity/…`, stack Quarkus/Next.js, skills irmãs que só existem lá).
- **Onboarding manual.** Um projeto novo que adota o cerne não tem como obter o guia a não ser copiando de outro projeto (perpetuando a divergência).

## Objetivo e critérios de sucesso

1. **Fonte única, no cerne:** uma skill `cerne-authoring` **canônica e agnóstica de projeto** mora no repositório do cerne e é embutida no binário (`go:embed`). Descreve o fluxo **cerne-native** (o fluxo-produto: `feature new → pull → push → approve/start/complete`, decisões, referências, **épicos**), sem nada específico de cliente (sem nomes de módulo, sem stack, sem skills irmãs).
2. **Caminho 1 — projetos existentes:** `cerne skill install [--dir <projeto>]` grava `<projeto>/.claude/skills/cerne-authoring/SKILL.md`. **Idempotente** e re-rodável para atualizar; reporta `criada`/`atualizada`/`já atual` por skill. Não exige `.cernerc`/git/backend — instalável em qualquer pasta, antes ou depois do `cerne init`.
3. **Caminho 2 — projetos novos (follow-up no hub):** o scaffolding `cli-rockt`/ai-kit passa a obter a skill **executando `cerne skill install`** (ou vendorando a partir do embed do cerne), nunca mantendo cópia própria. Fora do escopo deste case (outro repo, depende deste) — registrado como próximo passo.
4. **Dogfooding honesto:** o repo do cerne está em **transição** (autora cases como arquivos em `docs/process/cases/` + `import`), então o cerne **não consome** a skill canônica para si ainda — ela descreve o estado-fim cerne-native. O `CLAUDE.md` do cerne ganha uma nota sobre o comando e essa ressalva.
5. `go vet` + suíte verdes; `go build` embute a skill.

## Decisões do design

| Fork | Decisão | Razão |
|---|---|---|
| **Dono da skill** | **cerne** (não o cliente) | cerne é a ferramenta/guardião; o guia de uso pertence a ela. Inverte a dependência: clientes consomem, não copiam. |
| **Distribuição** | `cerne skill install` (existentes) + scaffolding (novos) **funilando no mesmo embed** | Escolha do usuário (1 + 2). Uma fonte; os dois caminhos materializam o mesmo conteúdo. |
| **Conteúdo** | Fluxo **cerne-native**, **agnóstico de projeto** | Reusa a maturidade da skill do hub, sem os hub-ismos (módulos/stack/skills irmãs). Inclui épicos (0027). |
| **Embed** | `go:embed` num pacote `internal/skills` | Mesmo padrão do `ui/dist`; binário autocontido, sem rede nem arquivos soltos. |
| **Escopo do `install`** | Só filesystem; **não** resolve projeto cerne | Uma skill instala em qualquer pasta, inclusive antes do `init`. Sem acoplar a backend/git/lock. |
| **Conflito com edição local** | **Sobrescreve** para o canônico; reporta `criada`/`atualizada`/`já atual` | Artefato gerenciado (como `CONTEXT.md`/wiki: "não edite à mão"). Quer mudar? Edite a fonte no cerne e mande PR. |
| **Catálogo** | v1 = só `cerne-authoring`; mecanismo extensível (`[]Skill`) | YAGNI; mais skills entram aditivamente. |
| **`cerne init` instala?** | **Fora do v1** (comando avulso primeiro) | Mantém o case pequeno; integrar ao `init` é aditivo depois. |

---

## Desenho

### `internal/skills` (novo, embute o canônico)
- `internal/skills/cerne-authoring/SKILL.md` — conteúdo canônico (agnóstico).
- `internal/skills/skills.go`:
  - `//go:embed cerne-authoring/SKILL.md` (embed.FS ou string).
  - `type Skill struct { Name, RelPath, Content string }` — `RelPath` relativo a `.claude/skills/` (ex.: `cerne-authoring/SKILL.md`).
  - `func All() []Skill` — v1 devolve a única skill.
  - `type Result struct { Skill string; Status string }` — `created|updated|unchanged`.
  - `func Install(projDir string) ([]Result, error)` — para cada skill, escreve `<projDir>/.claude/skills/<RelPath>` (cria dirs); compara conteúdo p/ status; escrita atômica.

### CLI
- `internal/cli/skillcmd.go` + registro em `root.go`: `cerne skill install [--dir <projeto>]` (default cwd). Saída pt-BR por skill: `cerne-authoring: criada|atualizada|já atual em .claude/skills/…`. Estrutura `skill` como comando-pai (espaço p/ `list` futuro).

### Doc
- `CLAUDE.md` (cerne): nota curta na seção *Documentation source of truth* — o cerne entrega sua própria skill via `cerne skill install`; o repo do cerne, em transição, ainda não a consome (descreve o estado-fim cerne-native).

---

## Verificação
- **skills (TDD):** `All()` não-vazia, cada `Content` com frontmatter `name:`/`description:` e não-stub; `Install` em `t.TempDir()` → cria o arquivo no caminho certo com o conteúdo do embed; 2ª chamada → `unchanged`; conteúdo alterado → `updated`; conteúdo idêntico ao embed em pasta nova → `created`.
- **cli:** `cerne skill install --dir <tmp>` grava a skill e imprime o status; default cwd.
- `go vet` + suíte verdes; `go build` ok (embed presente).

## Fora de escopo
- **Adoção no hub (caminho 2):** cli-rockt/ai-kit consumir `cerne skill install` no scaffold **e** deletar a cópia própria do hub — outro repo, depende deste; próximo passo imediato após o merge.
- **`cerne init` instalar a skill** automaticamente — aditivo.
- **`cerne skill list` / `uninstall` / catálogo multi-skill** — v1 entrega só `install` da `cerne-authoring`.
- **cerne consumir a própria skill** — só quando a transição (docs/ → cerne guardião único) fechar.
