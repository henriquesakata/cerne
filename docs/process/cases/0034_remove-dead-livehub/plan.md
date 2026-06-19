# Case 0034 — Plano de implementação

Remoção de dead-code comportamento-preservante. Conventional Commits; corpo termina com `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`. A suíte existente é a rede de segurança: o que sobra deve ficar verde sem edição além do teste reescrito.

---

## 1. spec + plan (registro)
- Espelho de `spec.md`/`plan.md` + linha no índice `docs/process/CLAUDE.md`.
- `docs(cerne): case 0034 — drop dead single-project live path (spec + plan)`

## 2. Remover o dead-code (um commit coeso)
A reescrita do teste e a remoção são **uma concern só** — "remover o caminho live morto preservando a cobertura de `pullLoop`": não dá para remover os helpers dos 5 testes sem remover os testes, e a cobertura de `pullLoop` é re-ancorada no mesmo movimento.

- **live_test.go:** `TestPullLoopAdvancesHeadFromRemote` reescrito contra `newMultiHub`/`watchHeadMulti(ctx, hub, "proj-a", repoDir, …)` — o assinante recebe `liveEvent{kind:"change", pid, sha}`. Removidos os 5 testes do caminho morto + helpers órfãos presos ao `*liveHub` (`subCount`, `openStream`) e o `waitFor` (só o teste removido o usava). Mantidos `newGitBackendNoClean`/`readSSEData`/`waitEvent` (usados pelo `uimulti_test.go`); imports `net/http`/`net/http/httptest` saem.
- **live.go:** apaga `liveHub` (tipo), `newLiveHub`, `subscribe`, `unsubscribe`, `broadcast`, `watchHead`, `handleStream`; imports `fmt`/`net/http`/`sync` saem. Sobram `headPollInterval`, `keepaliveInterval`, `pullLoop`.
- **server.go:** apaga `NewLive`; `newMux` perde `hub *liveHub` + o ramo `if hub != nil { stream }`; `New` → `newMux(repoDir, projDir, projectID)`; imports `context`/`time` saem; docstrings de `New`/`newMux` atualizadas.
- **uimulti.go:** docstring de `watchHeadMulti` perde o "Mirrors `liveHub.watchHead`".
- **Verificação:** `go vet ./...` + `go test ./... -count=1` + `go build ./cmd/cerne` verdes.
- `refactor(server): remove the dead single-project liveHub and NewLive`

## 3. Fechamento
- `result.md` + índice → Implemented. Registrar no cerne (`import`+`pull`, commit `docs.lock`). Entrega: merge do `main` recente, push + MR (`-R rockt/cerne`).

## Notas / riscos
- **Helpers compartilhados:** `newGitBackendNoClean`/`readSSEData`/`waitEvent` ficam em `live_test.go` (o `uimulti_test.go` os usa); só `subCount`/`openStream`/`waitFor` (presos ao `*liveHub`, ou exclusivos do teste removido) saem.
- **`pullLoop`+constantes seguem vivos:** o ramo morto é só o `liveHub`; o `uimulti` usa `pullLoop`, `headPollInterval`, `keepaliveInterval` — `live.go` não some, só encolhe.
- **Sem `NewLive` em `uicmd.go`:** confirmado — `cerne ui` usa `NewUIMulti` desde o 0032; `grep` por `NewLive(` só acha a definição.
- **Base recente:** o caso 0033 (tabela de rotas única) já tinha pousado no `main`; o trabalho parte do `newMux` pós-refactor (`mountDataRoutes`), então o diff é só a remoção do `hub`/stream, não a tabela.
