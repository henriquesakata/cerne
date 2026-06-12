# cerne ui

Front-end mínimo (Vite + React + TypeScript) servido pelo próprio binário do
cerne via `go:embed` (`ui/embed.go` embute `ui/dist/`).

## Rebuild

`ui/dist/` é **commitado** de propósito: assim `go build ./...` gera o binário
completo a partir de um clone limpo, sem toolchain Node. Sempre que alterar
qualquer arquivo em `ui/src/` (ou `index.html`), refaça o build e commite o
`dist/` junto:

```bash
cd ui
npm install
npm run build
```

## Dev server

Com `cerne ui` rodando (porta 7717), o dev server do Vite faz proxy de `/api`:

```bash
cd ui
npm run dev
```
