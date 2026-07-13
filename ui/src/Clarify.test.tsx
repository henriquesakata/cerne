import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import Markdown from './Markdown'
import { countOpen, parseClarification, resolveMarker } from './clarify'

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
