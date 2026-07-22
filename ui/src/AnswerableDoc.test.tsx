// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import '@testing-library/jest-dom/vitest'

import AnswerableDoc from './AnswerableDoc'

// Acento ANTES do marcador de propósito: o offset dos spans no browser é em
// code units UTF-16, mas o endpoint ancora em BYTES do arquivo — o componente
// tem de converter (F-0193).
const DOC = 'decisão pendente: [ESCLARECER: sincrona ou assincrona? | sincrona | assincrona] fim.\n'
const RAW = '[ESCLARECER: sincrona ou assincrona? | sincrona | assincrona]'

function renderDoc(onSaved = vi.fn()) {
  return render(
    <AnswerableDoc id="N-0001" docPath="content/notes/N-0001.md" content={DOC} onSaved={onSaved} />,
  )
}

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

describe('AnswerableDoc', () => {
  it('responde um chip via endpoint com offset em BYTES e refaz o fetch', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ entity: 'N-0001', answer: 'sincrona' }), { status: 200 }),
    )
    vi.stubGlobal('fetch', fetchMock)
    const onSaved = vi.fn()
    renderDoc(onSaved)

    fireEvent.click(screen.getByRole('button', { name: /sincrona ou assincrona\?/ }))
    fireEvent.click(screen.getByRole('button', { name: 'sincrona' }))

    await waitFor(() => expect(onSaved).toHaveBeenCalled())
    expect(fetchMock).toHaveBeenCalledTimes(1)
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toContain('/clarify/N-0001/answer')
    const body = JSON.parse(String(init.body)) as {
      doc: string
      offset: number
      raw: string
      answer: string
    }
    expect(body.doc).toBe('content/notes/N-0001.md')
    expect(body.raw).toBe(RAW)
    expect(body.answer).toBe('sincrona')
    // "decisão" tem um ã (2 bytes em UTF-8): offset em bytes = code units + 1.
    expect(DOC.indexOf(RAW)).toBe(18)
    expect(body.offset).toBe(19)
  })

  it('erro do endpoint vira mensagem, sem chamar onSaved', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        new Response(JSON.stringify({ error: 'clarification not found at the given anchor' }), {
          status: 409,
        }),
      ),
    )
    const onSaved = vi.fn()
    renderDoc(onSaved)

    fireEvent.click(screen.getByRole('button', { name: /sincrona ou assincrona\?/ }))
    fireEvent.click(screen.getByRole('button', { name: 'assincrona' }))

    await waitFor(() => expect(screen.getByText(/anchor/)).toBeInTheDocument())
    expect(onSaved).not.toHaveBeenCalled()
  })
})
