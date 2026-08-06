import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import BacklogChat from './BacklogChat'
import type { ChatAnswer, CreateOutcome, Feature, LLMMode } from './api'

const postChat = vi.fn<(messages: unknown) => Promise<ChatAnswer>>()
const registerChatCandidate = vi.fn<
  (title: string, specBody: string, facets?: string[], components?: string[]) => Promise<CreateOutcome>
>()
const fetchLLMMode = vi.fn<() => Promise<LLMMode>>()

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  postChat: (messages: unknown) => postChat(messages),
  registerChatCandidate: (title: string, specBody: string, facets?: string[], components?: string[]) =>
    registerChatCandidate(title, specBody, facets, components),
  fetchLLMMode: () => fetchLLMMode(),
}))

beforeEach(() => {
  cleanup()
  postChat.mockReset()
  registerChatCandidate.mockReset()
  fetchLLMMode.mockReset()
  fetchLLMMode.mockResolvedValue('byo-key')
})

const feature = (id: string): Feature => ({
  id,
  title: 'Exportar relatórios em CSV',
  module: '',
  state: 'candidate',
  created_at: '2026-08-06T00:00:00Z',
  updated_at: '2026-08-06T00:00:00Z',
})

describe('BacklogChat (F-0210)', () => {
  it('envia a mensagem e mostra a resposta do agente', async () => {
    postChat.mockResolvedValue({ answered: true, reply: 'Em qual formato?' })
    render(<BacklogChat />)

    fireEvent.change(screen.getByPlaceholderText(/descreva/i), {
      target: { value: 'Preciso exportar relatórios.' },
    })
    fireEvent.click(screen.getByRole('button', { name: /enviar/i }))

    await waitFor(() => expect(screen.getByText('Em qual formato?')).toBeInTheDocument())
    expect(postChat).toHaveBeenCalledWith([
      { role: 'user', content: 'Preciso exportar relatórios.' },
    ])
  })

  it('nasce desabilitado com orientação quando o projeto não tem [llm] configurado (critério 5)', async () => {
    fetchLLMMode.mockResolvedValue('delegation')
    render(<BacklogChat />)

    await waitFor(() => expect(screen.getByText(/configure/i)).toBeInTheDocument())
    expect(postChat).not.toHaveBeenCalled()
  })

  it('mostra desabilitado com orientação quando needs_provider no envio', async () => {
    postChat.mockResolvedValue({ answered: false, needs_provider: true })
    render(<BacklogChat />)

    fireEvent.change(screen.getByPlaceholderText(/descreva/i), {
      target: { value: 'Oi' },
    })
    fireEvent.click(screen.getByRole('button', { name: /enviar/i }))

    await waitFor(() =>
      expect(screen.getByText(/configure/i)).toBeInTheDocument(),
    )
  })

  it('sem bloco de proposta, não mostra botão de registrar', async () => {
    postChat.mockResolvedValue({ answered: true, reply: 'Me conta mais sobre o formato.' })
    render(<BacklogChat />)
    fireEvent.change(screen.getByPlaceholderText(/descreva/i), { target: { value: 'Oi' } })
    fireEvent.click(screen.getByRole('button', { name: /enviar/i }))

    await waitFor(() => expect(screen.getByText('Me conta mais sobre o formato.')).toBeInTheDocument())
    expect(screen.queryByRole('button', { name: /registrar candidata/i })).not.toBeInTheDocument()
  })

  it('com bloco de proposta, mostra título/corpo editáveis e só registra ao confirmar', async () => {
    const proposal =
      'Perfeito, aqui está a proposta:\n\n' +
      '```cerne-proposta\n' +
      '# Exportar relatórios em CSV\n\n' +
      '## Problema\nFalta exportação.\n' +
      '```\n'
    postChat.mockResolvedValue({ answered: true, reply: proposal })
    registerChatCandidate.mockResolvedValue({ ok: true, feature: feature('F-0099') })
    const onRegistered = vi.fn()
    render(<BacklogChat onRegistered={onRegistered} />)

    fireEvent.change(screen.getByPlaceholderText(/descreva/i), { target: { value: 'Oi' } })
    fireEvent.click(screen.getByRole('button', { name: /enviar/i }))

    const registerButton = await screen.findByRole('button', { name: /registrar candidata/i })
    // Confirmation is explicit: registering must not have happened just from the reply arriving.
    expect(registerChatCandidate).not.toHaveBeenCalled()

    expect(screen.getByDisplayValue('Exportar relatórios em CSV')).toBeInTheDocument()
    fireEvent.click(registerButton)

    await waitFor(() =>
      expect(registerChatCandidate).toHaveBeenCalledWith(
        'Exportar relatórios em CSV',
        expect.stringContaining('## Problema'),
        [],
        [],
      ),
    )
    await waitFor(() => expect(onRegistered).toHaveBeenCalledWith(feature('F-0099')))
  })
})
