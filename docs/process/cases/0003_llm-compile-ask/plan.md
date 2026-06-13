# Plano de implementação — Case 0003: camada LLM (`compile` + `ask`)

> **For Claude:** desenvolvimento **subagente-dirigido**, TDD estrito (teste vermelho → verde → commit), uma preocupação por commit. Toolchain: Go 1.26 (`/usr/local/go/bin` no mac; `/c/development/tools/go/bin` no Windows). A suíte do v1 (266) deve permanecer verde a cada passo. Trailer de commit: `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.

**Goal:** entregar o corte v1.5 — `cerne compile` (wiki de síntese por módulo, incremental por input-hash) e `cerne ask` (Q&A vigente-primeiro com citação), sobre uma interface de provider plugável cujo único modo implementado é **delegação ao agente hospedeiro**. Núcleo determinístico permanece LLM-free.

**Spec:** [spec.md](spec.md). **Escopo:** A — núcleo CLI, ambos os comandos, **sem UI**.

---

## Decisões de implementação (refinamentos da spec)

1. **Provider só "delegação" em runtime; `Fake` nos testes.** `Provider.Complete` existe para o `Fake` (caminho síncrono ponta-a-ponta) e para o BYO-key futuro. Delegação **não** chama `Complete` — usa o fluxo de duas fases.
2. **Wiki versionada no backend** (`content/wiki/…`), projetada por `pull` em `.docs/wiki/`. `.docs/wiki/.pending/` é transiente (instrução pro agente), nunca commitado.
3. **Páginas:** uma por módulo (features vivas) + `project.md` (decisões ativas + refs vigentes; e todas as features quando o projeto não declara módulos). `Decision`/`Reference` não têm `Module` → daí a página transversal.
4. **`input_hash`** no frontmatter dirige o dirty-tracking — sem novo tipo de evento, espelha o padrão sha256 do `materialize`/`docs.lock`.
5. **Reuso:** commit/push/lock via `internal/app.CommitAndSnapshot`; recuperação do `ask` via `internal/index` (FTS); frontmatter via `internal/doccheck.FrontmatterValue`.

---

## M1 — `internal/llm`: provider plugável + config

### Task 1.1: Interface + Fake + resolução de modo

**Files:** Create `internal/llm/llm.go`, `internal/llm/llm_test.go`; Modify `internal/project/config.go` (+ teste)

**Step 1 — teste:** `ResolveMode(cfg)` → `"delegation"` quando `[llm]` ausente; respeita `mode` explícito; `Fake{Reply:"X"}.Complete(ctx,"p")` retorna `"X"` sem erro.

**Step 3 — implementação:**
```go
// internal/llm
type Provider interface { Complete(ctx context.Context, prompt string) (string, error) }
type Fake struct { Reply string; Prompts []string } // registra prompts p/ asserção
const ModeDelegation = "delegation"
func ResolveMode(m string) string // "" -> ModeDelegation
```
`internal/project`: adicionar `LLM LLMConfig \`toml:"llm"\`` com `type LLMConfig struct { Mode string \`toml:"mode"\` }` (campos BYO-key documentados, inertes).

**Commit:** `feat(llm): provider interface, fake, and .cernerc [llm] mode`

---

## M2 — `internal/wiki`: modelo + proveniência + input-hash

### Task 2.1: Página, frontmatter e InputHash

**Files:** Create `internal/wiki/wiki.go`, `internal/wiki/wiki_test.go`

**Step 1 — teste:** `InputHash(sources)` é estável e independe da ordem de entrada (ordena por ID); muda se estado/conteúdo de uma fonte muda; `ParseProvenance(frontmatter)` e `RenderProvenance(p)` fazem round-trip de `cerne_wiki/sources/compiled_from/input_hash`.

**Step 3 — implementação:** `type Source struct { ID, State string; ContentHash string }`; `InputHash([]Source) string` = sha256 de `sort(ID|State|ContentHash)\n…`; `Provenance` struct + parse/render do frontmatter (reusa `doccheck.FrontmatterValue` na leitura).

**Commit:** `feat(wiki): page provenance frontmatter and input-hash`

---

## M3 — compile: dirty-tracking + montagem de prompt (determinístico)

### Task 3.1: Conjunto de páginas + fontes vivas

**Files:** Create `internal/compile/compile.go`, `internal/compile/compile_test.go`

**Step 1 — teste:** dado um backend com features em vários módulos + decisões/refs, `Pages(repoDir)` retorna uma página por módulo (features vivas daquele módulo) + `project` (decisões ativas + refs vigentes); projeto sem módulos ⇒ só `project` com tudo.

**Step 3:** `livePages(store…)` monta `[]Page{Name, Sources []wiki.Source}` usando os mesmos estados vivos do lint/materialize (`candidate/approved/in_development`, `active`, `current`).

### Task 3.2: Dirty-tracking + prompt

**Step 1 — teste:** `Dirty(repoDir)` lista as páginas cujo `input_hash` atual difere do gravado em `content/wiki/<name>.md` (ou ausente); mudar 1 feature marca **só** o módulo dela; `--force` ⇒ todas. `BuildPagePrompt(page)` inclui instrução + cada fonte com `state` e conteúdo (golden string).

**Commit:** `feat(compile): page set, input-hash dirty-tracking, prompt assembly`

---

## M4 — compile: CLI (delegação + síncrono)

### Task 4.1: Comando `cerne compile`

**Files:** Create `internal/cli/compilecmd.go` (+ teste); Modify `internal/cli/root.go`, `internal/store/layout.go` (`content/wiki` em `layoutSubdirsInit`)

**Comportamento:** `cerne compile [--scope <module>] [--force] [--ingest]`.
- **Delegação (default):** escreve `.docs/wiki/.pending/<page>.prompt.md` das páginas sujas; imprime instrução pt-BR (processe e escreva `content/wiki/<page>.md`, depois `cerne compile --ingest`). `--ingest`: para cada página pendente escrita, carimba frontmatter (`sources/compiled_from/input_hash`), `app.CommitAndSnapshot`, limpa `.pending/`.
- **Síncrono (modo fake nos testes):** itera as sujas, `Provider.Complete(BuildPagePrompt)`, escreve+carimba+commita numa fase.

**Step 1 — teste CLI (fake):** backend bare; `compile` (fake) cria `content/wiki/modules/<m>.md` carimbada; segundo `compile` ⇒ "nada a recompilar"; editar feature ⇒ só o módulo dela recompila; `--scope` restringe.

**Commit:** `feat(cli): cerne compile (delegation two-phase + fake sync path)`

---

## M5 — materialize/pull: projetar a wiki

### Task 5.1: `content/wiki/` → `.docs/wiki/`

**Files:** Modify `internal/materialize/materialize.go` (+ teste)

**Step 1 — teste:** com `content/wiki/modules/finance.md` + `content/wiki/project.md` no backend, `Materialize` copia-os para `.docs/wiki/…` e entra no manifest; conteúdo verbatim.

**Step 3:** loop que espelha `content/wiki/**` em `.docs/wiki/**` (mesmo cálculo de hash do manifest existente). Não altera a lógica de features/decisões/refs.

**Commit:** `feat(materialize): project compiled wiki into .docs/wiki`

---

## M6 — ask: recuperação + tagging + CLI

### Task 6.1: Recuperação vigente-primeiro, marcada por estado

**Files:** Create `internal/ask/ask.go` (+ teste)

**Step 1 — teste:** `Retrieve(repoDir, dbPath, q, history bool)` retorna trechos da wiki + hits do `index.Search`, cada um com `State`; default exclui terminais (`superseded/rejected/obsolete`); `history=true` inclui-os, ainda marcados. `BuildAskPrompt(q, snippets)` carrega cada trecho com seu estado + instrução de citar e respeitar estado (golden).

### Task 6.2: Comando `cerne ask`

**Files:** Create `internal/cli/askcmd.go` (+ teste); Modify `internal/cli/root.go`

**Comportamento:** `cerne ask "<pergunta>" [--history]`.
- **Delegação (default):** imprime o prompt montado no stdout (o agente responde inline).
- **Síncrono (fake):** `Provider.Complete` → imprime resposta + citações.

**Step 1 — teste CLI:** com fake, `ask` imprime resposta citando as fontes; sem `--history` nenhuma fonte terminal aparece no prompt; com `--history`, supersedida aparece **marcada** como tal (nunca como vigente).

**Commit:** `feat(cli): cerne ask (current-first retrieval, state-tagged, --history)`

---

## M7 — lint: regras de wiki

### Task 7.1: `wiki-source-unknown` + wiki no escopo de texto

**Files:** Modify `internal/lint/lint.go` (+ teste)

**Step 1 — teste:** página de wiki com `sources: [F-9999]` (inexistente) ⇒ finding `wiki-source-unknown`; página de wiki citando uma entidade supersedida ⇒ `ref-to-superseded` (páginas de wiki entram em `scannedDocs`).

**Step 3:** nova regra + constante `RuleWikiSourceUnknown` no `RuleOrder`; incluir `content/wiki/**` (ou a projeção) nos docs escaneados pelas regras de texto.

**Commit:** `feat(lint): validate wiki provenance and links`

---

## Ordem, dependências e dogfooding

M1 (llm) → M2 (wiki) → M3 (compile core) → M4 (compile CLI) → M5 (materialize) → M6 (ask) → M7 (lint). Cada task termina com commit; push ao fim de cada milestone.

**Dogfooding (manual, no `result.md`):** `cerne import` do próprio repo cerne num backend bare, depois `cerne compile` (modo delegação — este agente processa os prompts) e `cerne ask "qual o estado da camada de lint?"`, anexando a wiki gerada e a resposta.

**Fora deste plano** (deferred na spec): provider BYO-key (HTTP), UI (painel wiki + caixa ask), páginas temáticas, dois-passos no `ask`.

## Riscos / pontos de atenção

- **`Decision`/`Reference` sem `Module`** → daí a página `project.md` transversal; não tentar atribuir módulo a elas.
- **Custo de `compile`** em projeto grande (hub: 308 features) — `--scope` + incremental por input-hash são a defesa; documentar no help.
- **Determinismo:** todo teste usa o `Fake`; nunca chamar LLM real na suíte. O não-determinístico (texto) fica fora das asserções; o determinístico (page-set, input-hash, retrieval, tagging, ingest, lint) é o que se trava.
- **`ask` em delegação não retorna resposta** num terminal sem agente — é esperado (imprime o prompt). Documentar no help.
