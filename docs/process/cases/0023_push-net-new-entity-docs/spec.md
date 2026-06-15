# Case 0023 — `push` aceita docs net-new de entidade rastreada (plan/result)

tags: #spec #cerne #push #authoring #bugfix

**Status:** Done (2026-06-14) — ver [result.md](result.md)
**Tipo:** Correção do `cerne push` — aceitar `plan.md`/`result.md` net-new sob uma feature já rastreada.
**Origem:** Dogfooding (autoria do F-0311 no hub): o fluxo cerne-native documentado ("author plan.md → `cerne push` → `cerne feature start`") **falha** para features net-new. `cerne feature new` cria só o stub de `spec.md` (rastreado); ao autorar um `plan.md` novo e dar `cerne push`, o comando barra com *"arquivo não rastreado … rode 'cerne pull'"* e o `feature start` fica bloqueado (gate de plano). Só features **importadas** (que vieram com plan.md) tinham plano.

---

## Problema

`collectChangedDocs` (pushcmd.go) trata **qualquer** arquivo em `.docs/current/**` ausente do manifest do `docs.lock` como "não rastreado" e aborta. Isso é certo para lixo, mas **rejeita também um `plan.md`/`result.md` legítimo** autorado depois do `spec.md` (o caminho normal pós-`feature new`). Resultado: não há como pushar plan/result de uma feature net-new → `feature start`/`complete` travam.

---

## Objetivo e critérios de sucesso

1. `cerne push` **aceita** um doc net-new quando ele é `plan.md` ou `result.md` sob um **folder de feature já rastreado** (um irmão — tipicamente `spec.md` — está no manifest). Escreve no backend (`content/features/<ID>/<doc>.md`), adiciona ao manifest e commita (o resto do `runPush` já faz isso).
2. **Preserva** a rejeição de arquivos arbitrários: um `notes.md` (ou qualquer nome fora de spec/plan/result) sob um folder de feature continua "não rastreado" (mantém o teste existente).
3. Continua respeitando o **drift guard** (snapshot remoto ≠ local → "backend avançou, rode pull") — ortogonal a este fix.
4. Suíte + `go vet` verdes; regressão TDD reproduz o cenário (plan.md net-new pushável; notes.md ainda barrado).

---

## Decisões do design

| Fork | Decisão | Razão |
|---|---|---|
| Escopo do aceite | só `plan.md`/`result.md` sob feature rastreada | É o gap real (spec já é stub do `feature new`; decisões/refs nascem stubadas por `decision/reference new`). Evita aceitar lixo. |
| "Entidade rastreada" | um irmão no mesmo folder `current/features/<slug>/` está no manifest | Usa só o manifest (sem I/O extra); o `spec.md` rastreado prova que a feature existe. |
| Onde corrigir | `collectChangedDocs` (guard `!tracked`) | Mínimo; `runPush` já escreve novo arquivo + `lock.Materialized[rel]=sum` + commit. |

---

## Desenho

`internal/cli/pushcmd.go`:
- Novo helper `acceptableNewDoc(rel, manifest) bool`: `true` sse `rel` começa com `current/features/`, `path.Base(rel)` ∈ {`plan.md`,`result.md`}, e existe `k` no manifest com prefixo `path.Dir(rel)+"/"`.
- `collectChangedDocs`: no ramo `!tracked`, em vez de abortar, abortar **só** se `!acceptableNewDoc(...)`; senão segue (parse cerne_id → backendRelPath → append). Arquivo rastreado e inalterado continua pulado.

---

## Verificação
- **TDD:** fixture com feature `F-0001` (spec rastreado no manifest); autorar `current/features/F-0001-…/plan.md` com `cerne_id` → `push` envia (backend tem `content/features/F-0001/plan.md`; manifest passa a incluí-lo). 
- **Não-regressão:** `notes.md` net-new sob a mesma feature → ainda "não rastreado" (teste existente intacto).
- `go vet` + suíte verdes. Smoke real: re-tentar o `cerne push` do plan.md do F-0311 (no hub) → sucesso → `cerne feature start F-0311`.

---

## Fora de escopo
- Docs net-new de decisão/referência sem stub (nascem via `decision/reference new`).
- Criar entidade nova via push (entidades nascem via `feature/decision/reference new`).
- Deleção via push (continua não suportada).
