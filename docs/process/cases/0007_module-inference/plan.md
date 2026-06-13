# Plano — Case 0007 (inferência/atribuição de módulo)

**Goal:** Atribuir módulo a features sem módulo via heurística conservadora (folder + tags), com backfill `classify` (dry-run/--yes) + manual, e integração no import; enriquecendo a wiki do hub.

## Tarefas (TDD, uma por commit)

1. **Núcleo `InferModule`** (novo pacote `internal/classify` ou em `internal/importer`): `InferModule(importedFrom, content string, modules []string) (string, bool)` — candidatos = módulos que casam como token whole-word (via `slug.Fold`) no último segmento de `importedFrom` OU como tag `#<módulo>` no `tags:` do conteúdo; retorna só com 1 candidato distinto. Testes table-driven: folder único; tag única; folder+tag concordam; 2 módulos distintos ⇒ ("",false); nenhum ⇒ ("",false); case/acento-insensível. → commit `feat(classify): conservative single-match module inference`.
2. **`cerne classify` backfill** (`internal/app` + `internal/cli/classifycmd.go`): para cada feature **sem módulo**, lê `imported_from` (do JSON) + `content/features/<id>/spec.md`, roda `InferModule`. Dry-run (default) imprime `F-xxxx → módulo` ordenado + "N ficam sem módulo". `--yes`: seta `Module`, evento `feature.classified` (source heuristic), 1 commit+push, atualiza lock. Testes: dry-run não toca backend; --yes atribui só as inequívocas; idempotente (2ª rodada não reatribui já-classificadas → "nada a classificar"). → commit `feat(cli): classify backfills modules for module-less features`.
3. **`cerne classify <id> --module <m>` manual** (mesmo cmd): valida módulo declarado + id existente; seta Module; evento source manual; commit+push. Erro pt-BR se módulo não declarado / id inexistente. Teste. → commit `feat(cli): manual single-feature module assignment`.
4. **Importer** (`internal/importer`): cases raiz sem módulo de path chamam `InferModule(folder, specContent, modules)`; nasce com o módulo quando casa. Testes ajustados/novos no fixture hubtree (adicionar um case raiz com tag/nome de módulo declarado → vem com módulo; um ambíguo → sem módulo). → commit `feat(importer): infer module for root cases at import time`.

## Verificação final

- `go vet ./...` + `go test ./... -count=1` verdes.
- Dogfood no clone do hub: `classify` dry-run (quantas das 236), `--yes`, `compile --force` (páginas de módulo maiores, nota "sem módulo" menor). Números no result.md.
