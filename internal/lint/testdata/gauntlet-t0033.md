---
cerne_id: T-0033
---

# Cartões de crédito: total devido, variação no mês e sparkline por cartão

**Spec:** [T-0033](./spec.md) · **Épico:** E-0003
**Data:** 2026-10-03
**Agente:** claude-opus-5-5 · esforço desconhecido

## Resultado

Entregue. O widget `credit-cards` está no catálogo da Visão Geral (oculto por padrão, uma coluna): o total devido hoje e a variação de um mês no cabeçalho, e um registro de duas linhas por cartão — nome, sparkline em degrau do mês e valor; vencimento da próxima fatura não paga e, só quando há parcela a vir, a composição. Cada registro leva ao extrato do cartão. Venceu o Monarch nas duas rodadas do Gauntlet; o crítico de design foi de 6,5 a 8/10.

Portões no fechamento (branch `claude/e0003-t0033`): `tsc --noEmit` limpo; `vitest run` 230 arquivos, 3495 testes; `vitest run browser` 6 arquivos, 106 testes; `npm run build` ok; guards do hub 14/14; detector do impeccable sem achados nos arquivos do widget.

## Aceitação

Os testes citados estão em `web/app/_components/dashboard/CreditCardsWidget.test.tsx`, salvo indicação.

- **critério 1 — ATENDIDO, com desvio aceito.** Entram todos os cartões (`type === "CREDIT"`), do maior valor devido hoje para o menor. Não existe conta inativa no modelo (nem `AccountResponse` nem `NetWorthAccount` têm `active`), então o filtro da spec deixa passar todos e o caso "inativo sem dívida fora" não é exercitável. Evidência: "todos os cartões, do maior valor devido para o menor…" (`:155`) e "dois cartões que devem o mesmo saem pela ordem do id…" (`:164`).
- **critério 2 — ATENDIDO, com desvio aceito.** O total por cartão e a composição "R$ X na fatura · R$ Y parcelado" quando há parcela a vir (as duas partes no painel largo; "inclui R$ Y parcelado" no estreito; nada quando tudo está na fatura). A sparkline tem um ponto por dia de "1 mês", na escala do próprio cartão, sem eixo, com o ponto de hoje. Evidência: "o valor se abre em fatura e parcelado…" (`:182`), "um estorno abate a fatura…" (`:196`), "a sparkline: um ponto por dia do último mês…" (`:216`), "a linha é um degrau…" (`:248`), "'1 mês' começa no mesmo dia do mês anterior…" (`:172`, com 29/02 de ano bissexto).
- **critério 3 — ATENDIDO.** Variação = devido hoje − devido no mesmo dia do mês anterior; subir é o tom negativo, descer o positivo, só no valor; início ≤ 0 é "sem base". Evidência: os três testes de "o cabeçalho" (`:121`, `:136`, `:144`).
- **critério 4 — ATENDIDO, com desvio aceito.** "Vence 10/10" pela próxima fatura não paga, "Sem fatura aberta" sem nenhuma, e a pílula de atraso do `PaymentStatusCell` quando venceu. O vencimento é o menor `dueDate` das compras não pagas, não o cálculo de `credit-cycle.ts`. Evidência: "o vencimento da próxima fatura não paga…" (`:305`), "os limites da semana…" (`:324`), "uma fatura paga não é a próxima…" (`:339`), "cartão sem ciclo…" (`:344`).
- **critério 5 — ATENDIDO.** Cartão sem compra: R$ 0,00, sem sparkline e sem composição; sem cartão, o `EmptyState` leva a `/accounts`. Evidência: "cartão sem nenhuma compra…" (`:350`) e "sem cartão, depois de uma carga ok: o estado vazio leva às contas" (`:497`).
- **critério 6 — ATENDIDO na metade desta task.** O total do cabeçalho é o passivo `cards` do ponto de hoje (`netWorthSeries`), o número que o Patrimônio líquido desconta e que o T-0032 lista. Evidência: `dashboard-consistency.test.tsx:98` e `:116`, com o mock respondendo por escopo. O widget do T-0032 não está neste branch; ele acrescenta o seu `describe` ao mesmo arquivo.

Do épico:

- **E-0003 critério 1 e critério 2.** No catálogo, `defaultVisible: false`, span 1, e acrescentado oculto a um layout já salvo: "está no catálogo, fora da tela até o usuário pedir…" (`:505`) e `DashboardClient.layout.test.tsx:519`.
- **E-0003 critério 9.** Total e variação no cabeçalho, uma linha por cartão com nome, sparkline, valor, composição e vencimento, link para o extrato: os testes dos critérios 1 a 4 acima.
- **E-0003 critério 10.** `dashboard-consistency.test.tsx:98`.
- **E-0003 critério 11.** Cifra: `Locked` na linha e no total, nunca soma parcial (`:402`, `:430`); janela cortada: dias "não lido", sem cadeado (`:458`); carregando e erro com retry (`:480`, `:488`); vazio só com carga ok (`:497`).
- **E-0003 critério 12.** O widget pede só o escopo `"12m"` da carga compartilhada (`:121`; `dashboard-consistency.test.tsx:98`).
- **E-0003 critério 13.** Guardas `design-tokens`, `icons`, `figures`, `load-states`, `click-feedback`, `primitives` e `shared-css` verdes, sem entrada nova nas listas; sem `linearGradient` (`:216`).
- **E-0003 critério 14.** `dashboard.browser.test.ts:523`, 13 casos: 375px, os dois lados de cada troca do painel (352/353px e 480/481px), 900px, cifrado e nome longo — nada passa da tela nem do painel, nada encosta em nada.
- **E-0003 critério 15.** Sparkline `aria-hidden` e a tabela `srOnly` com todos os pontos, fora dos links: "os números que as sparklines desenham estão numa tabela…" (`:293`).
- **E-0003 critério 16.** Chaves `Finance.dashboard.creditCards.*` nos dois idiomas (`:517`, `lib/messages-parity.test.ts`); figuras por `lib/format.ts` (`figures.guard.test.ts`).
- **E-0003 critério 17.** Cenas `dashboard-credit-cards`, `-wide`, `-locked`, `-empty` e `-long` em `web/scripts/render-static.tsx`.

## O que mudou

Em `web/`:

- **Novos:** `app/_components/dashboard/CreditCardsWidget.tsx`, `_credit-cards.module.css`, `CreditCardsWidget.test.tsx`, `dashboard-consistency.test.tsx`; `lib/due-soon.ts` e `lib/due-soon.test.ts`.
- **Blocos acrescentados:** `widgetRegistry.ts` (entrada `credit-cards`), `DashboardClient.tsx` (ramo em `renderWidget`), `DashboardClient.layout.test.tsx`, `dashboard.browser.test.ts`, `scripts/render-static.tsx` (cinco cenas), `messages/pt-BR.json` e `en.json`.
- **Primitiva compartilhada:** `PaymentStatusCell.tsx` usa `isSoon` de `lib/due-soon.ts`, deriva "atrasada" da mesma distância que a pílula lê e só reexporta `SOON_DAYS`; teste dos limites −1/0/6/7 em `PaymentStatusCell.test.tsx:207`.

O contrato de `lib/net-worth.ts` e de `netWorthData.tsx` não foi tocado.

## Débitos e follow-ups

- **Adotar o `LockedHatch` compartilhado** quando a T-0030 o extrair e integrar: é o passo de integração a seguir. Hoje a sparkline desenha a mesma hachura (45°, passo de 6px, traço de 1px, `--border-hover`) no próprio arquivo.
- **Variação por cartão em número:** sugestão do juiz, não adotada. A segunda linha do registro já leva a exceção da composição, e a sparkline em degrau responde "subiu?" por cartão.
- **Metade do T-0032 no critério 6:** o `describe` de Ativos e passivos entra em `dashboard-consistency.test.tsx` quando aquele widget chegar ao branch do épico.

## Aprendizados e desvios

Desvios da spec, aceitos pelo lead:

- **Todos os cartões CREDIT entram** (critério 1): não existe conta inativa.
- **O vencimento é o menor `dueDate` das compras não pagas**, não o `credit-cycle` (critério 4): a API já grava em cada linha o vencimento da sua fatura, com os ajustes de data do usuário, que a carga não traz. É o dado que o A pagar usa.
- **`SOON_DAYS` e `isSoon` em `lib/due-soon.ts`:** o widget lê a regra da semana sem trazer a célula de status, o Modal e as actions para a Visão Geral.
- **Pílula âmbar na semana do vencimento**, além da vermelha de atraso que a spec pede.
- **Três caixas de sparkline por container query** (72×24, 96×28, 216×32): cada uma é desenhada no seu tamanho e o painel mostra a que cabe. Uma só, escalada, afinaria o traço e mudaria o ponto e o passo da hachura com a tela.
- **Composição só na exceção:** "tudo na fatura" dizia o estado normal em voz alta.
- **Escala com `MIN_SPAN` 0,5:** cada cartão tem a sua escala e nada na tela diz qual; um desvio de 10% não pode encher a caixa como uma dívida que dobrou.
- **Degrau:** a dívida vale do fecho de um dia ao do seguinte e muda a prumo; a rampa desenhava valores que nenhum dia teve.

Não adotado, por medida: "Atrasada desde 15/09" no lugar de "Venceu 15/09". A 375px a pílula teria 148px numa coluna de 128px, e um cartão em atraso com parcelas pediria 329px dos 309px da linha.

Aprendizados:

- A dívida de hoje é inteira mesmo com a janela cortada: `netWorthActions.ts:76` lê toda linha pendente sem janela, `:77` as pagas com data futura, `:92` falha (`tooManyRows`) se uma dessas leituras for cortada, e `netWorthData.tsx:111` as junta. Só os dias anteriores a `covered` ficam sem número.
- Um ponto de troca de layout precisa ser medido dos dois lados. Com a troca em 20rem, o painel logo acima dela dava ao nome uma coluna menor que a do celular; a troca foi para 22rem e o teste de navegador mede 352/353px e 480/481px.
- Esperar pelo nome cifrado para mostrar os valores era um carregando sem saída quando a chave não responde. Os valores saem sempre; o nome tem três estados (abrindo, sem chave, falhou).

## Gauntlet

Barra: `docs/design/monarch/2-accounts-cards-summary.png` (recorte do grupo "Credit Cards"), R-0008.

- **Rodada 1 — vitória.** Juiz (A = Monarch, B = nosso): "clear three-level hierarchy: a hero total, a signed delta with its period in words, then the per-card rows … It adds what a cardholder acts on: the due date, an overdue pill only on the exception" — veredito: B. Crítico de design: 6,5/10 ("disciplined, honest and Arc-clean, with a strong locked state. But the rows have no steady rhythm, the wide layout is uncomposed and the sparkline is timid"). Disposição: D1–D11 corrigidos; C1–C10 corrigidos, com o C1 (dívida parcial com leitura truncada) refutado como defeito pela evidência do loader (`netWorthActions.ts:76`, `:77`, `:92`; `netWorthData.tsx:111`) e resolvido como mensagem ("não lido", sem cadeado). Desvios aceitos: todos os CREDIT; vencimento pelo menor `dueDate`; `SOON_DAYS` em `lib/due-soon.ts`; pílula âmbar na semana.
- **Rodada 2 — vitória (aceite).** Juiz novo (A = nosso, B = Monarch): "Strong hierarchy: the total is the largest figure, the change sits right under it, and per-card amounts are bold and right-aligned in one column." — veredito: A. Crítico de design: 8/10, sem P0 ("ship as a showcase once the two P1s are in … The default state is already showcase-grade"). Disposição: D12–D16 e C11–C16 corrigidos no fechamento (D12 nome numa linha com reticências e `title`; D13 hachura dentro da grade; D14 caixa larga de 216×32; D15 cartão zerado sem fio; D16 linha em degrau; C11 atraso derivado da distância e limites testados na célula; C12 três estados do nome; C13 teste renomeado; C14 pontos de troca medidos; C15 `toHaveClass` e cor medida; C16 `unread` num lugar só). Limpos no crítico do delta: DOM duplicado (`display: none`, `aria-hidden`), ordenação com nulos, aritmética. Sugestão do juiz não adotada: variação por cartão em número.
- **Aceite do lead:** duas vitórias, design 6,5 → 8.
