# Resultado — Case 0013 (`cerne ui` abre o browser)

**Status:** Done (2026-06-13). Suíte: 383 testes verdes, `go vet` limpo.

## O que foi entregue

`cerne ui` agora abre `http://localhost:<port>` no browser padrão por default; `--no-browser` desliga.

- **`internal/cli/browser.go`**: `browserCommand(goos, url)` (puro) — windows `rundll32 url.dll,FileProtocolHandler`, darwin `open`, demais `xdg-open`; `openBrowser(url)` best-effort (`exec.Command(...).Start()`, não bloqueia; falha → aviso em stderr, servidor segue).
- **`internal/cli/uicmd.go`**: flag `--no-browser`; `http.ListenAndServe` → `net.Listen` + `http.Serve` (bind antes de imprimir/abrir, sem corrida); `go openBrowser(url)` quando `!noBrowser`.

## Verificação

- **Unit:** `TestBrowserCommand` (windows/darwin/linux/freebsd) — argv correto por SO.
- **Manual (smoke):** num projeto temp, `cerne ui --no-browser --port 7723` → `GET /` = HTTP 200, URL impressa, **nenhum browser novo aberto** (refactor Listen+Serve sem regressão). O caminho default usa o mesmo `Serve` + `go openBrowser` (testado) → no Windows abre via `rundll32`. Não rodei o default-open no ambiente de dev para não popar aba; coberto por unit test + revisão.

## Decisão (ver spec)

Abre por default (pedido do usuário), opt-out `--no-browser` para headless/CI/SSH. `rundll32`/`open`/`xdg-open` sem shell, sem dependência nova. Best-effort: nunca derruba o servidor.

## Inventário

- **Código:** `internal/cli/browser.go` (novo), `internal/cli/uicmd.go` (flag + Listen/Serve + open).
- **Testes:** `internal/cli/browser_test.go` (+1 test, 4 sub-casos).
- **Commits:** `feat(cli): cross-platform browser-open command builder`, `feat(cli): open the browser on 'cerne ui' (default; --no-browser to skip)`, + docs.

## Fora de escopo

- Escolher browser específico; deep-link para uma feature; abrir aba vs janela.
