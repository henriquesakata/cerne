# Case 0004 — Idempotência do registro histórico (dedup)

tags: #spec #cerne #stub #domain #lint #historical

**Status:** Draft (stub — precisa de rodada de brainstorming para fechar decisões)
**Tipo:** Extensão do núcleo de domínio/app do registro histórico (case 0002) + possível defesa no lint.
**Origem:** Revisão do case 0002 (2026-06-13). Dívida nº 2 do [result.md do 0002](../0002_historical-entity-registry/result.md), mas **confirmada ao vivo como mais séria do que "lixo de entidade"**: re-registrar uma entidade histórica corrompe a contagem do lint.

---

## Problema

O registro histórico (case 0002) **não é idempotente**. `cerne retire` e o seed `historical.toml` funilam pelo mesmo núcleo `SaveHistoricalEntity`, que sempre faz `NextID` + cria — **sem checar se já existe uma histórica com o mesmo `name`/alias**. Não há dedup em `internal/importer/seed.go` nem em `internal/app/historical.go`.

**Impacto confirmado na rodada de revisão** (clone bare do `hub-docs`, 308 features): com Bills+Investments registrados uma vez, `ref-to-superseded` = 89. Após rodar `retire "Bills"`/`retire "Investments"` **de novo** (criando H-0003/H-0004, duplicatas de H-0001/H-0002), o lint passou a contar **a mesma menção no mesmo arquivo 2× e 4×** — `ref-to-superseded` saltou para **160**. Ou seja: re-rodar `retire` ou re-importar o seed **infla a saída do lint com achados duplicados**, não apenas cria entidades redundantes.

Agrava o cenário: o backend real `gitlab.com/rockt/hub-docs` **já contém** H-0001 (Bills) + H-0002 (Investments) de um dogfood anterior — qualquer re-semeadura criaria duplicatas em produção.

---

## Objetivo e critérios de sucesso (rascunho)

Registro histórico **idempotente**: re-rodar `retire`/seed com um `name` já existente não cria duplicata, e o lint conta cada menção **uma vez**.

Critérios-alvo:
- `cerne retire "Bills" ...` duas vezes ⇒ uma única entidade `H-NNNN`; segunda invocação é no-op (ou update — decidir).
- Re-importar o mesmo `historical.toml` ⇒ zero entidades novas.
- Lint sobre um corpus com Bills/Investments registrados ⇒ contagem `ref-to-superseded` estável entre execuções (sem inflar).
- Caminho de limpeza para as duplicatas que já existirem (ou pelo menos detecção).

---

## Direção a explorar (alternativas — decidir no brainstorming)

1. **Dedup-on-write no núcleo** (`SaveHistoricalEntity`): antes do `NextID`, procurar histórica existente com mesmo `name` (case-insensitive); se existe, **no-op** ou **merge** (unir aliases, atualizar `reason`/`superseded_by`). Como `retire` e seed compartilham o núcleo, conserta os dois de uma vez.
2. **Semântica de update** explícita: `retire` de nome existente atualiza a entidade (merge de aliases, sobrescreve `by`/`reason`) e emite evento `historical.updated`. Mais expressivo, mas exige decidir o que "merge" significa.
3. **Dedup só no import** (seed pula nomes já presentes), mantendo `retire` ad-hoc como está. Mais estreito; não resolve `retire` repetido.

**Defesa-em-profundidade (transversal, talvez independente):** o lint deduplicar findings por `(arquivo, trecho-da-menção)` mesmo quando entidades distintas compartilham um alias — duas históricas legítimas podem ter o mesmo alias e ainda assim a mesma menção não deveria contar duas vezes. Vale mesmo se a idempotência entrar.

---

## Questões em aberto

1. Chave de dedup: só `name`? `name` + qualquer `alias`? Normalização (case-folding, acentos)?
2. No-op vs update: re-registrar com aliases diferentes funde ou ignora? Precisa de `historical.updated` no log?
3. Limpeza das duplicatas que já existem no `hub-docs` real (H-0003/H-0004 só apareceram no meu clone local, mas H-0001/H-0002 estão no GitLab): comando `cerne forget <id>`? Migração manual?
4. A defesa no lint (dedup por menção) é deste case ou de um follow-up do lint?

---

## Próximos passos

1. **Brainstorming** para escolher dedup-on-write vs update-semantics e a chave de dedup.
2. `plan.md` (TDD): domínio/app (dedup no núcleo) → import/retire (reuso) → regressão de lint (contagem estável) → teste de dupla-execução.
3. `result.md`.
