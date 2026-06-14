# Resultado — Case 0018 (import auto-classifica módulos)

**Status:** Done (2026-06-14). Suíte: **435 testes** verdes, `go vet` limpo.

## O que foi entregue

`cerne import` deixou de criar subsistemas órfãos: agora roda o **backfill de classificação** após o fluxo de importação, **nos dois modos** e **mesmo sem candidatos novos**.

- **`internal/cli/importcmd.go` (`runImport`)** — removido o early-return cego ("nada/tudo já importado" antes de qualquer trabalho). Novo fluxo:
  - **dry-run** (`!yes`): plano de importação (ou "nada novo") **+** projeção do backfill via `app.ClassifyBackfill(repoDir, "", false)` → "Inferência classificaria N feature(s) sem módulo (M permaneceriam)." (só quando N>0; `projDir=""` é seguro porque apply=false nunca commita).
  - **apply** (`--yes`): aplica os docs novos (quando há) e **depois** `app.ClassifyBackfill(repoDir, projDir, true)` → "Classificadas N feature(s) por inferência (M sem módulo permanecem)." (só quando N>0). O backfill já commita+pusha+atualiza o lock no máximo uma vez, e nada quando N=0.
  - Helpers `reportNothingNew`, `reportClassifyProjection`, `reportClassifyResult`.
- Commits `d5878f3` (abertura), `32e9940` (impl + teste).

## Por que o studio sumia (causa-raiz)

O importer **já** infere módulo para cross-app cases ([importer.go](../../../internal/importer/importer.go) chama `classify.InferModule(source, specBody, modules)`), **mas só na 1ª importação de cada doc**, e o import é idempotente (pula `imported_from` já presente, nunca revisita). Os cases do studio foram importados **antes** de `studio` ser declarado como módulo — `InferModule` não casa um módulo não-declarado → ficaram sem módulo — e nunca mais foram revisitados. Como o `compile` pula módulos sem fontes, o subsistema inteiro sumia da wiki até um `cerne classify` manual. Agora o próprio `import` reclassifica as features órfãs que passaram a casar.

## Decisões (e desvio da spec)

| Fork | Decisão |
|---|---|
| Auto-rodar vs avisar | `import --yes` **roda** o backfill (mesma inferência single-match, alta precisão, só toca features sem módulo). |
| Rodar sem candidatos novos | Sim — o valor é reclassificar features **já importadas** após declarar o módulo. |
| **Desvio:** resíduo "sempre" reportado | A spec dizia reportar o resíduo sempre; na prática reporto **junto da classificação** (quando N>0) e deixo o `compile` como repórter padrão das sem-módulo, para **evitar ruído** em projetos sem módulos declarados (e não quebrar os testes de import existentes, que rodam sem módulos). |

## Verificação

| Item | Como | Resultado |
|---|---|---|
| Regressão do studio | CLI test `TestImportBackfillsModuleDeclaredAfterImport` | importa um root case com `finance` **não** declarado → módulo-less; declara `finance`; `import` dry-run projeta "classificaria 1" **sem escrever**; `import --yes` (nada novo) → F-0001 vira `finance`, saída "Classificadas 1" |
| Não-regressão | suíte de import existente (dry-run, yes, incremental, nothing, idempotência) | 9/9 verdes — sem módulos declarados o backfill é silencioso (N=0), saídas inalteradas |
| Suíte + vet | `go test ./...` / `go vet ./...` | 435 testes; vet limpo |
| Smoke no hub | `cerne import --yes` no hub (studio já corrigido no 0017-dogfood) | 0 classificadas novas (idempotente), sem commit vazio |

## Inventário

- **Código:** `internal/cli/importcmd.go` (`runImport` reestruturado + 3 helpers de report).
- **Testes:** `internal/cli/importcmd_test.go` (+`TestImportBackfillsModuleDeclaredAfterImport`, +helper `declareModules`; 434→435).
- **Commits:** `d5878f3`, `32e9940` (+ este result).

## Fora de escopo / follow-ups

- Heurística de inferência inalterada (folder + tags + conteúdo) — o case muda **quando** ela roda, não **como**.
- Reassignment de features que **já têm** módulo segue manual (`classify <id> --module`).
- Override de `[llm] mode` por env (`CERNE_LLM_MODE`) — follow-up #2 do dogfood do 0017, ainda aberto.
