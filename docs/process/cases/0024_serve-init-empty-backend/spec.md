# Case 0024 — `serve` inicializa backend vazio no register

## Contexto

O case 0022 (`cerne serve` multi-projeto) registra o backend de um projeto via
`PUT /api/projects/{pid}`, clonando o repositório informado. O clone assume um
backend **já inicializado** (com `project.json` e a árvore de layout do cerne).

Na integração com hosts que provisionam o docs-backend sob demanda (ex.: o Studio
cria `<projeto>-docs` no Gitea na primeira vez), o repositório recém-criado está
**vazio de layout** — tem no máximo um commit inicial (README via `auto_init`),
mas nenhuma estrutura cerne. Hoje, registrar esse backend "funciona" (clona), mas
a primeira operação (`POST /features`) falha porque não há layout.

## Problema

`ensureProjectClone` (multiproject) só clona; não inicializa o layout quando o
backend está vazio. Resultado: um `<projeto>-docs` recém-provisionado não é
utilizável até alguém rodar `cerne init` manualmente contra ele — o que o host
(serviço S2S) não faz.

## Decisão

Estender o register para **inicializar o layout quando ausente**, espelhando o
caminho de _join_ do `cerne init` (`runInit`): após o clone, se
`store.LoadProject(repoDir)` retornar `ErrNotFound`, rodar `store.InitLayout` +
`gitback.CommitAndPush("cerne: initialize backend layout")`. Idempotente: um
backend já inicializado é deixado intacto.

O corpo do register passa a aceitar `name` e `modules` opcionais para popular o
`project.json` inicial (fallback de `name` = `pid`). Nada muda para backends já
inicializados.

## Escopo

- `internal/server/multiproject.go`: `handleRegister` lê `name`/`modules`;
  `ensureProjectClone` faz clone-or-init.
- Teste: registrar um backend bare vazio (sem layout) e verificar que o layout é
  criado + empurrado e que uma feature pode ser criada em seguida.

## Fora de escopo

- Provisionamento do repositório no git-host (responsabilidade do host/identity-api).
- Credenciais de push do cerne-service (embutidas na URL do backend pelo host).
- O caso truly-empty (origin sem commit algum) já é coberto pela sequência
  Clone+InitLayout+CommitAndPush — a mesma de `seedBareBackend` nos testes.
