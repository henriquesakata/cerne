# Plano — Case 0013 (`cerne ui` abre o browser)

**Goal:** `cerne ui` abre a URL no browser padrão por default; `--no-browser` desliga; best-effort e cross-platform.

## Tarefas (TDD)

1. **`browserCommand` (puro) + teste** (`internal/cli/browser.go`, `browser_test.go`): `browserCommand(goos, url string) (string, []string)` — windows→rundll32, darwin→open, default→xdg-open. Teste table-driven (windows/darwin/linux) primeiro (red), implementa (green). → commit `feat(cli): cross-platform browser-open command builder`.
2. **Wire no `ui`** (`internal/cli/uicmd.go`): flag `--no-browser`; trocar `http.ListenAndServe` por `net.Listen` + `http.Serve`; após o bind, imprime URL e (se `!noBrowser`) `go openBrowser(url)` (best-effort, aviso em stderr via `exec.Command(...).Start()`). → commit `feat(cli): open the browser on 'cerne ui' (default; --no-browser to skip)`.

## Verificação

- `go vet ./...` + `go test ./... -count=1` verdes.
- Manual: build; `cerne ui --no-browser` num projeto inicializado serve (`GET /` 200) sem abrir browser; `cerne ui` abre o browser. (O bind antes do Serve evita corrida.)
