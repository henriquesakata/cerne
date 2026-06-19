# Case 0030 — `docs.lock` à prova de conflito (manifest local + merge driver)

tags: #spec #cerne #docs-lock #git #dx #conflitos

**Status:** Rascunho (2026-06-18) — spec para revisão; `plan.md`/`result.md` a seguir.
**Tipo:** Corretivo (estrutural). Reduz a superfície de merge do `docs.lock` commitado a um único campo e ensina o git a resolver esse campo sozinho — sem mudar o contrato de reprodutibilidade (pin por-commit exato continua).
**Origem:** Dor recorrente de quem usa o cerne em projetos-cliente: **conflitos frequentes no `docs.lock`** quando dois branches/worktrees (incl. agentes paralelos) rodam comandos cerne. Confirmado no código: o arquivo concentra dois "ímãs de conflito". Relaciona-se à nota de operação "colisões de caso em worktrees paralelas" (mesma classe de problema: estado compartilhado que diverge por branch).

---

## Problema

O [`docs.lock`](../../../../internal/project/lock.go) commitado tem 4 campos e dois deles tornam o merge impossível para o git resolver:

| Campo | Muda quando | Mergeável pelo git? |
|---|---|---|
| `version`, `backend` | nunca | — |
| `snapshot` (SHA do HEAD do backend) | **toda** mutação do backend: `feature new/approve/start/complete/reject/substituir`, `decision new`, `reference new`, artifact, `push`, `pull`, `import` | ❌ (1 linha, content-addressed) |
| `materialized` (mapa rel-path → sha256 de cada arquivo de `.docs/`) | regenerado **por inteiro** no `pull`; por-entrada no `push` | ❌ (mapa grande) |

Dois mecanismos distintos de conflito:

1. **`snapshot` é um SHA numa única linha, reescrito a cada operação** ([`CommitAndSnapshot`](../../../../internal/app/artifact.go), [`TransitionFeature`](../../../../internal/app/transition.go), [`runPush`](../../../../internal/cli/pushcmd.go)). Dois branches avançam para SHAs diferentes → o git vê `"snapshot": "A"` vs `"snapshot": "B"` na mesma linha → conflito que ele não sabe resolver. É a mesma classe de dor de `package-lock.json`, pior porque o backend avança a cada evento de ciclo de vida.

2. **`materialized` é um mapa grande** reescrito inteiro no [`runPull`](../../../../internal/cli/pullcmd.go) (`lock.Materialized = manifest`). Dois pulls divergentes → dezenas de linhas em conflito.

### Insight: `materialized` é estado **local**, não verdade compartilhada

A prova está no próprio código:
- o `pull` **sempre** o regenera do backend ([`pullcmd.go`](../../../../internal/cli/pullcmd.go): `lock.Materialized = manifest`) — o valor commitado é descartado no próximo pull; ninguém o lê como insumo de reprodutibilidade;
- os únicos consumidores são do lado do `push` ([`checkContextUntouched`](../../../../internal/cli/pushcmd.go), [`collectChangedDocs`](../../../../internal/cli/pushcmd.go)), que detectam o que **o usuário local** editou em `.docs/current/**` desde o último pull;
- ele descreve hashes de arquivos que **já são gitignored** (`.docs/`). Commitar um índice de hashes de arquivos ignorados é o cheiro.

Logo o `materialized` não precisa — e não deveria — estar no arquivo commitado.

### Insight: o `snapshot` que resta é resolúvel deterministicamente

Todo branch do projeto empurra para o **mesmo backend `main`**. Então os dois SHAs em conflito são, em ~100% dos casos, **ancestral/descendente** na história do backend. "Resolver para o commit de backend mais novo (o descendente)" é a resolução correta e determinística — exatamente o que um **custom git merge driver** faz.

## Objetivo e critérios de sucesso

1. **Frente 1 — manifest sai do arquivo commitado.** O `docs.lock` commitado passa a ser só `{version, backend, snapshot}`. O mapa de hashes vira um sidecar **gitignored e por-worktree**, `.docs/.manifest.json`, escrito pelo `pull` (depois do materialize) e atualizado pelo `push`. Comportamento de `push` (drift-detection contra `snapshot` remoto, detecção de edições locais) **inalterado**.
2. **Frente 2 — merge driver para o `snapshot`.** Novo comando-plumbing `cerne lock merge <base> <ours> <theirs>` (invocado pelo git): valida `version`/`backend` iguais e resolve `snapshot` para o **descendente** no backend (consulta o clone de cache). `cerne init` e `cerne pull` registram o driver (idempotente) + garantem a linha `docs.lock merge=cerne-lock` no `.gitattributes`.
3. **Reprodutibilidade preservada.** O `snapshot` commitado continua pinando o commit exato do backend por commit do projeto (pin por-commit — escolha do dono). O driver só escolhe entre dois pins quando há divergência de branch.
4. **Upgrade sem fricção.** Projeto antigo (com `materialized` no `docs.lock`): `LoadManifest` cai de volta para o `materialized` do lock quando o sidecar ainda não existe; o próximo `pull`/`push` grava o sidecar e **remove** `materialized` do arquivo commitado. Nenhum passo manual.
5. **Degradação graciosa.** Sem o driver registrado (ex.: colega que clonou e ainda não rodou `cerne pull`), o git cai no merge padrão → conflito normal de 1 linha em `snapshot`, trivial de resolver à mão ou re-rodando `pull`. Nada quebra.
6. **Sem regressão; `go vet` + suíte + `go build` verdes.** Inclui ajustar os testes que hoje asseguram `lock.Materialized` para asseverarem sobre o sidecar.

## Decisões do design

| Fork | Decisão | Razão |
|---|---|---|
| **Onde mora o manifest** | `.docs/.manifest.json` (sob a pasta já gitignored, por-worktree) | Co-localizado com o que descreve; não precisa de nova entrada no `.gitignore` do cliente; per-worktree (não pode ir pro cache, que é compartilhado por `projectID` entre worktrees). |
| **Ordem de escrita** | Sidecar gravado **depois** do `Materialize` | [`Materialize`](../../../../internal/materialize/materialize.go) faz `os.RemoveAll(docsDir)` e reconstrói — o `runPull` já recebe o manifest como retorno e o grava após; mantém-se esse fluxo. |
| **Migração** | Fallback de leitura: sidecar ausente → lê `materialized` do `docs.lock`; próxima escrita dropa o campo | Upgrade seamless, sem exigir `pull` manual; `.docs/` é gitignored e reconstruível de qualquer modo. |
| **Regra de resolução** | Escolher o **descendente** no backend; `version`/`backend` divergentes ou histórico realmente divergente → sai ≠0 (humano resolve) | Branches empurram pro mesmo `main` → ancestral/descendente cobre ~tudo; nunca "adivinha" numa divergência real. |
| **Onde o driver acha o backend** | `cwd` (raiz do worktree no merge) → `.cernerc` → `projectID` → clone de cache; `fetch` antes de checar ancestralidade | O git roda o driver na raiz do worktree; o clone de cache é a única cópia local da história do backend. |
| **Registro do driver** | `.gitattributes` (commitado, compartilhado) **+** `merge.cerne-lock.*` no config local; ambos garantidos por `init` **e** `pull` | `.git/config` é local (não viaja no clone); o `pull` é o ponto de sync que todo mundo roda → self-heal para clones novos. |
| **go-git vs binário git** | Escrever o config via go-git (sem depender de `git` no PATH); o driver é só **invocado** pelo git do usuário no merge | Consistência com `internal/gitback`; a resolução só roda em contexto de `git merge`, onde o binário existe por definição. (Worktree edge a validar no plan.) |
| **`materialized` no struct** | Sai do `project.Lock`; persistência via novo `project.Manifest` (`.docs/.manifest.json`) | Tipo commitado fica mínimo; manifest é estado local com seu próprio I/O. |

---

## Desenho

### `internal/project` — split lock/manifest
- `Lock` perde o campo `Materialized`; passa a `{Version, Backend, Snapshot}`. `LoadLock` ainda parseia arquivos antigos (campo extra ignorado); `SaveLock` deixa de emiti-lo.
- Novo par `SaveManifest(projDir, map[string]string)` / `LoadManifest(projDir) (map[string]string, error)` que lê/grava `<projDir>/.docs/.manifest.json`. `LoadManifest`, no miss, tenta `materialized` do `docs.lock` (migração) e só então devolve vazio.

### `internal/cli/pullcmd.go`, `pushcmd.go`
- `runPull`: após `Materialize` + `SaveLock` (só snapshot), grava o manifest via `SaveManifest`. Chama `ensureLockMergeDriver(projDir)` (self-heal).
- `runPush`: lê o manifest via `LoadManifest` (em vez de `lock.Materialized`); atualiza e regrava o sidecar; `lock` só carrega/escreve `snapshot`. Drift-detection (`remoteHead != lock.Snapshot`) inalterada.

### `internal/cli/lockcmd.go` (novo)
- `cerne lock` (grupo) → `cerne lock merge <base> <ours> <theirs>` (oculto; plumbing do git, contrato `%O %A %B`, escreve o resultado em `<ours>`/`%A`).
- Resolução: parseia ours/theirs; `version`/`backend` ≠ → erro pt-BR (sai ≠0). `snapshot` igual → escreve e sai 0. Senão resolve projeto pelo `cwd` → cache repoDir, `Fetch`, e via `gitback` decide ancestralidade: ours ancestral de theirs → theirs; vice-versa → ours; nenhum → erro pt-BR ("backends divergiram — rode `cerne pull` e resolva"). Escreve `{version, backend, snapshot=winner}` em `%A`.

### `internal/cli/initcmd.go`
- Após provisionar (`.cernerc`/`docs.lock`/`.docs/`/skills): `ensureLockMergeDriver(targetDir)` — idempotente, **não-fatal** (aviso + segue; alinhado ao tratamento de skills no 0029).

### `ensureLockMergeDriver(projDir)` (helper compartilhado por init/pull)
- Garante a linha `docs.lock merge=cerne-lock` em `<projDir>/.gitattributes` (append sem duplicar; cria se ausente).
- Garante `merge.cerne-lock.name` + `merge.cerne-lock.driver = cerne lock merge %O %A %B` no config do repo do projeto (via go-git). Idempotente.

### `internal/gitback`
- Helper de ancestralidade (ex.: `IsAncestor(repoDir, a, b) (bool, error)`) e, se preciso, `Fetch(repoDir)`, para o driver decidir o descendente sem shell-out.

---

## Verificação (TDD)
- **`internal/project`:** `TestManifestRoundTrip` (grava/lê `.docs/.manifest.json`); `TestLoadManifestFallsBackToLock` (lock antigo com `materialized`, sidecar ausente → devolve o mapa do lock); `TestSaveLockOmitsMaterialized` (JSON gravado não contém a chave).
- **`internal/cli` (lock merge):** `TestLockMergeChoosesDescendant` (cache com A→B; ours=A, theirs=B → resolvido=B, sai 0); `TestLockMergeBackendMismatch` → ≠0; `TestLockMergeDivergent` (SHAs sem relação) → ≠0 + mensagem.
- **`internal/cli` (registro):** `TestInitRegistersMergeDriver` (`.gitattributes` tem a linha; config tem `merge.cerne-lock.driver`); `TestPullSelfHealsMergeDriver`.
- **`internal/cli` (sidecar):** `TestPullWritesManifestSidecar` (sidecar existe; `docs.lock` **sem** `materialized`); ajustar `pushcmd_test`/`pullcmd_test`/`initcmd_test` que hoje leem `lock.Materialized` para lerem o sidecar.
- `go vet ./...` + suíte verdes; `go build ./cmd/cerne` ok; e2e golden-path verde.

## Fora de escopo
- **Pin por-ref** (commitar `main` em vez do SHA): explicitamente recusado pelo dono — mantemos pin por-commit exato.
- **Auto-resolver divergência real de backend** (dois `main` que de fato bifurcaram): o driver falha com mensagem clara em vez de adivinhar.
- **`cerne doctor` / re-registro em massa** dos drivers em todos os projetos: o `pull` já faz self-heal; um comando dedicado fica como follow-up se necessário.
- **Server/UI:** os caminhos de `internal/server` só escrevem `snapshot`/`backend` (nunca `materialized`) e não materializam `.docs/` — inalterados.
- **Lint de wiki-links** (`[[note]]`): gap conhecido e não relacionado.
