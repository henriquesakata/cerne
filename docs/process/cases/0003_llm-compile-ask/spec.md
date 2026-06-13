# Case 0003 — Camada LLM: `compile` e `ask` (v1.5)

tags: #spec #cerne #stub #llm #wiki

**Status:** Draft (stub — precisa de rodada de brainstorming para fechar decisões)
**Tipo:** Nova camada opcional (LLM plugável) sobre o núcleo determinístico.
**Origem:** Corte v1.5 da [spec fundadora (case 0001)](../0001_founding-spec/spec.md) e pendência nº 6 do [result.md](../0001_founding-spec/result.md). É a parte da tese inspirada no [LLM Wiki do Karpathy](https://gist.github.com/karpathy/442a6bf555914893e9891c11519de94f) que foi deliberadamente adiada para entregar o núcleo determinístico primeiro.

---

## Problema

O v1 entrega o estado vigente materializado (`.docs/`) e um `CONTEXT.md` **gerado deterministicamente** (índice tabular de features/decisões/referências). Falta a camada de **síntese compilada** e o **Q&A**:

1. **Sem wiki de síntese**: não há páginas que *resumam e cruzem* o conhecimento (visão por módulo, "como a autenticação funciona hoje" agregando várias features/ADRs). O `CONTEXT.md` lista, não sintetiza.
2. **Sem Q&A**: para perguntar "por que abandonamos X?" ou "qual o estado atual de pagamentos?", hoje só existe `cerne search` (FTS por termo). Não há resposta em linguagem natural que combine estado vigente + mergulho no histórico.

A decisão de arquitetura do case 0001 já reserva o lugar disto: **núcleo determinístico + LLM plugável** (materialização, lifecycle, lint e busca funcionam sem LLM; compilação e Q&A são camadas opcionais).

---

## Objetivo e critérios de sucesso (rascunho)

Dois comandos novos, ambos opcionais e dependentes de um provider LLM configurado:

- **`cerne compile`** — (re)compila páginas de síntese (`.docs/wiki/`) a partir das entidades + conteúdo vigentes. Incremental: o log de eventos do case 0001 já marca páginas "sujas" quando entidades mudam; `compile` recompila só o que está sujo.
- **`cerne ask "<pergunta>"`** — Q&A: responde a partir da wiki + estado vigente, mergulhando no histórico (`index` FTS) quando necessário; cita as entidades/docs usados.

Critérios-alvo:
- Núcleo continua 100% funcional **sem** LLM configurado (`compile`/`ask` falham com mensagem clara orientando a configurar um provider).
- `compile` é determinístico no *que* recompila (dirty-tracking via eventos), não-determinístico só no texto gerado.
- `ask` nunca "inventa" entidades supersedidas como vigentes (a resposta respeita o estado).

---

## Direção a explorar (alternativas — decidir no brainstorming)

1. **Provider LLM plugável** (interface `LLMProvider` em novo pacote `internal/llm`): implementações para (a) **BYO API key** (chamada direta a um endpoint compatível — Claude/OpenAI-like, configurável via `.cernerc`/env) e (b) **delegação ao agente hospedeiro** (cerne emite *prompts/tarefas* que o agente que já roda — Claude Code, Cursor — executa, sem o cerne ter chave). O case 0001 já usa o padrão "(b)" no `lint --fix-tasks`.
2. **Formato da wiki**: páginas markdown por módulo + páginas por tema, com front-matter de proveniência (quais entidades alimentaram a página) para o dirty-tracking e para o lint poder validar links.
3. **`ask` arquitetura**: recuperação (wiki + FTS do `index`) → síntese pelo provider → resposta com citações. Reusar o `internal/index` existente como retriever.

---

## Questões em aberto

1. Configuração do provider: onde mora a chave (env `CERNE_LLM_*`? `.cernerc` nunca commita segredo)? Como escolher entre modo BYO-key e modo delegação?
2. Dirty-tracking: o log de eventos já marca o quê como "sujo"? Precisa estender o case 0001 para registrar a relação entidade→página-wiki?
3. Custo/limites: `compile` de um projeto grande (o hub tem 308 features) pode ser caro — escopo por módulo, cache por hash de conteúdo, opt-in explícito.
4. `ask` responde só sobre o vigente por padrão, com flag `--history` para incluir supersedidos? Ou sempre cita o estado de cada fonte?
5. Determinismo de testes: como testar `compile`/`ask` sem chamar LLM real? (provider fake/mock; testar a montagem de prompt e o retrieval, não o texto gerado).

---

## Próximos passos

1. Rodada de **brainstorming** para fechar o modelo de provider (BYO-key vs delegação) e o formato da wiki.
2. `plan.md` (TDD): `internal/llm` (interface + fake) → dirty-tracking → `compile` → `ask` → integração com a UI (painel de wiki / caixa de pergunta).
3. `result.md`.
