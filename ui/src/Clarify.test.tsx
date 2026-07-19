import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import Markdown from './Markdown'
import { countOpen, markerSpans, parseClarification, resolveMarker } from './clarify'

afterEach(cleanup)

describe('clarify helpers', () => {
  it('parseClarification decompõe as formas do marcador', () => {
    expect(parseClarification('[ESCLARECER: qual formato?]')).toEqual({
      question: 'qual formato?',
      options: [],
      resolved: false,
      answer: '',
    })
    expect(parseClarification('[ESCLARECER: onde? | doc | sidecar]')).toEqual({
      question: 'onde?',
      options: ['doc', 'sidecar'],
      resolved: false,
      answer: '',
    })
    expect(parseClarification('[«ESCLARECER»: legado?]')?.question).toBe('legado?')
    expect(parseClarification('[ESCLARECIDO: onde? → doc]')).toEqual({
      question: 'onde?',
      options: [],
      resolved: true,
      answer: 'doc',
    })
    expect(parseClarification('[ESCLARECEDOR]')).toBeNull()
  })

  it('countOpen ignora resolvidos, comentários de template e citações em código', () => {
    const md =
      '<!-- guia [ESCLARECER: exemplo] -->\n' +
      'cita `[ESCLARECER: inline]` e\n' +
      '```\n[ESCLARECER: no fence]\n```\n' +
      '[ESCLARECER: um?] x [ESCLARECIDO: dois? → ok] y [ESCLARECER: três?]'
    expect(countOpen(md)).toBe(2)
  })

  it('markerSpans devolve os spans em ordem, com offset ancorado e citações puladas', () => {
    const md =
      '<!-- guia [ESCLARECER: exemplo] -->\n' +
      'cita `[ESCLARECER: inline]` e\n' +
      '```\n[ESCLARECER: no fence]\n```\n' +
      '[ESCLARECER: um?] x [ESCLARECIDO: dois? → ok] y [ESCLARECER: três?]'
    const spans = markerSpans(md)
    expect(spans.map((s) => s.raw)).toEqual([
      '[ESCLARECER: um?]',
      '[ESCLARECIDO: dois? → ok]',
      '[ESCLARECER: três?]',
    ])
    expect(spans.map((s) => s.resolved)).toEqual([false, true, false])
    // O offset tem de ancorar exatamente o raw — é o contrato do resolveMarker.
    for (const s of spans) {
      expect(md.slice(s.offset, s.offset + s.raw.length)).toBe(s.raw)
    }
  })

  it('resolveMarker substitui só a ocorrência ancorada no offset', () => {
    const md = '[ESCLARECER: igual?] meio [ESCLARECER: igual?]'
    const next = resolveMarker(md, '[ESCLARECER: igual?]', 26, 'resposta')
    expect(next).toBe('[ESCLARECER: igual?] meio [ESCLARECIDO: igual? → resposta]')
    // Offset que não bate com o raw → null (conteúdo mudou sob os pés).
    expect(resolveMarker(md, '[ESCLARECER: igual?]', 3, 'x')).toBeNull()
  })
})

describe('Markdown com chips de esclarecimento', () => {
  it('renderiza marcador aberto como chip e resolvido como rastro estático', () => {
    render(
      <Markdown>
        {'Prosa [ESCLARECER: qual formato?] e [ESCLARECIDO: onde? → doc].'}
      </Markdown>,
    )
    expect(screen.getByText(/qual formato\?/)).toHaveClass('clarify-open')
    expect(screen.getByText(/onde\? → doc/)).toHaveClass('clarify-resolved')
    // Sem onAnswer o chip aberto não é botão.
    expect(screen.queryByRole('button')).toBeNull()
  })

  it('não transforma marcador dentro de código', () => {
    render(<Markdown>{'`[ESCLARECER: em código]`'}</Markdown>)
    expect(screen.queryByText(/em código/)).not.toHaveClass('clarify-open')
  })

  it('clique abre popover; opções e resposta livre chamam onAnswer com o offset original', () => {
    const onAnswer = vi.fn()
    const doc = '---\ncerne_id: F-0001\n---\n\nX [ESCLARECER: onde? | doc | sidecar] fim.'
    const raw = '[ESCLARECER: onde? | doc | sidecar]'
    render(<Markdown onAnswer={onAnswer}>{doc}</Markdown>)

    fireEvent.click(screen.getByRole('button', { name: /onde\?/ }))
    // Opção proposta pelo agente vira botão (modelo AskUserQuestion).
    fireEvent.click(screen.getByRole('button', { name: 'sidecar' }))
    expect(onAnswer).toHaveBeenCalledWith(raw, doc.indexOf(raw), 'sidecar')

    // Resposta livre: textarea + confirmar.
    fireEvent.click(screen.getByRole('button', { name: /onde\?/ }))
    fireEvent.change(screen.getByPlaceholderText(/Outra resposta/), {
      target: { value: 'nenhum dos dois' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Responder' }))
    expect(onAnswer).toHaveBeenLastCalledWith(raw, doc.indexOf(raw), 'nenhum dos dois')
  })

  // F-0181: o marcador cujo corpo tem markdown inline é quebrado pelo remark em
  // vários nós do mdast. Antes, o remarkClarify casava o regex DENTRO de um nó de
  // texto e nenhum nó continha o marcador inteiro — o chip sumia enquanto o
  // countOpen (que lê a string crua) seguia contando. Beco sem saída na UI.
  const comCodigo = 'Prosa [ESCLARECER: qual provedor? cite o `backup.yaml` aqui] fim.'

  it('marcador com código inline vira chip (critério 1)', () => {
    render(<Markdown>{comCodigo}</Markdown>)
    expect(screen.getByText(/qual provedor\?/)).toHaveClass('clarify-open')
  })

  it('idem dentro de célula de tabela GFM — a repro da F-0463 (critério 2)', () => {
    const md = [
      '| Fork | Decisão |',
      '|---|---|',
      '| Provedor do destino | [ESCLARECER: B2 ou S3? Define credencial do `backup.yaml`] |',
    ].join('\n')
    render(<Markdown>{md}</Markdown>)
    expect(screen.getByText(/B2 ou S3\?/)).toHaveClass('clarify-open')
  })

  it('marcador com negrito vira chip (critério 3)', () => {
    render(<Markdown>{'X [ESCLARECER: qual **destino**?] fim.'}</Markdown>)
    expect(screen.getByText(/qual \*\*destino\*\*\?/)).toHaveClass('clarify-open')
  })

  // Caso de borda decidido na spec: o corpo do marcador não pode conter "]",
  // então um link markdown TERMINA o marcador no "]" do rótulo — a borda cai
  // dentro do nó de link, que é absorvido pelo chip. Pino do comportamento.
  it('link no corpo: marcador termina no ] do rótulo e o nó é absorvido', () => {
    render(<Markdown>{'X [ESCLARECER: ver [doc](http://x) antes] fim.'}</Markdown>)
    expect(screen.getByText(/ver \[doc/)).toHaveClass('clarify-open')
  })

  it('forma resolvida atravessando nós vira rastro estático (critério 5)', () => {
    render(<Markdown>{'X [ESCLARECIDO: cite o `backup.yaml` → B2] fim.'}</Markdown>)
    expect(screen.getByText(/backup\.yaml/)).toHaveClass('clarify-resolved')
  })

  it('responder um marcador que atravessa nós ancora raw e offset (critério 4)', () => {
    const doc = '---\ncerne_id: F-0001\n---\n\n' + comCodigo
    const raw = '[ESCLARECER: qual provedor? cite o `backup.yaml` aqui]'
    let saved = ''
    const onAnswer = (r: string, offset: number, answer: string) => {
      saved = resolveMarker(doc, r, offset, answer) ?? 'MISMATCH'
    }
    render(<Markdown onAnswer={onAnswer}>{doc}</Markdown>)
    fireEvent.click(screen.getByRole('button', { name: /qual provedor\?/ }))
    fireEvent.change(screen.getByPlaceholderText(/Escreva a resposta/), {
      target: { value: 'B2' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Responder' }))
    // O offset tem de bater com o raw no doc ORIGINAL (com frontmatter), senão
    // o resolveMarker devolve null e a resposta se perde.
    expect(saved).toContain('[ESCLARECIDO: qual provedor? cite o `backup.yaml` aqui → B2]')
    expect(saved).not.toBe('MISMATCH')
    expect(doc.indexOf(raw)).toBeGreaterThan(0)
  })

  // Critério 6 da F-0181, o que fecha a CLASSE do bug em vez de um caso: para
  // qualquer conteúdo, o que o contador enxerga e o que a página renderiza têm
  // de ser o mesmo conjunto. Enquanto forem derivações independentes, divergem.
  it('invariante: todo span contado vira chip, e nenhum chip a mais (critério 6)', () => {
    const corpus = [
      'Prosa simples sem marcador nenhum.',
      'X [ESCLARECER: simples?] fim.',
      'Prosa [ESCLARECER: um?] e [ESCLARECIDO: dois? → ok] e [ESCLARECER: três?].',
      'Com código [ESCLARECER: cite o `a.yaml` agora?] fim.',
      'Com negrito [ESCLARECER: qual **destino**?] fim.',
      '| A | B |\n|---|---|\n| c | [ESCLARECER: B2 ou S3? veja `x.yaml`] |',
      'cita `[ESCLARECER: em código]` — não conta.',
      '<!-- guia [ESCLARECER: exemplo] -->\ntexto.',
      '```\n[ESCLARECER: no fence]\n```\ntexto.',
      '[ESCLARECER: igual?] meio [ESCLARECER: igual?]',
      '---\ncerne_id: F-0001\n---\n\nCom frontmatter [ESCLARECER: cite o `y.yaml`?] fim.',
    ]
    for (const md of corpus) {
      const { container } = render(<Markdown>{md}</Markdown>)
      const chips = container.querySelectorAll('.clarify-open, .clarify-resolved')
      const abertos = container.querySelectorAll('.clarify-open')
      expect(chips.length, `total de chips em: ${md}`).toBe(markerSpans(md).length)
      expect(abertos.length, `chips abertos em: ${md}`).toBe(countOpen(md))
      cleanup()
    }
  })

  // Critério 8: a repro literal da F-0463 do Hub — a aba marcava 1 pendência e
  // a página não oferecia nada para responder.
  it('repro F-0463: badge conta 1 e existe 1 chip clicável (critério 8)', () => {
    const md = [
      '| Fork | Decisão | Razão |',
      '|---|---|---|',
      '| Provedor do destino | [ESCLARECER: B2, S3 ou outro? Define custo e credencial do `backup-externalsecret.yaml`] | escolha de custo |',
    ].join('\n')
    expect(countOpen(md)).toBe(1)
    const { container } = render(<Markdown onAnswer={() => {}}>{md}</Markdown>)
    expect(container.querySelectorAll('.clarify-open')).toHaveLength(1)
    expect(screen.getByRole('button', { name: /B2, S3 ou outro\?/ })).toBeInTheDocument()
  })

  it('round-trip: a resposta do chip produz o doc resolvido', () => {
    const doc = '---\ncerne_id: F-0001\n---\n\nX [ESCLARECER: onde?] fim.'
    let saved = ''
    const onAnswer = (raw: string, offset: number, answer: string) => {
      saved = resolveMarker(doc, raw, offset, answer) ?? 'MISMATCH'
    }
    render(<Markdown onAnswer={onAnswer}>{doc}</Markdown>)
    fireEvent.click(screen.getByRole('button', { name: /onde\?/ }))
    fireEvent.change(screen.getByPlaceholderText(/Escreva a resposta/), {
      target: { value: 'no doc' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Responder' }))
    expect(saved).toBe('---\ncerne_id: F-0001\n---\n\nX [ESCLARECIDO: onde? → no doc] fim.')
  })
})
