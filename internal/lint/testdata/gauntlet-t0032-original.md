---
cerne_id: T-0032
---

# Ativos e passivos: barra proporcional por grupo, em totais ou percentual

**Spec:** [T-0032](./spec.md) · **Épico:** E-0003
**Data:** 2026-10-03
**Agente:** claude-opus-5-5 · esforço 80/100 (`reasoning_effort` do harness)
**Branch:** `claude/e0003-t0032` (sai do T-0030), HEAD de código `b7a4f561`

## Resultado

Entregue. O widget `assets-liabilities` está no catálogo da Visão Geral, oculto por padrão e com uma coluna: cada lado (Ativos, Passivos) é o total, uma barra proporcional por grupo e a lista na ordem da barra, sobre o ponto de hoje da carga compartilhada do T-0030. O Gauntlet fechou com duas vitórias do juiz cego contra o painel "Summary" do Monarch e 8/10 do crítico de design.

## Aceitação

Caminhos relativos a `web/`. `W` = `app/_components/dashboard/AssetsLiabilitiesWidget.test.tsx`; `B` = `app/_components/dashboard/dashboard.browser.test.ts` (Chrome, 375px e 900px); `C` = `app/_components/dashboard/dashboard-consistency.test.tsx`.

| Critério | Estado | Evidência |
|---|---|---|
| critério 1 — largura do trecho = participação; grupo com menos de 1% ainda aparece; a soma visual não passa de 100% | ATENDIDO, com o piso em 4px em vez de 2px (desvio aceito pelo lead) | `W:134` "cada bloco tem o total, a barra com um trecho por grupo na largura da participação…"; `W:216` "um grupo com menos de 1% tem o seu trecho…"; `B:556` (trechos dentro da barra, piso e vão medidos, soma ≤ barra, nas oito cenas); `B:616` (largura medida ≈ participação, ±1,5 ponto); `B:695` (trecho de um terço de ponto com 4px) |
| critério 2 — a lista segue a ordem da barra, do maior para o menor; amostra, nome e valor ou percentual | ATENDIDO | `W:134`; `W:168` "a amostra de cada linha é a cor do seu trecho…"; `W:366` "Percentual troca o valor de cada linha pela participação…"; `B:556` (amostra casada com o trecho pelo grupo) |
| critério 3 — sem passivo, o bloco Passivos fica, com "Nenhuma dívida hoje" | ATENDIDO | `W:396` "sem cartão e sem conta no negativo: o bloco Passivos fica, com a frase…"; `W:412` "um cartão sem nada a pagar também é nenhuma dívida"; `B:733` (cena `-clear`) |
| critério 4 — grupo cifrado: `Locked` na linha, total do bloco como `Locked` ("total cifrado"), sem barra | ATENDIDO | `W:429` "a compra de cartão que não decifra: Locked na linha, o total do bloco cifrado e nenhuma barra…"; `W:450`; `W:459`; `W:475`; `B:733` (cenas `-locked` e `-shut`) |
| critério 5 — `dashboard-consistency.test.tsx` monta T-0030 e T-0032 sobre um fixture e compara o líquido | ATENDIDO | `C:161` "o líquido do cabeçalho do Patrimônio líquido é Ativos menos Passivos…, sobre UMA leitura dos doze meses" (sob o `NetWorthProvider` real, só as leituras mockadas); `C:174` (segue batendo com o Patrimônio em "Tudo"); `C:187` (cada total é a soma das suas linhas) |
| E-0003 critério 1 — o widget no catálogo, ocultável, reordenável, 1 ou 2 colunas | ATENDIDO | `W:674` "está no catálogo, fora da Visão Geral padrão e com uma coluna"; `app/_components/DashboardClient.layout.test.tsx:522` (ligado no layout, é o painel do widget numa célula de uma coluna); `B:673` (duas colunas: os lados lado a lado) |
| E-0003 critério 2 — um layout salvo ganha o widget com `defaultVisible` (oculto) | ATENDIDO | `app/_components/DashboardClient.layout.test.tsx:522` ("sem layout salvo, e com um layout salvo antes do widget, ele não aparece") |
| E-0003 critério 8 — total, barra por grupo, lista; `SegmentedControl` "Totais \| Percentual" | ATENDIDO | `W:134`; `W:358` "o controle é um grupo rotulado no cabeçalho do painel…"; `W:366`; `W:383` (os percentuais somam 100,0%); `B:652` |
| E-0003 critério 10 — a mesma grandeza, o mesmo número (líquido do T4 = ativos − passivos do T6) | ATENDIDO na parte desta task; o caso "total do T7 = passivo Cartões" entra com o T-0033 no mesmo arquivo | `C:161`, `C:174`, `C:198`, `C:204`, `C:210`; `C:187` já prova que a linha Cartões é a soma de `cardDebtOn` por cartão |
| E-0003 critério 11 — cifra e carga: `Locked`, `LoadError` com retry, nunca soma parcial ou zero falso | ATENDIDO | `W:429`–`W:475` (cifra); `W:486` (janela truncada não muda hoje); `W:631` (carregando), `W:640` (falha com retry), `W:649` (vazio só com carga ok e nada a somar), `W:658` (ponto ausente é erro, com causa e retry) |
| E-0003 critério 12 — uma leitura | ATENDIDO | `W:134` (o widget pede só o escopo `12m`); `C:161` (`readNetWorthRows` e `loadPortfolioSources` chamados uma vez com três consumidores) |
| E-0003 critério 13 — Arc: só tokens, sem degradê, cor de série só do `chartTheme`, ícone Lucide, guardas sem exceção nova | ATENDIDO | `lib/net-worth.colors.test.ts:21`–`:48`; `app/_components/chart-theme.test.ts:79`; `W:168`; `design-tokens`, `icons`, `figures`, `load-states`, `click-feedback`, `primitives` e `shared-css` verdes no `vitest run` (230 arquivos) sem entrada nova nas listas |
| E-0003 critério 14 — a 375px cabe numa coluna, sem rolagem lateral, sem rótulos sobrepostos | ATENDIDO | `B:556` nas oito cenas a 375px: `scrollWidth ≤ clientWidth`, nada passa do painel, nome, participação e valor numa linha sem se tocar |
| E-0003 critério 15 — gráfico `aria-hidden` com alternativa em texto; não depender só da cor; controle rotulado | ATENDIDO | `W:247` "a barra é um desenho escondido do leitor de tela…; a lista diz o valor e a participação de cada grupo"; `W:358`; `B:556` (rótulo do grupo presente e não desenhado) |
| E-0003 critério 16 — strings em pt-BR e en sob `Finance.dashboard.*`; figuras por `lib/format.ts` | ATENDIDO | `lib/messages-parity.test.ts` e `app/figures.guard.test.ts` verdes; `W:680`, `W:692`; `lib/format.test.ts:129` (`formatPercentFloor`) |
| E-0003 critério 17 — cena de captura | ATENDIDO | `scripts/render-static.tsx:2224`–`:2228` (`dashboard-assets-liabilities` + `-percent`, `-hover`, `-wide`, `-sliver`, `-locked`, `-shut`, `-clear`); `B:411` exercita as oito |

Portões no HEAD `b7a4f561`: `npx tsc --noEmit` sem erro; `npx vitest run` 230 arquivos, 3536 testes; `npm run build` ok; `npx vitest run browser` 6 arquivos, 120 testes, sem SKIPPED; guardas do hub 14/14; detector do `impeccable` `[]`.

## O que mudou

- **Novo** `web/app/_components/dashboard/AssetsLiabilitiesWidget.tsx` + `_assets-liabilities.module.css`: os dois lados, a barra em CSS (`flex-basis` em percentual, cor por `--slice`), a lista como legenda e alternativa em texto, `SegmentedControl` compacto no `aside` do `WidgetPanel` (na linha do título, alvo de 44px no celular).
- **`web/lib/net-worth.ts`** (só acréscimos; `netWorthSeries`, `groupOrder` e os tipos não mudaram): `groupColor(group)`, `drawnAs(group)`, `CLASS_SLOTS`.
- **`web/app/_components/chart-theme.ts`**: `chartTheme.owed` (`strong` = o laranja do par de dados; `soft` = o mesmo laranja a 60% sobre o card).
- **`web/lib/format.ts`**: `formatPercentFloor` ("<0,1%" para uma parte que não é zero).
- **`web/app/_components/dashboard/netWorthData.tsx`** (só acréscimo): `useNetWorthRetry(scope)`.
- **Catálogo e página**: entrada `assets-liabilities` no fim de `BUILTIN_WIDGETS` (`widgetRegistry.ts:48`), ramo no fim de `renderWidget` (`DashboardClient.tsx:548`), chaves `Finance.dashboard.widgets.assetsLiabilities` e `Finance.dashboard.assetsLiabilities.*` em pt-BR e en.
- **Testes**: `AssetsLiabilitiesWidget.test.tsx` (47), `dashboard-consistency.test.tsx` (6), `net-worth.colors.test.ts` (4), bloco T-0032 em `dashboard.browser.test.ts` (27), casos em `DashboardClient.layout.test.tsx`, `chart-theme.test.ts`, `netWorthData.test.tsx` e `format.test.ts`.
- **Capturas**: cenas `dashboard-assets-liabilities*` em `scripts/render-static.tsx`.

## Débitos e follow-ups

- **Paleta interina do `chartTheme` (hub).** O tom do devido (`owed.soft`) e os dois tons de `series` ficam abaixo de 3:1 contra a superfície; a lista com nome e valor é o alívio. Trocar quando o `@rockt/ui` publicar tokens categóricos de dados.
- **Integração com o branch do épico.** Este branch não tem T-0027/28/29; os blocos em arquivos compartilhados entraram no fim. O lead pede a integração quando o T-0030 estiver lá.
- **T-0033** acrescenta o caso dos cartões a `dashboard-consistency.test.tsx`.
- **T-0031** reusa `groupColor` e `drawnAs`; conferir lá a distância entre `owed.soft` e o tom dos ETFs (`series[5]`), que na Composição dividem a legenda.
- **Sem link no cabeçalho** para `/accounts` e sem animação na troca de escala (o detector reprova transição de largura).

## Aprendizados e desvios

Desvios da spec e do plano, todos por decisão ou aceite do lead na revisão:

- **Cor fixa por grupo** (`groupColor`), não `seriesColor(i)` na ordem da lista: o grupo tem a mesma cor qualquer que seja o conjunto presente.
- **Passivos na família do laranja** de `polarity.negative` (`chartTheme.owed`), nunca a cor de um ativo.
- **Totais em escala comum, Percentual em 100% cada**: o alternador muda o desenho, não só os números. Sob a barra menor, uma legenda diz o que o resto do trilho é ("2,0% dos ativos"); abaixo de um vigésimo do trilho a barra é um toco de 8px dividido como o bloco.
- **Piso de 4px no trecho** (o critério 1 dizia 2px).
- **Participação ao lado do valor** em Totais.
- **Classes além dos slots em "Outros investimentos"**, somadas numa linha.
- **Sem linha focável**: o ponteiro só destaca, não revela dado.
- **Lado com parte negativa ou total não positivo fica "sem base"** (sem barra, sem participação), sem limitar em zero: o total segue o que o Patrimônio subtrai.

Do processo:

- O `cerne feature start` rodou antes do `cerne push` do plano (o primeiro Write foi bloqueado pelo hook de worktree); o plano foi enviado em seguida.
- O cerne guard recusa commit cuja mensagem cite uma task ainda não iniciada.
- `npx vitest run browser` oscilou com a máquina saturada por outros agentes (carga média acima de 10): estouro de tempo em suítes de navegador, uma delas alheia a esta task. As cenas deste bloco passaram a ser renderizadas três por vez, com limite de 60s e uma nova tentativa; com a máquina livre, quatro execuções seguidas verdes.

## Gauntlet

Barra: `docs/design/monarch/2-accounts-cards-summary.png` (recorte do painel Summary), R-0008.

### Rodada 1 — vitória

Juiz (A = nosso, B = Monarch): "Strong hierarchy: each group total is the largest thing in its block, so 'how much' is read before 'made of what'." — VEREDITO: A.
Crítico de design: 6,5/10 ("a disciplined, Arc-clean ledger … but the bar pair misstates scale, colour is unstable, and nothing in it is memorable"). Pedidos D1–D9.
Crítico de código (10): pedidos C1, C4–C10; C2/C3 (cor por posição, cor repetida) resolvidos pela decisão do lead: cor fixa por grupo (`groupColor(group)`), ativos nos slots de series, passivos na família de polarity.negative.
Limpos: arredondamento por maior resto, propagação de cifra, piso de 2px, SegmentedControl.
Correções em round1-corrections.md; enviadas quando abrir vaga.
Entrega da rodada 2: cor fixa por grupo, escala comum em Totais, D4–D9 e C1, C4–C10 corrigidos. C5 refutado como defeito com evidência do dev: `netWorthActions.ts:76` (pendentes à parte da janela), `:90-93` (corte/falha = erro da carga), `:87` (mês cortado só ajusta covered), `net-worth.ts:221` (hoje nunca é anulado por covered), `netWorthData.tsx:109` (saldo de hoje pela F-0020).

### Rodada 2 — vitória (aceite)

Juiz novo (A = Monarch, B = nosso): "the liabilities bar sits on the assets scale (short coloured stub, grey remainder), so 'I owe very little against what I own' is visible at once." — VEREDITO: B.
Crítico de design: 8/10, sem P0 ("Hierarchy, the shared scale, the locked/shut/clear states and the phone are showcase-grade"). P1 D10 (coluna de participações), D11 (toco), D12 (hover pisca) + D13 (legenda do trilho, do juiz), D14 (tint 60%) → fechamento.
Crítico do delta (8, sem bloqueante): C11–C18 → fechamento. Limpos: groupColor estável, soma com null, escala, "100,0%", "sem base", teste de consistência discrimina.
Aceite do lead: 2 vitórias, design 6,5 → 8. Fechamento em round2-closing.md.

### Fechamento

D10–D14 e C11–C18 corrigidos:

- D10: a lista é uma grade só (subgrid); as participações terminam numa linha (`B:556`).
- D11: o toco é uma marca de 8px, sem vão, dividida como o bloco (`W:270`, `B:616`).
- D12: sem vão entre as linhas; a placa do destaque é a caixa da linha (`W:563`, `B:710`).
- D13: legenda "2,0% dos ativos" sob a barra menor, só em Totais (`W:292`, `B:616`).
- D14: tom de `overdrawn` a 60% do laranja (`chart-theme.test.ts:79`).
- C11: cenas `-hover` e `-shut` registradas em `scripts/render-static.tsx:2225` e `:2227` e exercitadas em `B:411` (lista do teste geral, `B:556`), `B:712` e `B:743`.
- C12: `LoadError` do ponto ausente com causa e o retry da carga (`W:658`; `netWorthData.test.tsx:243`).
- C13: a escala só existe entre dois lados inteiros (`W:333`).
- C14: o destaque é derivado do que está desenhado (`W:593`).
- C15: o tom do devido mora no `chartTheme.owed` (`chart-theme.test.ts:79`).
- C16: `formatPercentFloor` em `lib/format.ts` (`format.test.ts:129`); nenhum "<" no catálogo (`W:692`).
- C17: contorno no trilho em `forced-colors` (`W:574`).
- C18: sem a tinta duplicada no teste de navegador, sem o `max-width` morto, lista de classes derivada de `CLASS_SLOTS`, nenhum resto de `groupColors`, `groups.BDR` ou `groups.CRYPTO`.
