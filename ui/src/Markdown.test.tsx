import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import Markdown from './Markdown'

const BLOCO = ['Antes do bloco.', '', '```js', 'const a = 1', 'console.log(a)', '```', '', 'Depois.'].join('\n')

const writeText = vi.fn<(text: string) => Promise<void>>()

function withClipboard(impl: ((text: string) => Promise<void>) | null) {
  Object.defineProperty(navigator, 'clipboard', {
    value: impl ? { writeText: (t: string) => writeText(t) } : undefined,
    configurable: true,
  })
  if (impl) writeText.mockImplementation(impl)
}

beforeEach(() => {
  writeText.mockReset()
  withClipboard(() => Promise.resolve())
})

afterEach(cleanup)

describe('Markdown (F-0247): botão de copiar no bloco cercado', () => {
  it('critérios 1 e 5: o bloco ganha um botão com nome acessível', () => {
    render(<Markdown>{BLOCO}</Markdown>)
    expect(screen.getByRole('button', { name: /copiar bloco de código/i })).toBeInTheDocument()
  })

  it('critério 2: copia exatamente o conteúdo do bloco, sem cerca nem rótulo de linguagem', () => {
    render(<Markdown>{BLOCO}</Markdown>)
    fireEvent.click(screen.getByRole('button', { name: /copiar bloco de código/i }))
    expect(writeText).toHaveBeenCalledWith('const a = 1\nconsole.log(a)\n')
  })

  it('critério 3: confirma a cópia e volta sozinho ao estado anterior', async () => {
    vi.useFakeTimers()
    try {
      render(<Markdown>{BLOCO}</Markdown>)
      const botao = screen.getByRole('button', { name: /copiar bloco de código/i })
      fireEvent.click(botao)
      await act(async () => {})
      expect(screen.getByText('Copiado!')).toBeInTheDocument()

      await act(async () => {
        vi.advanceTimersByTime(2000)
      })
      expect(screen.queryByText('Copiado!')).not.toBeInTheDocument()
      expect(screen.getByRole('button', { name: /copiar bloco de código/i })).toBeInTheDocument()
    } finally {
      vi.useRealTimers()
    }
  })

  it('critério 6: sem clipboard disponível não lança nem afirma sucesso', () => {
    withClipboard(null)
    render(<Markdown>{BLOCO}</Markdown>)
    expect(() => fireEvent.click(screen.getByRole('button', { name: /copiar bloco de código/i }))).not.toThrow()
    expect(screen.queryByText('Copiado!')).not.toBeInTheDocument()
  })

  it('critério 6: writeText rejeitando não lança nem afirma sucesso', async () => {
    withClipboard(() => Promise.reject(new Error('permissão negada')))
    render(<Markdown>{BLOCO}</Markdown>)
    fireEvent.click(screen.getByRole('button', { name: /copiar bloco de código/i }))
    await act(async () => {})
    expect(screen.queryByText('Copiado!')).not.toBeInTheDocument()
  })

  it('não acrescenta botão a código inline', () => {
    render(<Markdown>{'Um `trecho` inline.'}</Markdown>)
    expect(screen.queryByRole('button')).not.toBeInTheDocument()
  })
})
