# Case 0010 — Release pipeline

tags: #spec #cerne #build #ci #release

**Status:** Done — script de cross-compile verificado localmente; CI escrita (não roda neste ambiente) (2026-06-13); ver [result.md](result.md)
**Tipo:** Build/distribuição — version stamping, cross-compile da matriz, GitLab CI release on tags.
**Origem:** Pendência nº 3 do [result.md do case 0001](../0001_founding-spec/result.md) e "Next steps" do CLAUDE.md: binários win/mac/linux + versão via ldflags (hoje `0.0.1-dev` hardcoded). Torna o cerne instalável por terceiros (meta open source).

---

## Problema

1. **Versão hardcoded:** `internal/cli/root.go` tem `var version = "0.0.1-dev"`; `cerne --version` sempre mente, independente da tag. Não há como saber qual build se está rodando.
2. **Sem distribuição:** não existem binários publicados. Quem quer usar o cerne tem que clonar e `go build` — barreira alta para o público amador (uma das metas do produto).
3. **Sem reprodutibilidade de release:** nenhum processo que, a partir de uma tag, produza os binários de todas as plataformas de forma determinística.

---

## Objetivo e critérios de sucesso

A partir de uma tag git (`vX.Y.Z`), produzir binários para win/mac/linux com a versão estampada, de forma reproduzível e publicável.

**Critério de aceite:**
1. `go build -ldflags "-X .../internal/cli.version=$V"` faz `cerne --version` reportar `$V`; sem ldflags, default `dev`. **(verificável localmente)**
2. `scripts/build-release.sh <versão>` cross-compila a matriz (linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64) com a versão estampada, em `dist/`, mais `SHA256SUMS`. **(verificável localmente — rodado neste case)**
3. `.gitlab-ci.yml` ganha um stage `release` disparado **só em tag** que roda o script com `$CI_COMMIT_TAG`, publica os binários no Package Registry genérico e cria a GitLab Release linkando os assets. **(escrita conforme convenção GitLab; não executável neste ambiente — honestidade de verificação)**
4. Stack pure-Go (modernc/sqlite, go-git, go:embed) ⇒ `CGO_ENABLED=0` cross-compila sem toolchain C. Confirmado pela matriz buildando.
5. Suíte e `go vet` seguem verdes; `ui/dist` permanece commitado (embed) — build de UI no CI fica como follow-up.

---

## Decisões do design (2026-06-13)

| Fork | Decisão | Razão |
|---|---|---|
| **Ferramenta de release** | **Script bash + GitLab CI nativo** (não goreleaser) | Zero dependência nova de toolchain; a matriz é pequena; `go build` cross-compila puro. goreleaser seria peso extra para o que um loop de 5 linhas faz. |
| **Version stamping** | **ldflags `-X internal/cli.version`** (var já existe) | Sem mudança de código além de documentar o caminho; cobra já lê essa var em `--version`. Default `dev`. |
| **Matriz** | linux/{amd64,arm64}, darwin/{amd64,arm64}, windows/amd64 | Cobertura das plataformas reais; arm64 (Apple Silicon, ARM Linux) incluso. `CGO_ENABLED=0`. |
| **Publicação** | **Package Registry genérico** (upload via curl) + **GitLab Release** (`release-cli`) | Padrão GitLab nativo; sem infra externa. |
| **`ui/dist`** | **Continua commitado**; build de UI no CI = follow-up | Mantém `go build` reprodutível a partir de clone limpo; mover para CI exige Node + verificação que não dá pra fazer aqui. |
| **Checksums** | `SHA256SUMS` no `dist/` | Higiene de release; verificável. |

---

## Desenho

### Version (ldflags)

`internal/cli/version.go` (ou mantém em root.go): `var version = "dev"`. Build de release:

```
go build -trimpath -ldflags "-s -w -X gitlab.com/rockt/cerne/internal/cli.version=$VERSION" -o <out> ./cmd/cerne
```

`-trimpath` (paths reproduzíveis), `-s -w` (binário menor). `cerne --version` → `cerne version $VERSION`.

### `scripts/build-release.sh`

```
scripts/build-release.sh [versão]   # versão default: git describe --tags --always
```
Loop sobre a matriz; `CGO_ENABLED=0 GOOS=.. GOARCH=.. go build -trimpath -ldflags "..." -o dist/cerne_${V}_${OS}_${ARCH}[.exe] ./cmd/cerne`; ao fim, `sha256sum dist/* > dist/SHA256SUMS`. Idempotente (limpa `dist/` no início).

### `.gitlab-ci.yml` — stage `release`

- `stages: [test, release]`.
- `build:release`: `rules: - if: '$CI_COMMIT_TAG'`; roda `scripts/build-release.sh "$CI_COMMIT_TAG"`; faz upload de cada binário + `SHA256SUMS` para o Package Registry genérico (`${CI_API_V4_URL}/projects/${CI_PROJECT_ID}/packages/generic/cerne/${CI_COMMIT_TAG}/...`); expõe `artifacts`.
- `release`: `needs: [build:release]`, imagem `registry.gitlab.com/gitlab-org/release-cli`, cria a Release apontando os assets do Package Registry.

---

## Verificação

- **Local (feito):** build com ldflags → `--version` reporta a versão; `scripts/build-release.sh 0.0.0-test` cross-compila os 5 alvos + SHA256SUMS; conferir tamanhos/headers dos binários (ELF/Mach-O/PE).
- **CI (não executável aqui):** a YAML segue a convenção GitLab (package registry + release-cli) — será validada na primeira tag real.

---

## Fora de escopo

- **Build de UI no CI** (un-commit do `ui/dist`) — follow-up; precisa de Node no pipeline + verificação.
- Homebrew/scoop/winget, assinatura de binários, notarização macOS.
- goreleaser.
