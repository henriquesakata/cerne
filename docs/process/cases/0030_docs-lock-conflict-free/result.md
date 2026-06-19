# Case 0030 — Resultado

tags: #result #cerne #docs-lock #git #dx

**Status:** Implementado (2026-06-18) — suíte verde (506 testes), validado end-to-end com `git merge` real.

O `docs.lock` deixou de ser fonte de conflitos de merge: o arquivo commitado encolheu para o pin (`{version, backend, snapshot}`) e o git resolve o `snapshot` sozinho via um merge driver. Sem mudar o contrato de reprodutibilidade (pin por-commit preservado).

## O que mudou (frente A — manifest fora do lock)
- `internal/project/manifest.go`: novo sidecar `.docs/.manifest.json` (`SaveManifest`/`LoadManifest`) — gitignored, por-worktree. `LoadManifest` cai de volta para o `materialized` de um `docs.lock` pré-0030 (raw parse), então o upgrade é seamless.
- `internal/project/lock.go`: o campo `Materialized` saiu de `project.Lock`. O `docs.lock` commitado agora é só `{version, backend, snapshot}`.
- `pull` grava o manifest no sidecar após o `Materialize`; `push` lê/atualiza o sidecar (drift-detection contra o `snapshot` remoto inalterada); `init` não semeia mais o mapa.

## O que mudou (frente B — merge driver)
- `internal/gitback/ancestry.go`: `IsAncestor(repo, a, b)` (go-git) + `Fetch` (traz objetos sem mover o HEAD).
- `internal/cli/lockcmd.go`: `cerne lock merge <base> <ours> <theirs>` (contrato git `%O %A %B`) — `version`/`backend` têm de bater, e o `snapshot` que é o commit **descendente** no backend vence. Divergência real (nenhum ancestral do outro) é recusada com dica pt-BR → git cai no conflito normal.
- `ensureLockMergeDriver` registra a linha `docs.lock merge=cerne-lock` no `.gitattributes` (commitado) + `merge.cerne-lock.*` no config local (não viaja no clone). Chamado por `init` e por `pull` (self-heal). Idempotente; no-op gracioso fora de repo git.

## Aceitação
- `go vet ./...` limpo; `go test ./... -count=1` = **506 testes verdes** em 23 pacotes; `go build ./cmd/cerne` ok.
- **End-to-end real:** dois branches pinando `S1` e `S2` (S1 ancestral de S2); `git merge` fez *Auto-merging docs.lock* e resolveu sozinho para `S2` (descendente), byte-idêntico, sem marcadores de conflito, exit 0.
- **Worktree (a dor original):** após `cerne pull` de dentro de uma `git worktree`, o driver fica registrado no config compartilhado — validado com `git config --get`.

## Desvios do plano
- **`PlainOpenWithOptions{EnableDotGitCommonDir: true}`** (não o `PlainOpen` padrão): o registro a partir de uma worktree falhava silenciosamente porque o go-git não resolvia o *commondir* (o `.git` da worktree é um arquivo, não um diretório). Pego na validação manual; blindado por teste de regressão (pulado no Windows, onde um handle in-process do go-git impede a limpeza do `t.TempDir`; o CI/Linux roda).
- Mantido go-git (sem dependência do binário `git`) no caminho de produção; o `git` só é exigido nos testes de worktree (com `t.Skip` se ausente).

## Débitos / próximos passos
- **Adotar o driver no próprio repo do cerne:** commitar `.gitattributes` com `docs.lock merge=cerne-lock` e cada clone rodar `cerne pull`/`cerne init` uma vez (self-heal do config local). Mata a colisão de `docs.lock` nas worktrees paralelas deste repo.
- **Entrega:** após o merge, `go install ./cmd/cerne` para atualizar o binário global.
- **Divergência real** (dois `main` que de fato bifurcaram) continua sendo recusada — decisão de design (não adivinhar); o humano roda `cerne pull` e resolve.
