# Resultado — Case 0007 (inferência/atribuição de módulo)

**Status:** Done (2026-06-13). Suíte: 341 testes verdes, `go vet` limpo.

## O que foi entregue

Heurística conservadora de classificação de módulo + comando `classify` (backfill + manual) + integração no `import`, para trazer features sem módulo às páginas de módulo certas (case 0006).

- **Núcleo `InferModule`** (`internal/classify/classify.go`): candidatos de módulo a partir de (a) token whole-word no último segmento do `imported_from` e (b) tag `#<módulo>` no conteúdo, casados via `slug.Fold` contra `project.json.modules`. Retorna módulo **só com match único** (0 ou ≥2 ⇒ sem módulo). Commit `fb3d79a`.
- **`cerne classify`** (`internal/app/classify.go` + `internal/cli/classifycmd.go`): backfill dry-run por padrão (lista `F-xxxx → módulo` + nº que permanece); `--yes` aplica, evento `feature.classified` (source `heuristic`), um commit+push, lock atualizado. Só toca features **sem módulo** ⇒ idempotente. Commit `8ae8865`.
- **Atribuição manual**: `cerne classify <id> --module <m>` (source `manual`, valida módulo declarado + id; reclassificação permitida no modo manual). Commit `dfd4151`.
- **Importer** (`internal/importer`): cases raiz sem módulo de path chamam `InferModule` e nascem com módulo quando o sinal é claro; `apps/<app>` seguem com módulo do path. Commit `4503810`.

**Nota de implementação:** `store.Feature` não persiste `imported_from` (só `store.Historical` tem). O backfill lê o `imported_from` do frontmatter do `spec.md` (a chave que o importer injeta), via `doccheck.FrontmatterValue`. Quem precisar de proveniência na entidade no futuro deve adicionar o campo a `store.Feature`.

## Verificação no corpus real do hub

Clone bare local do `hub-docs`, `cerne classify` → `compile --force`:

| | Antes (0006) | Depois (0007) |
|---|---|---|
| Features classificadas | 0 | **93** (de 236; 145 permanecem transversais) |
| Páginas de módulo na wiki | 3 (finance/identity/marketplace) | **6** (+one, studio, commons) |
| `finance.md` / `identity.md` / `marketplace.md` | 895 / 526 / 280 linhas | **1232 / 1099 / 824** |
| Nota "sem módulo" | 236 | **144** |

A wiki passou a cobrir ~92 features que antes só apareciam no índice/search, distribuídas nas páginas de módulo certas — sem classificar à força as 144 genuinamente transversais (conservadorismo do match único). Páginas seguem limitadas (maior: references 1591).

## Decisões (ver spec)

Match único (folder + tags) por ser determinístico, usar o sinal já disponível nos dados importados, e errar para o lado seguro (não classifica ambíguo/transversal). Dry-run por padrão mantém o humano no loop. Importer aplica o mesmo núcleo para projetos futuros não recriarem o gap.

## Inventário

- **Código:** `internal/classify/classify.go` (novo), `internal/app/classify.go` (novo), `internal/cli/classifycmd.go` (novo) + `root.go`, `internal/importer/*` (Scan/Apply recebem modules).
- **Testes:** `internal/classify/*`, `internal/app/*`, `internal/cli/*`, `internal/importer/*` (+25; 316→341), fixture hubtree com case raiz inferível + ambíguo.
- **Commits:** `fb3d79a`, `8ae8865`, `dfd4151`, `4503810` + docs deste case.

## Follow-ups

- As 144 transversais restantes: ou são genuinamente cross-cutting (correto ficarem sem módulo) ou pedem atribuição manual pontual (`cerne classify <id> --module`). Sem ação automática adicional recomendada.
- Dívida 0005/0003: caminho síncrono `Provider.Complete` / BYO-key segue exercido só pelo `Fake`.
- UX (observado no dogfood do hub, fora deste case): impasse `cached backend clone missing` vs `init` recusando com `.cernerc` existente — candidato a case próprio (`status`/`pull` recriarem o clone, ou `init --recreate-cache`).
