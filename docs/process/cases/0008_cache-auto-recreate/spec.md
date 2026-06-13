# Case 0008 — Auto-recuperação do clone de cache

tags: #spec #cerne #cli #ux

**Status:** Done — implementado e verificado (2026-06-13); ver [result.md](result.md)
**Tipo:** Correção de UX no `resolvePaths` (CLI).
**Origem:** Atrito real observado operando o cerne no hub (2026-06-13). O clone de cache do backend vive em `%LOCALAPPDATA%\cerne\<project_id>\repo`. Quando ele some (reboot que limpa `/tmp`, máquina nova, cache apagado), todo comando que precisa do backend falha — e a mensagem leva a um beco sem saída.

---

## Problema

`internal/cli/context.go` `resolvePaths`: quando `<cache>/repo/.git` não existe, o comando aborta com:

```
cached backend clone missing at <repo> (run 'cerne init' again to recreate it)
```

Mas `cerne init` **recusa** com `project already initialized` quando `.cernerc` existe (o projeto está inicializado — só o cache local sumiu). **Impasse.** O workaround manual é `git clone <backend> <cache>/repo`, que o usuário não deveria precisar saber.

O cache é um **artefato derivado** (índice + clone), reconstruível a partir do que está commitado (`.cernerc` tem o backend; `docs.lock` fixa o snapshot). Como `node_modules` a partir do `package.json`: se sumir, a ferramenta recria — não manda o usuário reinicializar o projeto.

---

## Objetivo e critérios de sucesso

Qualquer comando que precise do backend **recria o clone de cache** automaticamente quando ele falta, a partir do `backend` do `.cernerc`. Sem impasse, sem flag nova.

**Critério de aceite:**
1. Projeto com `.cernerc`/`docs.lock` válidos mas `<cache>/repo` ausente: `cerne status`/`pull`/`lint`/`search`/`compile`/`classify`/`push` **recriam** o clone e funcionam — sem erro, sem `init`.
2. A recriação clona do `cfg.Backend`; um aviso vai para **stderr** (não polui stdout de `--json`).
3. `init` segue recusando re-inicialização (`.cernerc` existente) — correto; o cache deixou de depender disso.
4. Suíte verde, `go vet` limpo; teste pinando o cenário (cache ausente → recriado).

---

## Decisão

**Auto-recreate em `resolvePaths`** (não em `init`): é o ponto único por onde todos os comandos passam. Quando `repoDir/.git` falta, limpa um `repoDir` parcial, `MkdirAll(cacheDir)`, `gitback.Clone(cfg.Backend, repoDir)`, aviso em stderr. `index.db` é tratado pelo fluxo existente (pull/ search reconstroem). Mensagem de falha do `resolveProject` (pull) atualizada para "apague o diretório e rode o comando de novo" (não mais "rode init").

**Não escolhido:** `init --recreate-cache` (uma flag a mais para um problema que não devia existir) e fazer o `status` ser o único a recriar (deixaria os demais comandos no impasse).

---

## Verificação

Dogfood: num projeto inicializado, apagar `<cache>/repo`, rodar `cerne status` → recria o clone e imprime o status normalmente.

---

## Fora de escopo

- Verificação de integridade do cache (corrupção parcial além de `.git` ausente).
- `init --recreate-cache`.
