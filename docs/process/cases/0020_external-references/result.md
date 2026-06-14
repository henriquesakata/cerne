# Resultado — Case 0020 (referências externas)

**Status:** Done (2026-06-14). Suíte: **450 testes** verdes, `go vet` limpo, build da UI ok (sem dep nova).

## O que foi entregue

Cadastrar documentação/artigos **externos** como referência — fonte de conhecimento da IA, com marca "externa" e backlink bidirecional para a correção/decisão que a fonte motivou.

### Frente A — modelo + app + CLI
- **`store.Reference`** ganhou `URL`, `External bool`, `Informs []string` (omitempty).
- **`app.CreateExternalReference`** (`internal/app/reference.go`): valida título/url e que cada `informs` (`F-`/`D-`) **existe**; cunha `R-NNNN`, `External:true`; grava o conteúdo colado em `content/references/<ID>.md` (com `cerne_id`) — indexado, então o `ask` cita; evento `reference.created`; commit+push+lock. **`ReferencesInforming(id)`** = lookup reverso.
- **CLI** `cerne reference new "<título>" --url <url> [--informs F-..,D-..] [--file|stdin]` (comando-pai `reference` novo). Commit `fdfa293`.

### Frente B — server
- **`POST /api/references`** (422 em informs pendurado). Detalhe de **feature** e **decisão** trazem `informed_by` (reverso, via `ReferencesInforming`). O detalhe de referência já trazia url/external/informs (struct embutida). `ask.Retrieve` marca hits de ref externa com a URL e `BuildAskPrompt` a expõe ("referência externa: <url>"); `/api/ask` devolve a URL por fonte. Commit `40aab14`.

### Frente C — UI
- `api.ts`: `createReference` + tipos (`url/external/informs`, `informed_by`).
- **Referências:** badge **"externa"** na lista; detalhe com **Fonte: <url>** + **Informou:** (links); formulário **"Nova referência externa"**.
- **Feature/Decisão (detalhe):** seção **Fontes** listando `informed_by` (com badge externa). Commit `9a29f97`.

## Decisões (confirmadas)
| Fork | Decisão |
|---|---|
| Entidade | estender `Reference` (não tipo novo). |
| Marca | campo explícito `External` (+ `URL`). |
| Conteúdo | texto colado vira content-doc indexado → `ask` cita sem código de indexação novo. |
| Vínculo | backlink estruturado `Informs` (F/D), bidirecional; reverso computado (uma fonte da verdade). |
| Criação | CLI **e** UI sobre `app.CreateExternalReference`. |

## Verificação
| Item | Como | Resultado |
|---|---|---|
| app create/validação/reverse | `app/reference_test.go` | cria (entity+content+evento+commit); url vazia/informs pendurado → erro sem escrita; `ReferencesInforming` ok |
| CLI | `cli/referencecmd_test.go` | `reference new` (conteúdo por arquivo, `--informs`); pendurado → erro; `--url` obrigatória |
| server | `server/references_test.go` | `POST` cria (422 pendurado); feature/decisão detail trazem `informed_by` |
| ask cita URL | `ask` unit | `BuildAskPrompt` expõe "referência externa: <url>" |
| UI | `npm run build` | `tsc` limpo; sem dep nova |
| Go embute dist | `go test ./...` | 450 testes |

## Resposta ao pedido do usuário
"Cadastrar docs/artigos externos como fonte, com marca de externa" + "o agente registrar o artigo que resolveu o problema, como histórico de decisões/fonte da correção": o agente roda `cerne reference new --url … --informs <F/D>` (ou o humano usa o formulário); a fonte fica marcada **externa**, seu conteúdo entra no `ask`, e o vínculo aparece dos dois lados — na ref ("Informou: F-0042") e na feature/decisão ("Fontes: R-0042 externa").

## Inventário
- **Go:** `store/entity.go` (campos), `app/reference.go` (+test), `cli/referencecmd.go` (+test) + `root.go`, `server/server.go` (+`references_test.go`), `ask/ask.go` (+test).
- **UI:** `api.ts`, `References.tsx`, `FeaturePage.tsx`, `Decisions.tsx`, `app.css`, `ui/dist`.
- **Commits:** `4ff37e1`, `fdfa293`, `40aab14`, `9a29f97` (+ este result).

## Fora de escopo / follow-ups
- Buscar/baixar a URL automaticamente (offline; conteúdo é colado).
- Editar `informs`/conteúdo de uma ref após criada.
- Campo de autor/data de publicação.
- Refs externas na **página de wiki** de references já fluem pelo conteúdo; um destaque visual de "externas" na wiki fica para depois.
