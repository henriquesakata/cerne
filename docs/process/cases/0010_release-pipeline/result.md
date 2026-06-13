# Resultado — Case 0010 (release pipeline)

**Status:** Done (2026-06-13). Suíte: 357 testes verdes, `go vet` limpo. `.gitlab-ci.yml` validado pela API do GitLab (`glab ci lint`).

## O que foi entregue

Da tag git aos binários multiplataforma com versão estampada, reproduzível e publicável.

- **Version stamping** (`internal/cli/root.go`): `version` agora default `"dev"`, sobrescrito no release via `-ldflags "-X gitlab.com/rockt/cerne/internal/cli.version=<tag>"`. `cerne --version` reporta a versão real do build. Commit `feat(build): stamp version via ldflags`.
- **`scripts/build-release.sh`**: cross-compila `linux/{amd64,arm64}`, `darwin/{amd64,arm64}`, `windows/amd64` com `CGO_ENABLED=0` (stack pure-Go) + `-trimpath -ldflags "-s -w -X ...version=$V"`; gera `dist/cerne_<v>_<os>_<arch>[.exe]` + `SHA256SUMS` (sha256sum/shasum). Limpa `dist/` no início. Commit `feat(build): cross-compile release matrix script`.
- **`.gitlab-ci.yml`**: stages `[test, release]`. `build:release` (on tag) roda o script com `$CI_COMMIT_TAG`, faz upload de cada binário + `SHA256SUMS` ao Package Registry genérico (`packages/generic/cerne/<tag>/...`); `release` (release-cli) cria a GitLab Release linkando os 6 assets. `test` segue gating todo release. Commit `ci(release): publish cross-compiled binaries on tags`.

## Decisão (ver spec)

Script bash + CI nativa do GitLab em vez de goreleaser — zero toolchain nova; a matriz é um loop de 5 alvos e a stack pure-Go cross-compila sem C. `ui/dist` continua commitado (embed); build de UI no CI fica como follow-up.

## Verificação

| Item | Como | Resultado |
|---|---|---|
| ldflags | `go run`/build com e sem `-X` | sem → `cerne version dev`; com → a tag |
| Matriz cross-compile | `scripts/build-release.sh 0.0.0-test` (rodado) | 5 binários: ELF x86-64/aarch64, Mach-O x86_64/arm64, PE32+ — tipos conferidos com `file` |
| Stamp no binário real | `dist/cerne_0.0.0-test_windows_amd64.exe --version` (host nativo) | `cerne version 0.0.0-test` |
| Checksums | `SHA256SUMS` | 5 hashes gerados |
| YAML do CI | `glab ci lint` (API do GitLab) | **"CI/CD YAML is valid!"** |
| **CI on real tag** | — | **Não executável neste ambiente** (precisa de runner + tag); YAML validada pela API, script verificado localmente. Será exercido na 1ª tag real. |

## Inventário

- **Código:** `internal/cli/root.go` (version default `dev` + doc do ldflags).
- **Build/CI:** `scripts/build-release.sh` (novo), `.gitlab-ci.yml` (stage release).
- **Sem novos testes Go** (build/CI não são unit-testáveis); verificação por execução do script + lint da API.

## Follow-ups

- **Build de UI no CI** (un-commit do `ui/dist`): precisa de Node no pipeline + verificação; mantém `go build` reprodutível por ora.
- Distribuição via Homebrew/scoop/winget; assinatura/notarização de binários.
- Primeira tag real (`vX.Y.Z`) para exercer o pipeline ponta-a-ponta (upload + Release).
