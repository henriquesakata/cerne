# Case 0020 — Referências externas (fonte de conhecimento + marca + backlink)

tags: #spec #cerne #references #ui #ask #provenance

**Status:** Done (2026-06-14) — ver [result.md](result.md)
**Tipo:** Nova faceta da entidade Reference (URL + marca externa + backlink `informs`) + comando CLI `reference new` + endpoint/forma na UI + citação no `ask`.
**Origem:** Pedido do usuário: cadastrar **documentação/artigos externos** como fonte de informação, com uma **marca** "fonte externa" em Referências; e — quando o **agente** resolve um problema a partir de um artigo — ele mesmo registrar o artigo, ligado à correção/decisão, como **histórico de decisões e fonte da correção**.

---

## Problema

Hoje uma `Reference` é `{ID, Title, State, timestamps}` + um content-doc, e **só nasce via `cerne import`** (de docs do próprio repo; `imported_from` = caminho no repo). Não há: (1) conceito de fonte **externa** (URL), (2) jeito de cadastrar fora do import, (3) marca distinguindo externo/interno, (4) vínculo entre a fonte e a correção/decisão que ela motivou. Sem isso, um artigo que embasou uma correção se perde — não vira conhecimento citável pela IA nem registro rastreável.

---

## Objetivo e critérios de sucesso

1. **Modelo:** `Reference` ganha `URL`, `External bool` (a marca) e `Informs []string` (IDs `F-`/`D-` que a fonte motivou). O **conteúdo** (texto/resumo colado) é o content-doc da ref → entra no índice FTS → o `ask` usa e cita.
2. **Criar (app-layer compartilhado)** por **CLI e UI**: valida URL/título não-vazios e que cada `informs` **existe** (sem backlink pendurado); cunha `R-NNNN`, `External:true`; grava content-doc com `cerne_id`; evento `reference.created`; commit+push+lock.
   - CLI: `cerne reference new "<título>" --url <url> [--informs F-0042,D-0007] [--file <path> | stdin]`.
   - UI: formulário na página Referências → `POST /api/references`.
3. **Marca + backlink bidirecional na UI:** lista/detalhe de Referências mostram badge **"externa"** + URL clicável e, no detalhe, "Informou: F-0042" (links); a página da **feature/decisão** mostra "Fontes: R-0042 (externa, link)" (lado reverso, via `app.ReferencesInforming`).
4. **`ask`:** fontes externas recuperadas são anotadas com a URL (ex.: `R-0042 (referência externa, <url>)`), para a IA citar marcando como externa.
5. Suíte + `go vet` verdes; build da UI ok (sem dep nova).

---

## Decisões do design

| Fork | Decisão | Razão |
|---|---|---|
| **Entidade** | Estender `Reference` (não criar tipo novo) | Referências já existem (UI/índice/ask/wiki); novo tipo duplicaria tudo. |
| **Marca** | Campo explícito `External bool` (+ `URL`) | É literalmente a "marca" pedida; não confunde "tem link" com "é externa". |
| **Conteúdo** | Texto colado vira content-doc da ref (indexado) | Reusa o mecanismo das refs internas; `ask` passa a citar sem código novo de indexação. |
| **Vínculo** | Backlink estruturado `Informs []string` (F/D), bidirecional | "Histórico de decisões / fonte da correção": navegável dos dois lados; valida existência (sem pendurado). Citação livre no doc continua valendo, mas o vínculo estruturado é durável e aparece na UI. |
| **Criação** | CLI **e** UI sobre um `app.CreateExternalReference` único | Agente (CLI) registra a fonte que achou; humano (UI) cura artigos. |
| **Reverso** | Computado (`ReferencesInforming(id)` varre refs por `Informs`), não armazenado no F/D | Uma fonte da verdade (a ref); evita sincronizar dois lados. |

---

## Desenho

### Backend (Go)
- `store.Reference` + `URL string \`json:"url,omitempty"\``, `External bool \`json:"external,omitempty"\``, `Informs []string \`json:"informs,omitempty"\``.
- `app.CreateExternalReference(repoDir, projDir, title, url, content string, informs []string) (store.Reference, error)` (novo `app/reference.go`): valida (título/url não-vazios; cada informs é `F-`/`D-` e existe via `LoadFeature`/`LoadDecision`); `NextID(repoDir,"R")`; `SaveReference`; grava `content/references/<ID>.md` com `cerne_id`; evento `reference.created` (payload url/external); `CommitAndSnapshot`. Espelha `app.CreateDecision`.
- `app.ReferencesInforming(repoDir, entityID string) ([]store.Reference, error)`: `LoadAllReferences` filtradas por `Informs` contendo `entityID`.
- `internal/server`: `POST /api/references` (`{title, url, content, informs}`); o detalhe de reference inclui `url/external/informs`; os detalhes de **feature** e **decision** incluem `informed_by: [R-…]` (via `ReferencesInforming`); `ask.Retrieve`/`BuildAskPrompt` anotam a URL de refs externas.

### CLI
- `cerne reference new` (cria o comando-pai `reference` em `root.go`): flags `--url` (obrig.), `--informs` (csv), `--file` (senão lê stdin) para o conteúdo; chama `app.CreateExternalReference`; imprime o `R-NNNN`.

### UI
- `api.ts`: `createReference`, tipos com `url/external/informs/informed_by`.
- `References.tsx`: badge "externa" + link na lista; detalhe com URL + "Informou:"; formulário "Nova referência externa".
- `FeaturePage.tsx`/`Decisions.tsx` (detalhe): seção "Fontes" com as refs `informed_by`.

---

## Verificação
- **app/CLI (TDD):** `CreateExternalReference` cunha R, grava entity+content+evento, commit; `--informs` inexistente → erro (sem escrita); URL vazia → erro; conteúdo via stdin/arquivo. `ReferencesInforming` retorna as refs certas.
- **server (httptest):** `POST /api/references` cria (200 + R) e persiste url/external/informs; feature/decision detail trazem `informed_by`; ref detail traz url/external/informs.
- **ask:** ref externa recuperada → URL aparece nas fontes/prompt.
- **UI:** build ok; badge + formulário + backlinks (revisão + build).
- `go vet` + suíte verdes.

---

## Fora de escopo
- Buscar/baixar a URL automaticamente (offline; conteúdo é colado).
- Editar `informs`/conteúdo de uma ref após criada (v1 cria; edição depois é outro case).
- Campo de autor/data-de-publicação além da URL.
- Lifecycle próprio (externas usam `current`/`superseded` como as demais).
