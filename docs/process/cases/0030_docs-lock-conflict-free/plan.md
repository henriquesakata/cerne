# Case 0030 — Plano de implementação

Commits TDD (teste vermelho → impl mínima → verde → commit), uma preocupação por commit. Suíte verde a cada commit (`go vet ./... && go test ./... -count=1`). Strings de usuário pt-BR; código/identificadores em inglês. Conventional Commits; corpo termina com `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`.

Duas frentes: **(A)** tirar o manifest do `docs.lock` commitado; **(B)** merge driver para o `snapshot`. Ordem importa: B opera sobre o arquivo já enxuto.

---

## 1. plan (registro)
- Este `plan.md` via `.docs/current/` + `cerne push`; espelho limpo na pasta de caso + linha no índice `docs/process/CLAUDE.md`.
- `docs(cerne): case 0030 — docs.lock conflict-free (plan)`

## 2. project — sidecar `.docs/.manifest.json` (aditivo)
- **Teste:** `TestManifestRoundTrip` (`SaveManifest`/`LoadManifest` em `<projDir>/.docs/.manifest.json`); `TestLoadManifestFallsBackToLock` (lock antigo com `materialized` + sidecar ausente → devolve o mapa do lock); sidecar presente vence o lock.
- **Impl:** `internal/project/manifest.go` — `SaveManifest(projDir, map[string]string)` / `LoadManifest(projDir) (map, error)`, JSON pretty + escrita atômica (reusa `writeFileAtomic`); fallback lê `materialized` do `docs.lock`. `Lock.Materialized` mantido por ora (passo aditivo, suíte verde).
- `feat(project): add .docs/.manifest.json sidecar (SaveManifest/LoadManifest)`

## 3. cli — migrar pull/push/init ao sidecar; remover `materialized` do lock
- **Teste:** `TestSaveLockOmitsMaterialized` (JSON sem a chave); `TestPullWritesManifestSidecar` (sidecar existe; `docs.lock` sem `materialized`); ajustar `pullcmd_test`/`pushcmd_test`/`initcmd_test` que hoje asseveram `lock.Materialized` → asseveram o sidecar.
- **Impl:** `runPull` grava `SaveManifest` após `Materialize` (no `docs.lock` só `Snapshot`); `runPush` lê `LoadManifest`, atualiza e regrava o sidecar (drift-detection contra o `Snapshot` remoto **inalterada**); `runInit` deixa de gravar `materialized`. Remover o campo `Materialized` de `project.Lock` (+ `SaveLock`/`LoadLock`).
- `refactor(project): drop docs.lock materialized; persist the manifest in the .docs/ sidecar`

## 4. gitback — ancestralidade
- **Teste:** `TestIsAncestorLinear` (repo linear A→B: `IsAncestor(A,B)`=true, `(B,A)`=false, self=true); `TestFetchBringsObjectsWithoutMovingHead`.
- **Impl:** `internal/gitback/ancestry.go` — `IsAncestor(repoDir, a, b) (bool, error)` (go-git, `Commit.IsAncestor`); `Fetch(repoDir)` para garantir os dois SHAs locais antes da checagem (um lado pode ter vindo de outro branch).
- `feat(gitback): add IsAncestor (+ Fetch) for docs.lock conflict resolution`

## 5. cli — `cerne lock merge` (driver)
- **Teste:** `TestLockMergeChoosesDescendant` (cache A→B; ours=lock@A, theirs=lock@B; resolve para B; e simétrico); `TestLockMergeBackendMismatch` → ≠0; `TestLockMergeDivergent` (irmãos, nenhum ancestral) → ≠0 + lock intacto.
- **Impl:** `internal/cli/lockcmd.go` — grupo `lock` + sub oculto `merge <base> <ours> <theirs>` (contrato `%O %A %B`; resultado em `<ours>`/`%A`). Resolve o projeto pelo `cwd` → cache repoDir; `Fetch`; descendente via `IsAncestor`; `version`/`backend` divergentes ou histórico sem relação → erro pt-BR. Registro em `root.go`.
- `feat(cli): add 'cerne lock merge' resolving snapshot to the descendant commit`

## 6. cli — registrar o driver no init e no pull
- **Teste:** `TestInitRegistersMergeDriver`; `TestPullSelfHealsMergeDriver`; `TestPullSelfHealsMergeDriverInWorktree` (regressão da worktree).
- **Impl:** `ensureLockMergeDriver(projDir)` — `.gitattributes` (`docs.lock merge=cerne-lock`) + `merge.cerne-lock.{name,driver}` no config via go-git (`PlainOpenWithOptions{EnableDotGitCommonDir: true}` para resolver o config compartilhado de worktrees). Chamado por `runInit` (**não-fatal**) e `runPull` (self-heal). No-op gracioso fora de repo git.
- `feat(cli): register the cerne-lock merge driver on init and pull` (+ `fix(cli): … EnableDotGitCommonDir`)

## 7. Fechamento
- `go vet ./...` + `go test ./... -count=1` (506) + `go build` verdes; **e2e real** (`git merge` auto-resolve).
- `result.md` (via `.docs/current/` + push) + `cerne feature complete F-0030`; índice → Implemented.
- **Entrega:** push + MR no `rockt/cerne` (`-R rockt/cerne` no glab).
