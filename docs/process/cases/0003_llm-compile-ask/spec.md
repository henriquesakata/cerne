# Case 0003 — Camada LLM: `compile` e `ask` (v1.5)

tags: #spec #cerne #llm #wiki #compile #ask

**Status:** Draft — spec fechada via brainstorming (2026-06-13); pronta para `plan.md`.
**Tipo:** Nova camada **opcional** (LLM plugável) sobre o núcleo determinístico.
**Origem:** Corte v1.5 da [spec fundadora (case 0001)](../0001_founding-spec/spec.md) e pendência nº 6 do [result.md](../0001_founding-spec/result.md). É a parte da tese inspirada no [LLM Wiki do Karpathy](https://gist.github.com/karpathy/442a6bf555914893e9891c11519de94f), adiada de propósito para entregar o núcleo determinístico primeiro.

---

## Problema

O v1 entrega o estado vigente materializado (`.docs/`) e um `CONTEXT.md` **gerado deterministicamente** (índice tabular). Falta a camada de **síntese compilada** e o **Q&A**:

1. **Sem wiki de síntese**: nada *resume e cruza* o conhecimento (visão por módulo, "como a autenticação funciona hoje" agregando várias features/ADRs). O `CONTEXT.md` lista, não sintetiza.
2. **Sem Q&A**: para "por que abandonamos X?" ou "qual o estado atual de pagamentos?", hoje só existe `cerne search` (FTS por termo) — sem resposta em linguagem natural que combine vigente + mergulho no histórico.

A arquitetura do case 0001 já reserva o lugar: **núcleo determinístico + LLM plugável** (materialização, lifecycle, lint e busca rodam sem LLM; compilação e Q&A são camadas opcionais).

---

## Objetivo e critérios de sucesso

Dois comandos novos, opcionais, sobre uma interface de provider plugável cujo **único modo implementado nesta rodada é a delegação ao agente hospedeiro** (BYO-key fica como impl futura atrás da mesma interface):

- **`cerne compile`** — (re)compila páginas de síntese **por módulo** a partir das entidades + conteúdo vigentes. Incremental via **input-hash manifest** (recompila só módulos cujas fontes mudaram).
- **`cerne ask "<pergunta>"`** — Q&A: monta contexto (wiki + `index` FTS), **vigente-primeiro**, com cada fonte marcada por estado, e responde citando as entidades/docs usados.

**Critério de aceite:**

1. Núcleo 100% funcional **sem** `[llm]` configurado — todos os comandos do v1 inalterados; a suíte existente (266) segue verde.
2. `compile` (delegação, default) num projeto multi-módulo escreve prompt-bundles **só** dos módulos sujos; `--ingest` carimba proveniência e commita; re-rodar sem mudança ⇒ "nada sujo"; `--force` recompila tudo.
3. Com **provider fake** (teste), `compile` gera páginas ponta-a-ponta de forma determinística; mudar 1 entidade marca **exatamente** o módulo dela como sujo (input-hash).
4. `ask` (delegação) imprime um prompt montado com a pergunta + fontes **marcadas por estado** + instrução de citação; default exclui terminais; `--history` inclui (marcados). Com fake, devolve resposta canônica citando fontes.
5. `ask` **nunca** apresenta entidade substituída como vigente (o estado viaja com cada trecho).
6. `lint` flagra página de wiki cujo `sources` aponta entidade inexistente e link de wiki para entidade substituída.

---

## Decisões do brainstorming (2026-06-13)

| Fork | Decisão | Consequência |
|---|---|---|
| **Provider** | Interface `Provider` + **só delegação** implementada (BYO-key futuro) | Zero segredo, zero HTTP; dogfooda o agente que já roda; fake cobre os testes |
| **Handshake** | **Duas fases por arquivo** | `compile` prepare→`--ingest`; `ask` imprime prompt no stdout; sem chamada síncrona em delegação |
| **Wiki** | **Por módulo**, frontmatter de proveniência | Conjunto de páginas determinístico (= `project.json` modules); sem curadoria de temas |
| **Dirty-tracking** | **Input-hash manifest** | Espelha o sha256 do materialize/`docs.lock`; pega edição/adição/remoção; `--force` força |
| **Ask** | **Vigente-primeiro**, marcado por estado, `--history` opt-in | Retriever = `internal/index` + wiki; invariante de estado garantido pelas tags |
| **Escopo** | **A — núcleo CLI, ambos os comandos** | `compile` + `ask` no CLI; **sem UI** (follow-up) |

---

## Desenho

### `internal/llm` — provider plugável

```go
// Provider abstracts a single text completion. The deterministic core never
// calls it; only compile/ask do, and only outside delegation mode.
type Provider interface {
    Complete(ctx context.Context, prompt string) (string, error)
}
```

- **Modo `delegation` (default, implementado):** **não** usa `Provider.Complete`. O fluxo é em duas fases por arquivo (ver `compile`/`ask`). É o agente hospedeiro (Claude Code/Cursor), já em execução, que faz a síntese — mesmo padrão de `lint --fix-tasks`.
- **`Fake` (testes):** implementa `Complete` com texto determinístico (ex.: ecoa um resumo das fontes). Permite rodar `compile`/`ask` síncronos ponta-a-ponta nos testes, sem LLM real.
- **`byo-key` (futuro, fora de escopo):** impl HTTP atrás da mesma interface; chave via env (`CERNE_LLM_*`), nunca em `.cernerc`.

**Config** (`.cernerc`, novo bloco): `[llm] mode = "delegation"` (default quando ausente). Campos de BYO-key (`provider`, `model`, `key_env`) ficam **documentados mas inertes** nesta rodada. Segredo nunca mora no `.cernerc`.

### Wiki — páginas por módulo, versionadas

A wiki é **artefato de síntese versionado** (não projeção por máquina): páginas compiladas vivem no backend em `content/wiki/` (commitadas + pushadas, para a equipe não recompilar/regastar LLM), e o `pull` as projeta em `.docs/wiki/`.

**Conjunto de páginas** (determinístico). Atenção ao modelo de dados: só `Feature` carrega `Module`; `Decision`/`Reference` não. Logo:
- `content/wiki/modules/<module>.md` — uma por módulo do `project.json`, sintetiza as **features vivas** daquele módulo.
- `content/wiki/project.md` — página transversal: sintetiza **decisões ativas + referências vigentes** (que não têm módulo) + visão geral.
- Projeto **sem módulos declarados** ⇒ só `project.md`, agregando todas as features vivas + decisões + referências.

Frontmatter de proveniência (dirige dirty-tracking e o lint):

```
---
cerne_wiki: <module|project>
sources: [F-0001, F-0007]      # módulo: features vivas; project: D-*/R-* (e F-* quando sem módulos)
compiled_from: <backend-sha>
input_hash: <sha256>
---
```

`input_hash` = sha256 de uma serialização canônica das fontes vivas da página: ordenadas por ID, cada uma `ID|state|sha256(conteúdo)`. Captura edição de conteúdo, mudança de estado e adição/remoção de entidade.

### `cerne compile`

```
cerne compile [--scope <module>] [--force] [--ingest]
```

1. **Dirty-tracking:** para cada módulo, computa o `input_hash` atual e compara com o gravado na página existente; sujo se difere ou a página falta. `--force` marca tudo; `--scope` restringe a um módulo.
2. **Montagem de prompt (determinística, testável):** `BuildPagePrompt(module, sources, content)` — instrução + as fontes vivas do módulo com estado e conteúdo.
3. **Caminho delegação (default):** escreve os bundles em `.docs/wiki/.pending/<module>.prompt.md` (transiente, gitignored) e imprime instrução pt-BR para o agente processá-los e escrever `content/wiki/<module>.md`. `cerne compile --ingest` valida as páginas escritas, carimba o frontmatter (`sources`/`compiled_from`/`input_hash`), commita+pusha e limpa `.pending/`.
4. **Caminho síncrono (fake/byo):** itera os módulos sujos, chama `Provider.Complete`, escreve a página, carimba e commita — uma fase só. É o caminho exercido nos testes.

### `cerne ask`

```
cerne ask "<pergunta>" [--history]
```

1. **Recuperação (determinística):** wiki (síntese vigente) + `internal/index` FTS. Default só vigente; `--history` alarga a entidades terminais. **Cada trecho recuperado é marcado com `state`.**
2. **Montagem de prompt:** pergunta + trechos marcados + instrução de citar entidades/docs e respeitar o estado de cada fonte.
3. **Caminho delegação (default):** imprime o prompt montado no stdout — o agente hospedeiro responde inline.
4. **Caminho síncrono (fake/byo):** chama `Provider.Complete` e imprime a resposta com citações.

### Lint — regras de wiki

Estende `internal/lint`: (a) `wiki-source-unknown` — `sources` do frontmatter que não resolve a uma entidade existente; (b) páginas de wiki entram no escopo das regras de texto já existentes (`ref-to-superseded`, `broken-link`), pegando link de wiki para entidade substituída.

---

## Determinismo de teste

- **Provider `Fake`** (texto canônico) cobre o caminho síncrono ponta-a-ponta de `compile`/`ask`.
- Testa-se o **determinístico**: montagem de prompt, recuperação/ordem do retriever, cálculo do `input_hash` e a detecção de sujo, validação no `--ingest`, regras de lint da wiki, e o tagging de estado nas fontes do `ask`. **Não** se testa o texto gerado por LLM real.

---

## Fora de escopo (deferred)

- **Provider BYO-key** (HTTP/endpoint, chave por env) — interface pronta, impl futura.
- **UI** (painel de wiki, caixa de `ask`) — follow-up, como o M6 foi no case 0001.
- **Páginas temáticas transversais** (curadas em `wiki.toml`) — só por módulo nesta rodada.
- **Dois-passos no `ask`** (wiki→FTS sob julgamento de suficiência) — recuperação direta basta.

---

## Questões em aberto — resolvidas

1. **Onde mora a chave / como escolher modo?** → `.cernerc [llm] mode` (default `delegation`, sem chave). BYO-key (chave por env) é futuro.
2. **Dirty-tracking precisa estender eventos?** → Não. Input-hash no frontmatter da página, recomputado no `compile`.
3. **Custo em projeto grande?** → `--scope <module>` + recompila só o sujo + wiki versionada (não recompila o que não mudou).
4. **`ask` e histórico?** → Vigente por default; `--history` opt-in; estado sempre marcado.
5. **Testar sem LLM real?** → Provider `Fake`; testar montagem/retrieval/manifest, não o texto.

---

## Plano de implementação (ordem TDD — esboço para `plan.md`)

1. **`internal/llm`**: `Provider` + `Fake` + resolução de `mode` no `.cernerc` (default delegation).
2. **`internal/wiki`** (puro): modelo de página + frontmatter de proveniência + `InputHash(module, sources)`.
3. **compile — dirty-tracking + montagem de prompt** (determinístico), table-driven.
4. **compile — CLI**: caminho delegação (prepare/`--ingest`) + caminho síncrono (fake); `--scope`/`--force`; commit+snapshot reusando `internal/app`.
5. **materialize/pull**: projetar `content/wiki/` → `.docs/wiki/`.
6. **ask — recuperação + tagging de estado + montagem** (reusa `internal/index`); CLI (imprime / fake responde); `--history`.
7. **lint**: `wiki-source-unknown` + páginas de wiki no escopo das regras de texto.
8. **Dogfooding (manual, no `result.md`)**: `compile` do próprio backend do cerne após `import`.

## Próximos passos

1. `plan.md` (skill `writing-plans`) com milestones/commits TDD na ordem acima.
2. Implementação subagente-dirigida.
3. `result.md`.
