# Case 0007 — Inferência/atribuição de módulo para features sem módulo

tags: #spec #cerne #import #compile #classify

**Status:** Done — implementado e verificado no corpus do hub (2026-06-13); ver [result.md](result.md)
**Tipo:** Heurística de classificação (núcleo) + comando `classify` (backfill + manual) + integração no `import`.
**Origem:** Follow-up do [case 0006](../0006_wiki-page-granularity/spec.md). A wiki só sintetiza agrupamentos coerentes (módulos); no hub, 236 features ficaram sem módulo porque o `import` não atribui módulo aos cases raiz (`docs/process/cases/`), só aos de `apps/<app>/...`. Trazer essas features para as páginas de módulo certas enriquece a wiki.

---

## Problema

Features sem módulo não entram em página de wiki nenhuma (decisão do 0006), ficando só no `CONTEXT.md`/`search`. No hub são 236 (217 implemented). Muitas têm módulo *óbvio* (`0021_finance-consolidation` → finance; spec com `tags: #identity` → identity), mas o importador descarta esse sinal. Falta um caminho — automático e conservador — de atribuir módulo a essas features.

**Risco a evitar:** classificar errado polui a wiki (síntese sob o módulo errado) — um erro silencioso pior que não classificar. A heurística precisa ser **conservadora** e **revisável**.

---

## Objetivo e critérios de sucesso

Atribuir módulo a features sem módulo a partir de sinal de alta confiança, mantendo o humano/agente no loop.

**Critério de aceite:**
1. `cerne classify` (dry-run) no hub lista as atribuições propostas (`F-xxxx → módulo`) e o nº das que ficam sem módulo; `--yes` aplica e a wiki passa a sintetizá-las na página de módulo certa (re-`compile` mostra páginas de módulo maiores e a nota de "sem módulo" menor).
2. Atribuição **conservadora**: só quando exatamente um módulo declarado casa; 0 ou ≥2 candidatos ⇒ continua sem módulo (genuinamente transversal).
3. `cerne classify <id> --module <m>` atribui manualmente (preciso, sem heurística).
4. Importer aplica a mesma inferência em projetos futuros (cases raiz nascem com módulo quando o sinal é claro).
5. Suíte verde, `go vet` limpo; idempotente (re-rodar `classify` não reatribui o que já tem módulo nem duplica).

---

## Decisões do design (2026-06-13)

| Fork | Decisão | Razão |
|---|---|---|
| **Sinal** | Nome da pasta (`imported_from`, tokens whole-word) **+** tags do conteúdo (`#<módulo>`) casadas contra `project.json.modules` | Ambos disponíveis nos dados já importados; tags são sinal limpo, folder é reforço |
| **Confiança** | **Match único**: assina só se exatamente 1 módulo distinto casa entre os dois sinais; 0 ou ≥2 ⇒ sem módulo | Evita classificar transversal/ambíguo errado |
| **Caminho** | **Backfill `classify` (dry-run/--yes) + manual `classify <id> --module`**, núcleo compartilhado com o `import` | Hub já importado precisa de backfill; manual cobre precisão; import cobre futuro |
| **Segurança** | **Dry-run por padrão**; só toca features **sem módulo** (não reclassifica) | Humano revisa antes; não mexe no que já está classificado |
| **Evento** | `feature.classified` `{id, module, source: heuristic|manual|import}` | Auditável no log |

---

## Desenho

### Núcleo — `inferModule`

```go
// InferModule returns the single declared module that the case's folder name
// (from imported_from) and content tags point to, or ("", false) when zero or
// more than one distinct module matches (ambiguous / cross-cutting).
func InferModule(importedFrom, content string, modules []string) (string, bool)
```
- Candidatos: módulos que aparecem como token whole-word (case-insensitive, via `slug.Fold`) no último segmento do `importedFrom` **ou** como tag `#<módulo>` no `tags:` do conteúdo.
- Retorna o módulo só se o conjunto de candidatos distintos tem tamanho 1.

### `cerne classify`

```
cerne classify [--yes]            # backfill heurístico (dry-run por padrão)
cerne classify <id> --module <m>  # atribuição manual de uma feature
```
- **Backfill:** para cada feature **sem módulo**, roda `InferModule`. Dry-run imprime a tabela `F-xxxx → módulo` + "N ficam sem módulo (transversais/ambíguas)". `--yes` aplica: seta `Module`, evento `feature.classified` (source `heuristic`), um único commit+push, atualiza lock.
- **Manual:** valida que `<m>` é módulo declarado e que `<id>` existe; seta `Module`, evento (source `manual`), commit+push.

### Importer

No scan de cases raiz (sem módulo de path), chamar `InferModule(folder, specContent, modules)`; se casar, a Feature nasce com esse módulo (evento de import normal). Cases de `apps/<app>` seguem com módulo do path.

---

## Verificação (corpus real do hub)

Dry-run + `--yes` de `classify` no clone do hub, depois `compile --force`: medir quantas das 236 são atribuídas e a queda na nota de "sem módulo" + crescimento das páginas de módulo. Números no [result.md](result.md).

---

## Fora de escopo

- NLP/embeddings para classificação (heurística determinística basta).
- Reclassificar features que já têm módulo.
- Sugestão interativa item-a-item (o dry-run + manual cobrem).
