# Plano — Case 0005 (usabilidade wiki/ask)

> Registrado retroativamente: implementação em TDD direto a partir das decisões da spec (A/B/C). Documenta a sequência seguida para manter o trio spec/plan/result.

**Goal:** Tornar `cerne compile`/`ask` úteis num projeto maduro, corrigindo os três gaps confirmados na revisão do case 0003.

## Tarefas (TDD, uma por commit)

1. **Fix A — implemented na wiki** (`internal/compile/compile.go`): renomear `liveFeatureStates` → `wikiSourceStates` e adicionar `domain.StateImplemented`. Conjunto local ao pacote `compile` (não tocar `materialize`/`lint`). Teste: feature implemented num módulo aparece nas Sources da página do módulo. → commit `feat(compile): include implemented features in wiki sources`.
2. **Fix B — excerpt limitado** (`internal/compile/compile.go`): `const maxSourceExcerptChars = 800` + `excerpt()` (corte em fronteira de runa, prefere última quebra de linha no cap, marcador `…(conteúdo truncado)`), aplicado por fonte em `BuildPagePrompt`. **`InputHash` inalterado** (continua sobre conteúdo integral). Teste: fonte longa truncada com marcador; fonte curta inteira; `InputHash` idêntico ao hash do conteúdo integral. → commit `feat(compile): bound per-source excerpt in page prompt`.
3. **Fix C — keywords no `ask`** (`internal/ask/ask.go`): `stopwords` (pt-BR+en) + `extractKeywords()` (tokeniza unicode, lowercase, descarta <3 runas e stopwords, dedup, OR-join; vazio ⇒ query original), usado antes do `index.Search`. Teste: extração com stopwords removidas; tudo-stopword ⇒ original; `Retrieve` recupera o hit que a query literal perderia. → commit `feat(ask): extract keywords from natural-language questions`.

## Verificação final

- `go vet ./...` + `go test ./... -count=1` verdes (307).
- Dogfood no clone do hub: `compile --force` (medir prompts), `ask` com pergunta NL (medir recall). **Resultado e a descoberta da página `project` grande estão no result.md** (→ case 0006).
