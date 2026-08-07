import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import BacklogChat from './BacklogChat'
import type { ChatAnswer, ChatOverride, CreateOutcome, Feature, LLMOptions } from './api'
import type { Conversation } from './useBacklogChats'

const postChat = vi.fn<(messages: unknown, override?: ChatOverride) => Promise<ChatAnswer>>()
const registerChatCandidate = vi.fn<
  (title: string, specBody: string, facets?: string[], components?: string[]) => Promise<CreateOutcome>
>()
const fetchLLMOptions = vi.fn<() => Promise<LLMOptions>>()

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  postChat: (messages: unknown, override?: ChatOverride) => postChat(messages, override),
  registerChatCandidate: (title: string, specBody: string, facets?: string[], components?: string[]) =>
    registerChatCandidate(title, specBody, facets, components),
  fetchLLMOptions: () => fetchLLMOptions(),
}))

beforeEach(() => {
  cleanup()
  postChat.mockReset()
  registerChatCandidate.mockReset()
  fetchLLMOptions.mockReset()
  fetchLLMOptions.mockResolvedValue({ mode: 'byo-key', models: [], efforts: [] })
})

const feature = (id: string): Feature => ({
  id,
  title: 'Exportar relatórios em CSV',
  module: '',
  state: 'candidate',
  created_at: '2026-08-06T00:00:00Z',
  updated_at: '2026-08-06T00:00:00Z',
})

const conversation = (patch: Partial<Conversation> = {}): Conversation => ({
  id: 'c1',
  title: '',
  updatedAt: 0,
  messages: [],
  model: '',
  effort: '',
  ...patch,
})

describe('BacklogChat (F-0218/F-0219: componente controlado)', () => {
  it('envia a mensagem e mostra a resposta do agente', async () => {
    postChat.mockResolvedValue({ answered: true, reply: 'Em qual formato?' })
    render(<BacklogChat conversation={conversation()} onUpdate={vi.fn()} />)

    fireEvent.change(screen.getByPlaceholderText(/descreva/i), {
      target: { value: 'Preciso exportar relatórios.' },
    })
    fireEvent.click(screen.getByRole('button', { name: /enviar/i }))

    await waitFor(() => expect(screen.getByText('Em qual formato?')).toBeInTheDocument())
    expect(postChat).toHaveBeenCalledWith(
      [{ role: 'user', content: 'Preciso exportar relatórios.' }],
      { model: undefined, effort: undefined },
    )
  })

  it('nasce desabilitado com orientação quando o projeto não tem [llm] configurado (critério 5)', async () => {
    fetchLLMOptions.mockResolvedValue({ mode: 'delegation', models: [], efforts: [] })
    render(<BacklogChat conversation={conversation()} onUpdate={vi.fn()} />)

    await waitFor(() => expect(screen.getByText(/configure/i)).toBeInTheDocument())
    expect(postChat).not.toHaveBeenCalled()
  })

  it('mostra desabilitado com orientação quando needs_provider no envio', async () => {
    postChat.mockResolvedValue({ answered: false, needs_provider: true })
    render(<BacklogChat conversation={conversation()} onUpdate={vi.fn()} />)

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
    render(<BacklogChat conversation={conversation()} onUpdate={vi.fn()} />)
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
    render(<BacklogChat conversation={conversation()} onUpdate={vi.fn()} onRegistered={onRegistered} />)

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

  it('F-0219: carrega o transcript/seleção da conversa recebida via prop', () => {
    render(
      <BacklogChat
        conversation={conversation({ messages: [{ role: 'user', content: 'Já dito antes' }], model: 'claude-opus-5' })}
        onUpdate={vi.fn()}
      />,
    )
    expect(screen.getByText('Já dito antes')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /nova conversa/i })).not.toBeInTheDocument()
  })

  it('F-0219: repassa toda mudança de transcript/modelo/esforço via onUpdate (quem persiste é o hook)', async () => {
    postChat.mockResolvedValue({ answered: true, reply: 'Entendido.' })
    const onUpdate = vi.fn()
    render(<BacklogChat conversation={conversation()} onUpdate={onUpdate} />)

    fireEvent.change(screen.getByPlaceholderText(/descreva/i), { target: { value: 'Preciso de X.' } })
    fireEvent.click(screen.getByRole('button', { name: /enviar/i }))
    await waitFor(() => expect(screen.getByText('Entendido.')).toBeInTheDocument())

    expect(onUpdate).toHaveBeenLastCalledWith({
      messages: [
        { role: 'user', content: 'Preciso de X.' },
        { role: 'assistant', content: 'Entendido.' },
      ],
      model: '',
      effort: '',
    })
  })

  it('critério 2: efeito de digitação completa eventualmente e um clique no histórico pula pro texto inteiro', async () => {
    const fullReply = 'Uma resposta razoavelmente longa para exercitar o efeito de digitação aos poucos.'
    postChat.mockResolvedValue({ answered: true, reply: fullReply })
    const { container } = render(<BacklogChat conversation={conversation()} onUpdate={vi.fn()} />)

    fireEvent.change(screen.getByPlaceholderText(/descreva/i), { target: { value: 'Oi' } })
    fireEvent.click(screen.getByRole('button', { name: /enviar/i }))

    // A bolha do assistente aparece antes do texto completo (digitação em curso).
    await waitFor(() => expect(container.querySelector('.backlog-chat-msg-assistant')).toBeInTheDocument())
    expect(screen.queryByText(fullReply)).not.toBeInTheDocument()

    // Clicar no histórico pula direto para o texto inteiro.
    fireEvent.click(container.querySelector('.backlog-chat-history')!)
    expect(screen.getByText(fullReply)).toBeInTheDocument()
  })

  it('critério 2: sem clique, o efeito de digitação converge pro texto completo', async () => {
    const fullReply = 'Resposta curta.'
    postChat.mockResolvedValue({ answered: true, reply: fullReply })
    render(<BacklogChat conversation={conversation()} onUpdate={vi.fn()} />)

    fireEvent.change(screen.getByPlaceholderText(/descreva/i), { target: { value: 'Oi' } })
    fireEvent.click(screen.getByRole('button', { name: /enviar/i }))

    await waitFor(() => expect(screen.getByText(fullReply)).toBeInTheDocument())
  })

  it('critério 4: Enter envia, Shift+Enter quebra linha sem enviar', async () => {
    postChat.mockResolvedValue({ answered: true, reply: 'ok' })
    render(<BacklogChat conversation={conversation()} onUpdate={vi.fn()} />)
    const textarea = screen.getByPlaceholderText(/descreva/i)

    fireEvent.change(textarea, { target: { value: 'Linha 1' } })
    fireEvent.keyDown(textarea, { key: 'Enter', shiftKey: true })
    expect(postChat).not.toHaveBeenCalled()

    fireEvent.change(textarea, { target: { value: 'Linha 1\nLinha 2' } })
    fireEvent.keyDown(textarea, { key: 'Enter', shiftKey: false })
    await waitFor(() => expect(postChat).toHaveBeenCalled())
  })

  it('critério 5: seletor de modelo/esforço envia a escolha selecionada na chamada', async () => {
    fetchLLMOptions.mockResolvedValue({
      mode: 'byo-key',
      models: ['claude-opus-5', 'claude-sonnet-5'],
      efforts: ['low', 'high'],
    })
    postChat.mockResolvedValue({ answered: true, reply: 'ok' })
    render(<BacklogChat conversation={conversation()} onUpdate={vi.fn()} />)

    await screen.findByText('claude-opus-5')
    fireEvent.change(screen.getByLabelText(/modelo/i), { target: { value: 'claude-opus-5' } })
    fireEvent.change(screen.getByLabelText(/esfor/i), { target: { value: 'high' } })

    fireEvent.change(screen.getByPlaceholderText(/descreva/i), { target: { value: 'Oi' } })
    fireEvent.click(screen.getByRole('button', { name: /enviar/i }))

    await waitFor(() =>
      expect(postChat).toHaveBeenCalledWith(
        [{ role: 'user', content: 'Oi' }],
        { model: 'claude-opus-5', effort: 'high' },
      ),
    )
  })

  it('F-0219: a conversa recebida com modelo/esforço já salvos pré-seleciona o seletor', async () => {
    fetchLLMOptions.mockResolvedValue({
      mode: 'byo-key',
      models: ['claude-opus-5', 'claude-sonnet-5'],
      efforts: ['low', 'high'],
    })
    render(<BacklogChat conversation={conversation({ model: 'claude-opus-5', effort: 'high' })} onUpdate={vi.fn()} />)

    await screen.findByText('claude-opus-5')
    expect(screen.getByLabelText(/modelo/i)).toHaveValue('claude-opus-5')
    expect(screen.getByLabelText(/esfor/i)).toHaveValue('high')
  })
})
