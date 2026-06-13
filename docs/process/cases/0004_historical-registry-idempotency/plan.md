# Plano — Case 0004 (idempotência do registro histórico)

> Registrado retroativamente: a implementação foi feita em TDD direto a partir da
> decisão da spec (upsert por nome normalizado). Este plano documenta a sequência
> efetivamente seguida, para manter o trio spec/plan/result do case.

**Goal:** Tornar `cerne retire` e o seed `historical.toml` idempotentes — re-rodar converge ao estado declarado, sem criar entidades `H-` duplicadas nem inflar a contagem do lint.

**Abordagem:** Upsert no núcleo compartilhado `SaveHistoricalEntity` (já usado por `retire` e pelo `import`), chaveado por `slug.Fold(name)`.

## Tarefas (TDD)

1. **`slug.Fold`** — extrair de `slug.Make` a dobra case+acento (lowercase + transliteração de diacríticos, preservando a string), para ser a chave canônica de comparação de nomes. Teste: `Fold("BILLS")==Fold("bills")`, `Fold("Aplicação")=="aplicacao"`, nomes distintos seguem distintos. → commit `feat(slug): add Fold`.
2. **Upsert no app** — `SaveHistoricalEntity` procura histórica existente por `Fold(name)`; se acha, atualiza in-place (mantém `ID`/`CreatedAt`, sobrescreve `superseded_by`/`reason`/`imported_from`, une aliases dedup por `Fold`, ordem estável) e emite `historical.updated`; senão cria (`NextID`, nasce `superseded`) e emite `historical.recorded`. Validação roda antes do lookup. Teste: dupla chamada → 1 entidade; aliases novos são unidos; evento correto por caminho. → commit `feat(app): upsert historical entities by normalized name`.
3. **CLI `retire`** — mensagem distinta para update ("já existia como histórica…") vs criação. Teste table-driven pinando idempotência + a mensagem. → commit `test(cli): pin retire idempotency and update message`.
4. **Seed import** — re-importar o mesmo `historical.toml` ⇒ zero entidades novas (reusa o núcleo). Teste. → commit `test(import): pin historical seed import idempotency`.
5. **Regressão de lint** — reproduzir o double-count: registrar duas vezes e assertar que `ref-to-superseded` conta cada menção uma única vez (a causa raiz — entidades duplicadas — deixa de existir). → commit `test(lint): regression for ref-to-superseded double-count on re-register`.

## Verificação final

- `go vet ./...` + `go test ./... -count=1` verdes.
- Dogfood: clone bare local do `hub-docs`, re-`retire` Bills/Investments, confirmar contagem estável (89) e ausência de duplicatas.
