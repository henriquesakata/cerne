# Case 0001 — Resultado

tags: #result #cerne #v1

**Status da spec:** Done (v0.2.0, 2026-06-12)
**Tags de release:** `v0.1.0` (CLI core) · `v0.2.0` (UI local completa)
**Suíte:** 227 testes Go verdes em 16 pacotes · `go vet` limpo · CI GitLab (vet + test)

---

## O que foi entregue

Todo o corte v1 da spec, nos 7 milestones do plan.md:

| Milestone | Entrega | Commits-chave |
|---|---|---|
| M0 | Módulo Go + cobra + CI GitLab | `c4ed905`, `acb94ff` |
| M1 | Domínio: FSM de Feature (6 estados), gates por documento, eventos NDJSON, lifecycles de Decision/Reference | `e155bfa`…`4882f24` |
| M2 | Storage: entidades JSON atômicas, layout do backend, log de eventos mensal, `NextID`, operações git puras (go-git, branch `main` determinístico, empty-remote) | `4e5c705`…`2e08445` |
| M3 | CLI núcleo: `.cernerc` + `docs.lock`, `init` (com join de backend existente), `feature new` + 5 transições com gates, `status` (`--all`/`--json`) | `8f3ec24`…`b9512e0` |
| M4 | `pull` (materialização do vigente + `CONTEXT.md` determinístico + manifest sha256) e `push` (write-back via `cerne_id`, detecção de drift); **e2e golden path** | `07db5a4`, `f61bf6f`, `e103c82` → **v0.1.0** |
| M5 | `import` (convenções hub: cases→Features, ADRs→Decisions, runbooks/arquitetura/planning→References; heurísticas de estado; dry-run default) e `lint` (4 regras determinísticas + `--fix-tasks`) | `78abc33`, `ec8e552` |
| M6 | `search` (SQLite FTS5 puro-Go, histórico completo), UI local embarcada (`cerne ui`): kanban, página da feature com tabs markdown + transições humanas, **Excalidraw** lazy-loaded com criação/edição/salvamento de diagramas | `f4a417e`…`c597774` → **v0.2.0** |

## Aceitação no mundo real (critérios da spec)

Rodada contra o **Rockt Hub real** (~1.029 arquivos md):

1. **`cerne import` do hub** ✅ — 308 features + 45 ADRs + 66 referências em 37s (dry-run em 4,4s); estados: 285 implemented, 15 candidate, 6 in_development, 2 superseded.
2. **Redução de contexto** ✅ — `.docs/` materializada com **138 arquivos vs 1.029 (-87%)**; só 21 features ativas têm conteúdo materializado.
3. **`cerne status` em 1 comando** ✅ — 6 em desenvolvimento / 15 candidatas / 285 implementadas, instantâneo.
4. **Golden path e2e** ✅ — `init → feature new → push spec → approve → pull` coberto por teste automatizado, sem LLM e sem infra além de um git bare.
5. **Rastreabilidade** ✅ — `docs.lock.snapshot` == HEAD do backend verificado em todos os fluxos.
6. **Lint determinístico** ✅ com nuance — **204 problemas em 0,5s**: 18 ref-to-superseded (ADRs ativas citando case 0090 e ADR-045 substituídos pelos identificadores originais), 25 links quebrados, 161 cases "implemented" sem result.md real. **Nuance Bills/Investments:** a absorção precede a convenção de cases do hub — não existe case "Bills" para virar entidade substituída, então não há needle para flagrar as menções em `features.md`/`R-0010`. O *mecanismo* está provado (18 achados reais); o caso específico exige registro histórico de entidades (ver Pendências).

## Decisões tomadas durante a implementação

- **Go 1.26** (vs Rust) — registrado no plan.md; confirmou-se acertado: go-git + modernc/sqlite + go:embed cobriram tudo sem cgo; binário único de 33 MB com UI.
- **Estados em inglês nos dados**, labels pt-BR só na UI.
- **SQLite como cache derivado** (`<cache>/index.db`), reconstruível; backend git é a única fonte da verdade.
- **`CommitAndSnapshot` compartilhado** (`internal/app`) — transições, push e artifacts usam o mesmo caminho commit+push+lock.
- **`ui/dist` commitado** (7,9 MB) — garante `go build` = binário completo a partir de clone limpo; rever quando houver pipeline de release.
- Ajustes guiados pela aceitação real: `docs/planning` importado como References; needles do lint derivados de `imported_from` (identificadores originais); transliteração de diacríticos em slugs; BOM/CRLF tolerados na detecção de stubs.

## Desvios da spec

- `cerne reject` também grava `Reason` na entidade (spec só exigia em supersede) — informação valiosa demais para ficar só no evento.
- Validação de `--module`: exigido/validado apenas quando o projeto declara 2+ módulos.
- Heurística de import para ADRs: 45 de 46 ADRs do hub reconhecidas (1 fora do padrão de nome `adr-NNN-*`); aceitável, documentado.

## Pendências e dívidas (para os próximos cases)

1. **Registro histórico de entidades** — falta um caminho para declarar "X existiu e foi absorvido por Y" quando X precede a documentação (caso Bills/Investments). Ideias: `cerne feature new --superseded-by ... --historical` ou seed de aliases no import. É o que falta para fechar o critério 1 por completo.
2. **Lint não lê wiki-links** (`[[nota]]`, padrão Obsidian usado no hub) — só links inline `[x](y)`; cobertura parcial de broken-link em vaults Obsidian.
3. **`ui/dist` no git** — substituir por build de UI no CI quando houver pipeline de release multiplataforma.
4. **Push não cria documentos novos** — docs nascem via `feature new`/backend; criar doc direto em `.docs/` não é suportado (erro orientativo existe).
5. **Mensagens 422 da API** reaproveitam textos com `--by`/`--reason` (sabor CLI) — revisar quando a UI de transição evoluir.
6. v1.5 da spec (LLM plugável): `cerne compile` (wiki) e `cerne ask` — não iniciados, por design do corte.

## Inventário (resumo)

- `cmd/cerne/` — entrypoint. `internal/`: `domain` (FSMs, gates, eventos), `store` (entidades, layout, eventos, IDs), `gitback` (go-git), `project` (config, lock, paths), `app` (núcleo de transição/artifact), `materialize`, `importer` (+testdata hubtree), `lint`, `index` (FTS5), `server` (API + static), `cli` (12 comandos), `doccheck`, `slug`. `ui/` — Vite + React 19 + TS: kanban, feature page, DiagramPanel (Excalidraw lazy), api.ts tipado; `dist/` commitado. `e2e/` — golden path. `.gitlab-ci.yml`, `.gitattributes`.
- Dependências diretas Go: cobra, go-git/v5, BurntSushi/toml, modernc.org/sqlite. UI: react, react-dom, react-markdown, @excalidraw/excalidraw.

## Próximos passos sugeridos

1. Dogfooding imediato: `cerne import` do próprio repo cerne + do hub num backend real (GitLab `rockt/hub-docs`), e passar a gerir os casos do cerne pelo cerne.
2. Case 0002: registro histórico de entidades (pendência 1) — fecha o critério Bills por completo.
3. Case 0003: `cerne compile`/`ask` (v1.5, LLM plugável).
4. Release pipeline (goreleaser ou CI GitLab): binários win/mac/linux + UI buildada no CI.
