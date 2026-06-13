# Case 0013 — `cerne ui` abre o browser

tags: #spec #cerne #cli #ui #ux

**Status:** Done — implementado e verificado (2026-06-13); ver [result.md](result.md)
**Tipo:** UX do comando `ui` (CLI).
**Origem:** Pedido de uso real: ao rodar `cerne ui`, abrir a página no browser padrão automaticamente, em vez de só imprimir a URL e o usuário ter que clicar/colar.

---

## Problema

`cerne ui` sobe o servidor e imprime `UI local em http://localhost:7717 (Ctrl+C para sair)`, mas não abre nada. O usuário precisa copiar a URL para o browser — atrito desnecessário, especialmente para o público amador (uma das metas do produto: "amador não vive no terminal").

---

## Objetivo e critérios de sucesso

`cerne ui` abre a URL no browser padrão **por default**, mantendo a URL impressa (fallback) e permitindo desligar.

**Critério de aceite:**
1. `cerne ui` abre `http://localhost:<port>` no browser padrão do SO e serve normalmente.
2. `cerne ui --no-browser` **não** abre o browser (apenas imprime a URL e serve) — para headless/CI/SSH.
3. A abertura é **best-effort**: se o comando do SO falhar, o servidor segue no ar (a URL impressa continua válida); aviso vai para stderr.
4. Cross-platform: Windows / macOS / Linux.
5. A URL só é aberta **após** o socket estar ouvindo (sem corrida).
6. Suíte + `go vet` verdes; comando `ui` segue servindo (refactor sem regressão).

---

## Decisões do design

| Fork | Decisão | Razão |
|---|---|---|
| **Opt-in vs opt-out** | **Abre por default**, opt-out via `--no-browser` | É o que o usuário pediu ("default"); a flag cobre headless/CI. |
| **Mecanismo de abertura** | Por `runtime.GOOS`: windows `rundll32 url.dll,FileProtocolHandler`, darwin `open`, demais `xdg-open` | Padrão de mercado; sem shell (evita quoting); sem dependência nova. |
| **Ordem** | `net.Listen` → imprime URL → goroutine abre o browser → `http.Serve` | Garante o socket ouvindo antes da abertura; a URL só é impressa quando o bind deu certo. |
| **Falha ao abrir** | Best-effort: aviso em stderr, segue servindo | Headless/sem browser não deve derrubar o servidor. |
| **Testabilidade** | Extrair `browserCommand(goos, url) (name, args)` puro | Unit-testável por SO; o `exec` real (que abriria um browser) não é testado. |

---

## Desenho

### `internal/cli/uicmd.go`

- Flag `--no-browser` (bool, default false).
- `runUI`: `ln, err := net.Listen("tcp", "localhost:<port>")`; imprime a URL; se `!noBrowser`, `go openBrowser(url)`; `http.Serve(ln, handler)`.
- `openBrowser(url)`: monta o comando via `browserCommand(runtime.GOOS, url)`, `exec.Command(...).Start()` (não `Run` — não espera o browser); falha → aviso em stderr.
- `browserCommand(goos, url) (string, []string)` — função pura:
  - `windows` → `("rundll32", ["url.dll,FileProtocolHandler", url])`
  - `darwin` → `("open", [url])`
  - default → `("xdg-open", [url])`

---

## Verificação

- **Unit:** `browserCommand` por SO (windows/darwin/linux).
- **Manual:** `cerne ui --no-browser` sobe e serve (`GET /` 200) sem abrir browser; `cerne ui` (default) abre o browser ao rodar.

## Fora de escopo

- Escolher browser específico; abrir aba vs janela; deep-link para uma feature.
