# Resultado — Case 0004 (idempotência do registro histórico)

**Status:** Done (2026-06-13). Suíte: 301 testes verdes, `go vet` limpo.

## O que foi entregue

Registro histórico (case 0002) agora é **idempotente** via **upsert por nome normalizado** no núcleo `SaveHistoricalEntity` ([internal/app/historical.go](../../../internal/app/historical.go)), compartilhado por `cerne retire` e pelo seed `historical.toml` do `import`.

- **`slug.Fold`** ([internal/slug/slug.go](../../../internal/slug/slug.go)) — chave canônica case+acento-insensível (lowercase + transliteração de diacríticos, preservando a string). `slug.Make` passou a reusá-la.
- **Upsert** — entidade existente (mesmo `Fold(name)`) é atualizada in-place: mantém `ID`+`CreatedAt`, sobrescreve `superseded_by`/`reason`/`imported_from`, **une aliases** (existentes ∪ novos, dedup por `Fold`, ordem estável). Evento `historical.updated` no update, `historical.recorded` na criação.
- **Validação antes do lookup** — entrada malformada nunca toca o backend.
- **CLI** — `cerne retire` distingue criação ("H-000X registrada") de atualização ("H-000X atualizada (já existia como histórica…)").

## Decisões (ver spec)

Upsert (não no-op nem erro) por ser a única semântica que faz re-import de seed convergir ao estado declarado sem descartar aliases novos. Chave = nome normalizado. Limpeza de duplicatas e dedup de findings no lint ficaram fora de escopo (a idempotência elimina a causa raiz; o `hub-docs` real não tem duplicatas).

## Verificação no corpus real do hub

Clone bare local do `hub-docs` (308 features; H-0001 Bills + H-0002 Investments já presentes):

| | Antes do re-registro | Depois do re-registro |
|---|---|---|
| Históricas | 2 (H-0001, H-0002) | **2** (sem H-0003/H-0004) |
| `ref-to-superseded` | 89 | **89** (estável) |

Com o binário **antigo**, o re-registro levava a `ref-to-superseded = 160` (mesma menção contada 2×/4× por causa das entidades duplicadas). Bug fechado.

## Inventário

- **Código:** `internal/slug/slug.go` (+`Fold`), `internal/app/historical.go` (upsert + `unionAliases` + `findHistoricalByName`), `internal/store/entity.go` (suporte), `internal/cli/*` (mensagem de update).
- **Testes:** `internal/app/historical_test.go`, `internal/cli/*` (retire idempotência), `internal/importer/*` (seed idempotência), `internal/lint/*` (regressão do double-count).
- **Commits:** `cc81462` (slug.Fold), `9231dde` (upsert app), `cec86e4` (CLI), `9e2370d` (import seed), `625f7cb` (lint regression), + docs deste case.

## Follow-ups (não bloqueiam)

- `cerne forget <id>` para remover entidade histórica criada por engano (não necessário hoje).
- Dedup de findings do lint por `(arquivo, menção)` caso duas históricas legítimas venham a compartilhar um alias.
