# Plano — Case 0020 (referências externas)

**Goal:** cadastrar artigos/docs externos como referência (URL + marca externa + conteúdo indexado), por CLI e UI, com backlink bidirecional `informs` (fonte ↔ correção/decisão) e citação no `ask`.

## Frente A — modelo + app + CLI (TDD)

1. **Modelo + `CreateExternalReference` + `ReferencesInforming`** (`store/entity.go` + novo `app/reference.go` + `app/reference_test.go`): campos `URL/External/Informs` na `Reference`; `CreateExternalReference(repoDir,projDir,title,url,content,informs)` (valida título/url; cada `informs` `F-`/`D-` existe; `NextID("R")`; `SaveReference`; content-doc `content/references/<ID>.md` com `cerne_id`; evento `reference.created`; `CommitAndSnapshot`); `ReferencesInforming(repoDir,id)`. Testes: cria (entity+content+evento+commit); informs inexistente → erro sem escrita; url vazia → erro; reverse lookup. → commit `feat(app): external references with informs backlink`.

2. **CLI `cerne reference new`** (`internal/cli/referencecmd.go` + `root.go` + teste): comando-pai `reference` + `new "<título>" --url <url> [--informs F-..,D-..] [--file <p>]` (senão stdin); chama o app; imprime `R-NNNN`. Teste: cria via CLI (conteúdo por arquivo) + `--informs` válido; erro em informs inexistente. → commit `feat(cli): cerne reference new — register an external source`.

Verificar: `go vet` + suíte verdes.

## Frente B — server (TDD)

3. **Endpoints** (`internal/server` + testes httptest): `POST /api/references` (`{title,url,content,informs}` → 200 + ref); ref detail inclui `url/external/informs`; **feature** e **decision** detail incluem `informed_by` (via `ReferencesInforming`); `ask.Retrieve`/`BuildAskPrompt` anotam a URL das refs externas. → commit `feat(server): create external refs + informed_by backlinks + ask URL`.

## Frente C — UI (após re-sync)

4. **`api.ts`** (`createReference`, tipos `url/external/informs/informed_by`); **`References.tsx`** (badge "externa" + link na lista; detalhe com URL + "Informou:"; formulário "Nova referência externa"); **feature/decisão detalhe** seção "Fontes" (`informed_by`). Build (`npm run build`), commitar `ui/dist`. → commit `feat(ui): external reference marker, form and source backlinks`.

Verificar: `go test ./...` (embute dist) verde; sem dep nova.

## Fechamento
- `result.md`; status spec → Done; linha 0020 no índice; `update-cerne.ps1`; smoke real opcional (`cerne reference new` na assinatura/hub).
