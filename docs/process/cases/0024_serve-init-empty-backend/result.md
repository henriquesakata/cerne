# Resultado — Case 0024

## O que foi entregue

`cerne serve` (multi-projeto) agora **inicializa o layout** ao registrar um
backend vazio. Hosts que provisionam o docs-backend sob demanda (Studio →
`<projeto>-docs` no Gitea) registram o repositório recém-criado e o usam
imediatamente, sem `cerne init` manual.

## Mudanças

- `internal/server/multiproject.go`:
  - `handleRegister` aceita `{backend, name?, modules?}` (antes só `backend`);
    `name` vazio cai para o `pid`.
  - `ensureProjectClone(repoDir, projDir, backend, name, modules)` faz
    **clone-or-init**: após o clone, se `store.LoadProject` retorna
    `ErrNotFound`, roda `store.InitLayout` + `gitback.CommitAndPush`
    ("cerne: initialize backend layout"). Idempotente para backends já
    inicializados (caminho inalterado).
  - Import de `internal/store`.
- `internal/server/multiproject_test.go`:
  - `seedEmptyBackend` (bare origin sem layout) +
    `TestMultiProjectRegisterInitsEmptyBackend` (register → state 0 features →
    cria feature → clone de verificação tem `project.json`).

## Decisões

- Espelha o caminho de _join_ do `cerne init` (`runInit`): mesma sequência
  Clone → `LoadProject` ErrNotFound → `InitLayout` + `CommitAndPush`, já provada
  por `seedBareBackend` nos testes.
- `name`/`modules` vêm no corpo do register em vez de inferir do `pid`: o host
  conhece o nome real do projeto e seus módulos; `pid` é só fallback.

## Verificação

- TDD: o teste novo falha contra o código atual (state 500 "project.json"),
  passa após a mudança.
- `go vet ./...` limpo; `go test ./...` toda verde (incl. e2e, cli).

## Propagação (integração hub)

- Binário local: `scripts/update-cerne.ps1` (CLI do dev).
- O cerne-service do hub builda do source seeded em `.workspaces/rockt/cerne` —
  requer `git push` pro gitlab + reseed (setup-env.sh) antes do build da imagem.

## Próximos passos

- Lado hub (F-0311): endpoint S2S no identity-api que garante `<projeto>-docs`
  (via GiteaProvisionService/GiteaAdminClient) e devolve URL credenciada; o BFF
  do Studio chama esse endpoint, registra aqui (`PUT /api/projects/{id}` com
  `name`/`modules`) e proxia.

## Arquivos

- `internal/server/multiproject.go` (mod)
- `internal/server/multiproject_test.go` (mod)
- `docs/process/cases/0024_serve-init-empty-backend/{spec,plan,result}.md` (new)
- `docs/process/CLAUDE.md` (index, mod)
