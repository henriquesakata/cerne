import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import QuoteComment, { QUOTE_MAX_CHARS, buildQuote, elideSnippet } from './QuoteComment'

beforeEach(() => {
  cleanup()
  window.getSelection()?.removeAllRanges()
})

// selectDom monta uma seleção real no jsdom entre dois nós de texto — é assim
// que o usuário atravessa nós inline (<strong>, <code>, <a>).
function selectDom(start: Node, startOffset: number, end: Node, endOffset: number) {
  const range = document.createRange()
  range.setStart(start, startOffset)
  range.setEnd(end, endOffset)
  const sel = window.getSelection()!
  sel.removeAllRanges()
  sel.addRange(range)
}

function selectInTextarea(el: HTMLTextAreaElement, start: number, end: number) {
  el.focus()
  el.setSelectionRange(start, end)
}

describe('elideSnippet (F-0250 critério 6)', () => {
  it('mantém trecho curto intacto, normalizando espaço', () => {
    expect(elideSnippet('  o critério   5\nmistura duas coisas ')).toBe('o critério 5 mistura duas coisas')
  })

  it('elide o meio preservando as duas pontas', () => {
    const long = 'INICIO' + 'x'.repeat(QUOTE_MAX_CHARS * 2) + 'FIM'
    const out = elideSnippet(long)
    expect(out.length).toBeLessThanOrEqual(QUOTE_MAX_CHARS)
    expect(out.startsWith('INICIO')).toBe(true)
    expect(out.endsWith('FIM')).toBe(true)
    expect(out).toContain('…')
  })
})

describe('buildQuote (F-0250 critério 2)', () => {
  it('cita o trecho como blockquote e emenda o comentário', () => {
    expect(buildQuote('o critério 5', 'mistura duas coisas  ')).toBe(
      '> o critério 5\n\nmistura duas coisas\n\n',
    )
  })
})

describe('QuoteComment (F-0250)', () => {
  it('critério 1/2: seleção no card abre o popover e confirmar injeta citação+comentário', () => {
    const onQuote = vi.fn()
    render(
      <QuoteComment onQuote={onQuote}>
        <p>o critério 5 mistura duas coisas</p>
      </QuoteComment>,
    )
    const p = screen.getByText(/mistura duas coisas/)
    expect(screen.queryByRole('button', { name: /comentar/i })).not.toBeInTheDocument()

    selectDom(p.firstChild!, 0, p.firstChild!, 12)
    fireEvent.mouseUp(p)

    const input = screen.getByPlaceholderText(/ajustar/i)
    fireEvent.change(input, { target: { value: 'separar em dois critérios' } })
    fireEvent.click(screen.getByRole('button', { name: /^comentar$/i }))

    expect(onQuote).toHaveBeenCalledWith('> o critério 5\n\nseparar em dois critérios\n\n')
    expect(screen.queryByPlaceholderText(/ajustar/i)).not.toBeInTheDocument()
  })

  it('critério 5: seleção atravessando nós inline cita o texto visível, sem markup', () => {
    const onQuote = vi.fn()
    const { container } = render(
      <QuoteComment onQuote={onQuote}>
        <p>
          o critério <strong>5</strong> mistura
        </p>
      </QuoteComment>,
    )
    const p = container.querySelector('p')!
    selectDom(p.firstChild!, 0, p.lastChild!, 8)
    fireEvent.mouseUp(p)

    fireEvent.change(screen.getByPlaceholderText(/ajustar/i), { target: { value: 'ajuste' } })
    fireEvent.click(screen.getByRole('button', { name: /^comentar$/i }))

    expect(onQuote).toHaveBeenCalledWith('> o critério 5 mistura\n\najuste\n\n')
  })

  it('critério 1: seleção dentro de textarea do card também abre o popover', () => {
    const onQuote = vi.fn()
    render(
      <QuoteComment onQuote={onQuote}>
        <textarea defaultValue={'## Problema\nFalta exportação.'} />
      </QuoteComment>,
    )
    const ta = screen.getByRole('textbox') as HTMLTextAreaElement
    selectInTextarea(ta, 12, 30)
    fireEvent.mouseUp(ta)

    fireEvent.change(screen.getByPlaceholderText(/ajustar/i), { target: { value: 'vago' } })
    fireEvent.click(screen.getByRole('button', { name: /^comentar$/i }))

    expect(onQuote).toHaveBeenCalledWith('> Falta exportação.\n\nvago\n\n')
  })

  it('critério 9: Esc descarta o popover sem tocar no compositor', () => {
    const onQuote = vi.fn()
    render(
      <QuoteComment onQuote={onQuote}>
        <p>o critério 5 mistura duas coisas</p>
      </QuoteComment>,
    )
    const p = screen.getByText(/mistura duas coisas/)
    selectDom(p.firstChild!, 0, p.firstChild!, 12)
    fireEvent.mouseUp(p)
    expect(screen.getByPlaceholderText(/ajustar/i)).toBeInTheDocument()

    fireEvent.keyDown(document, { key: 'Escape' })

    expect(screen.queryByPlaceholderText(/ajustar/i)).not.toBeInTheDocument()
    expect(onQuote).not.toHaveBeenCalled()
  })

  it('critério 9: clique fora descarta o popover sem chamar onQuote', () => {
    const onQuote = vi.fn()
    render(
      <QuoteComment onQuote={onQuote}>
        <p>o critério 5 mistura duas coisas</p>
      </QuoteComment>,
    )
    const p = screen.getByText(/mistura duas coisas/)
    selectDom(p.firstChild!, 0, p.firstChild!, 12)
    fireEvent.mouseUp(p)

    fireEvent.mouseDown(document.body)

    expect(screen.queryByPlaceholderText(/ajustar/i)).not.toBeInTheDocument()
    expect(onQuote).not.toHaveBeenCalled()
  })

  it('seleção vazia (clique simples) não abre popover', () => {
    render(
      <QuoteComment onQuote={vi.fn()}>
        <p>o critério 5 mistura duas coisas</p>
      </QuoteComment>,
    )
    fireEvent.mouseUp(screen.getByText(/mistura duas coisas/))
    expect(screen.queryByPlaceholderText(/ajustar/i)).not.toBeInTheDocument()
  })

  it('critério 3: dois comentários acumulam no compositor, na ordem em que foram criados', () => {
    const quotes: string[] = []
    render(
      <QuoteComment onQuote={(q) => quotes.push(q)}>
        <p>primeiro trecho e segundo trecho</p>
      </QuoteComment>,
    )
    const p = screen.getByText(/primeiro trecho/)

    selectDom(p.firstChild!, 0, p.firstChild!, 16)
    fireEvent.mouseUp(p)
    fireEvent.change(screen.getByPlaceholderText(/ajustar/i), { target: { value: 'um' } })
    fireEvent.click(screen.getByRole('button', { name: /^comentar$/i }))

    selectDom(p.firstChild!, 18, p.firstChild!, 32)
    fireEvent.mouseUp(p)
    fireEvent.change(screen.getByPlaceholderText(/ajustar/i), { target: { value: 'dois' } })
    fireEvent.click(screen.getByRole('button', { name: /^comentar$/i }))

    expect(quotes).toEqual(['> primeiro trecho\n\num\n\n', '> segundo trecho\n\ndois\n\n'])
  })

  it('comentário vazio não confirma — o botão fica desabilitado', () => {
    const onQuote = vi.fn()
    render(
      <QuoteComment onQuote={onQuote}>
        <p>o critério 5 mistura duas coisas</p>
      </QuoteComment>,
    )
    const p = screen.getByText(/mistura duas coisas/)
    selectDom(p.firstChild!, 0, p.firstChild!, 12)
    fireEvent.mouseUp(p)

    const confirm = screen.getByRole('button', { name: /^comentar$/i })
    expect(confirm).toBeDisabled()
    fireEvent.click(confirm)
    expect(onQuote).not.toHaveBeenCalled()
  })
})
