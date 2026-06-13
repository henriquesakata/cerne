# Case 0004 — Idempotência do registro histórico (dedup)

tags: #spec #cerne #domain #lint #historical

**Status:** Done — implementado e verificado no corpus real do hub (2026-06-13); ver [result.md](result.md)
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

## Decisão tomada (2026-06-13)

**Upsert por nome normalizado** no núcleo `SaveHistoricalEntity` (combina as alternativas 1 + 2) — escolhido por dar idempotência convergente: re-rodar `retire`/seed leva ao estado declarado, em vez de no-op (que descartaria silenciosamente aliases novos) ou erro (que quebraria o re-import de seed).

| Questão | Decisão |
|---|---|
| Chave de dedup | **Nome normalizado via `slug.Fold`** (lowercase + sem acento). Casa `Bills`/`bills`/`BILLS`; nomes distintos seguem distintos. |
| No-op vs update | **Update (upsert)**: entidade existente é atualizada in-place — mantém `ID`+`CreatedAt`, sobrescreve `superseded_by`/`reason`/`imported_from`, **une aliases** (existentes ∪ novos, dedup case-insensitive, ordem estável). Emite `historical.updated`; criação nova emite `historical.recorded`. |
| Limpeza de duplicatas existentes | **Desnecessária**: o `hub-docs` real só tem H-0001/H-0002 (legítimas). As duplicatas H-0003/H-0004 só existiram no clone local da revisão. `cerne forget` fica como follow-up se um dia surgir necessidade. |
| Defesa no lint (dedup por menção) | **Fora de escopo deste case**: a idempotência elimina a causa raiz (entidades duplicadas). O dedup de findings por `(arquivo, entidade)` do case 0001 já cobre o resto. Fica como follow-up de lint se duas históricas legítimas vierem a compartilhar alias.

---

## Verificação (corpus real do hub)

Clone bare local do `hub-docs` (308 features, H-0001/H-0002 já presentes): `ref-to-superseded = 89`. Após **re-`retire` Bills + Investments** com o binário corrigido: mensagem "H-000X atualizada (já existia…)", **2 históricas** (sem H-0003/H-0004), `ref-to-superseded = 89` (estável — não foi a 160). Bug 89→160 fechado.
