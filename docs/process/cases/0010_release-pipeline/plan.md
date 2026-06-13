# Plano — Case 0010 (release pipeline)

**Goal:** Da tag git aos binários win/mac/linux com versão estampada, reproduzível e publicável via GitLab CI.

## Tarefas

1. **Version default → `dev`** (`internal/cli/root.go` ou novo `version.go`): manter a var de pacote lida por cobra; default `"dev"`. Confirmar que `go build -ldflags "-X gitlab.com/rockt/cerne/internal/cli.version=vX"` sobrescreve. Verificação: build com e sem ldflags, checar `cerne --version`. → commit `feat(build): stamp version via ldflags`.
2. **`scripts/build-release.sh`**: matriz linux/{amd64,arm64} darwin/{amd64,arm64} windows/amd64; `CGO_ENABLED=0`; `-trimpath -ldflags "-s -w -X ...version=$V"`; saída `dist/cerne_${V}_${OS}_${ARCH}[.exe]`; `SHA256SUMS`. Limpa `dist/` no início. **Rodar localmente** com uma versão de teste e conferir os 5 binários (tipos ELF/Mach-O/PE via `file`). → commit `feat(build): cross-compile release matrix script`.
3. **`.gitlab-ci.yml`**: adicionar stage `release` (on tag): `build:release` (roda o script, upload ao Package Registry genérico, artifacts) + `release` (release-cli criando a Release). Manter o stage `test` intacto. → commit `ci(release): publish cross-compiled binaries on tags`.

## Verificação final

- `go vet ./...` + `go test ./... -count=1` verdes.
- `scripts/build-release.sh 0.0.0-test` produz os 5 binários + SHA256SUMS (conferido com `file`); `dist/` é gitignored.
- `cerne --version` reporta a versão estampada.
- A YAML do CI não roda neste ambiente — documentar no result.md.
