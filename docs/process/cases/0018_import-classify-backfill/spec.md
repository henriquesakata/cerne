# Case 0018 — `import` auto-classifica módulos (sem subsistema órfão)

tags: #spec #cerne #import #classify #wiki

**Status:** Done (2026-06-14) — ver [result.md](result.md)
**Tipo:** Comportamento do `cerne import` (roda o backfill de classificação + reporta resíduo sem módulo).
**Origem:** Dogfood do [case 0017](../0017_wiki-on-web/spec.md) no hub: o **studio** (subsistema real, módulo declarado) **não tinha página de wiki** porque nenhuma feature estava classificada nele. A correção exigiu rodar `cerne classify --yes` manualmente. Follow-up acordado: o `import` não deveria deixar subsistemas órfãos.

---

## Problema

O importer **já infere** módulo para cross-app cases ([importer.go](../../../internal/importer/importer.go) chama `classify.InferModule(source, specBody, modules)`), **mas só na primeira importação de cada doc**. O `import` é idempotente: pula o que já foi importado (`imported_from`) e **nunca revisita**.

Consequência (observada no hub com o studio): uma feature importada **antes** do seu módulo ser declarado fica sem módulo — `InferModule` não casa um módulo não-declarado — e, como o import nunca a revisita, ela permanece órfã **para sempre**. Features sem módulo **não entram na wiki** (o `compile` pula módulos sem fontes), então um subsistema inteiro **some** silenciosamente da síntese até alguém lembrar de rodar `cerne classify`. Nada avisa.

---

## Objetivo e critérios de sucesso

1. **`cerne import --yes` roda o backfill de classificação** (`app.ClassifyBackfill`) depois de aplicar a importação — varrendo **todas** as features sem módulo (não só as recém-importadas), de forma que declarar um módulo novo + re-importar reclassifique as features existentes que passaram a casar. Determinístico (single-match), idempotente, só toca features sem módulo.
2. **O backfill roda mesmo quando não há nada novo para importar** — o valor principal é reclassificar features **já existentes** após um módulo ser declarado (o caso do studio). Hoje o import dá early-return em "nada para importar" antes de qualquer trabalho.
3. **`import` sempre reporta o resíduo sem módulo** (apply e dry-run) — nada de drop silencioso. O dry-run projeta o que o backfill faria (`ClassifyBackfill` apply=false); o apply reporta quantas classificou e quantas permanecem.
4. Suíte + `go vet` verdes; regressão reproduz o cenário (feature importada sem o módulo declarado → módulo declarado → re-import classifica).

---

## Decisões do design

| Fork | Decisão | Razão |
|---|---|---|
| **Auto-rodar vs avisar** | `import --yes` **roda** o backfill (não só avisa) | Conserta sozinho; o backfill é a mesma inferência single-match que o import já usa — alta precisão, sem reclassificar o que já tem módulo. Avisar sozinho deixaria o buraco (ainda precisaria do comando manual). |
| **Sempre rodar o backfill** | Rodar mesmo com 0 candidatos novos (remover o early-return cego) | O cenário real (studio) é reclassificar features **já importadas** após declarar o módulo — não há candidato novo. |
| **Resíduo sempre reportado** | apply e dry-run imprimem "M sem módulo permanecem (`cerne classify` p/ revisar)" | Sem drop silencioso; o operador vê subsistemas/áreas ainda ambíguas. |
| **Sem flag de opt-out** | Sem `--no-classify` por ora (YAGNI) | Minimalismo; o backfill é seguro/idempotente. Adiciona-se depois se surgir necessidade real. |
| **Commits separados** | import faz seu commit (se aplicou docs); o backfill faz o seu (se classificou) | Cada um já commita+pusha+atualiza o lock; manter assim evita reescrever o fluxo de commit. |

---

## Desenho

### `internal/cli/importcmd.go` (`runImport`)

Reestruturar o fluxo para o backfill rodar nos dois modos, sem o early-return cego:

- **dry-run** (`!yes`): imprime o plano de importação (como hoje; "nada novo" quando vazio) **e** uma projeção do backfill — `assigned, skipped, _ := app.ClassifyBackfill(ctx.repoDir, "", false)` → "Inferência classificaria N feature(s) sem módulo; M permaneceriam." (`projDir=""` é seguro: apply=false nunca commita).
- **apply** (`yes`): aplica a importação (só quando há cands/seeds; mantém o commit + lock atuais) e **depois** `assigned, skipped, _ := app.ClassifyBackfill(ctx.repoDir, ctx.projDir, true)` (commita+pusha+lock só se classificou ≥1) → reporta "Classificadas N feature(s) por inferência. M sem módulo permanecem (`cerne classify` para revisar)."

Mensagens em pt-BR. O resíduo (`skipped`) é sempre reportado quando > 0.

---

## Verificação

- **CLI (regressão do studio):** backend com módulo `finance` **não** declarado; importar um root case cujo conteúdo casa "finance" → fica sem módulo (inferência não casa não-declarado). Declarar `finance` no projeto. Rodar `import --yes` de novo (nada novo a importar) → a feature existente vira `module=finance`; saída reporta "Classificadas 1". 
- **CLI dry-run:** com features sem módulo classificáveis, `cerne import` (sem --yes) imprime a projeção "classificaria N … M permaneceriam" e **não escreve** (sem commit).
- **CLI apply normal:** import com candidatos novos + backfill no mesmo run reporta importados **e** classificados.
- **Idempotência:** segundo `import --yes` sem mudanças não classifica nada novo (0) e não cria commit vazio.
- `go vet` + suíte verdes; **smoke** opcional no hub (re-rodar import e ver 0 novos classificados, já que o studio foi corrigido).

---

## Fora de escopo / staging

- Inferência por **conteúdo/keywords** além do que o `InferModule` já faz (folder + tags + conteúdo) — não muda a heurística, só **quando** ela roda.
- Reclassificar features que **já têm** módulo (mudança de módulo) — o backfill nunca toca features com módulo; reassignment continua manual (`classify <id> --module`).
- Override de `[llm] mode` por env — follow-up separado anotado no dogfood do 0017.
