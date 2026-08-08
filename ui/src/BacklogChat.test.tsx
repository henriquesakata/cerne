import { beforeEach, describe, expect, it, vi } from 'vitest'
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import BacklogChat from './BacklogChat'
import { setApiProject, type ChatAnswer, type ChatOverride, type CreateOutcome, type Feature, type LLMOptions } from './api'
import { getConversation, writeChats, type Conversation } from './useBacklogChats'

const streamChat = vi.fn<
  (messages: unknown, override: ChatOverride | undefined, onDelta: (d: string) => void) => Promise<ChatAnswer>
>()
const registerChatCandidate = vi.fn<
  (title: string, specBody: string, facets?: string[], components?: string[], capability?: string) => Promise<CreateOutcome>
>()
const fetchLLMOptions = vi.fn<() => Promise<LLMOptions>>()

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  streamChat: (messages: unknown, override: ChatOverride | undefined, onDelta: (d: string) => void) =>
    streamChat(messages, override, onDelta),
  registerChatCandidate: (
    title: string,
    specBody: string,
    facets?: string[],
    components?: string[],
    capability?: string,
  ) => registerChatCandidate(title, specBody, facets, components, capability),
  fetchLLMOptions: () => fetchLLMOptions(),
}))

let nextConvId = 0

beforeEach(() => {
  cleanup()
  localStorage.clear()
  setApiProject('proj-x')
  streamChat.mockReset()
  registerChatCandidate.mockReset()
  fetchLLMOptions.mockReset()
  fetchLLMOptions.mockResolvedValue({ mode: 'byo-key', models: [], efforts: [] })
  nextConvId += 1
})

// renderChat (F-0230): chatRequest.send grava a resposta direto no storage
// da lista de conversas — precisa existir lá antes do envio, exatamente como
// acontece de verdade (useBacklogChats já a persistiu antes do fetch
// resolver). Cada teste ganha um id novo pra não herdar status pendente de
// um teste anterior (o singleton do chatRequest é module-scoped).
function renderChat(patch: Partial<Conversation> = {}, props: { onUpdate?: (p: unknown) => void; onRegistered?: (f: Feature) => void } = {}) {
  const conversation: Conversation = {
    id: `c${nextConvId}`,
    title: '',
    updatedAt: 0,
    messages: [],
    model: '',
    effort: '',
    ...patch,
  }
  writeChats([conversation], conversation.id)
  const onUpdate = props.onUpdate ?? vi.fn()
  const utils = render(<BacklogChat conversation={conversation} onUpdate={onUpdate} onRegistered={props.onRegistered} />)
  return { ...utils, conversation, onUpdate }
}

const feature = (id: string): Feature => ({
  id,
  title: 'Exportar relatórios em CSV',
  module: '',
  state: 'candidate',
  created_at: '2026-08-06T00:00:00Z',
  updated_at: '2026-08-06T00:00:00Z',
})

describe('BacklogChat (F-0218/F-0219: componente controlado)', () => {
  it('envia a mensagem e mostra a resposta do agente', async () => {
    streamChat.mockResolvedValue({ answered: true, reply: 'Em qual formato?' })
    renderChat()

    fireEvent.change(screen.getByPlaceholderText(/descreva/i), {
      target: { value: 'Preciso exportar relatórios.' },
    })
    fireEvent.click(screen.getByRole('button', { name: /enviar/i }))

    await waitFor(() => expect(screen.getByText('Em qual formato?')).toBeInTheDocument())
    expect(streamChat).toHaveBeenCalledWith(
      [{ role: 'user', content: 'Preciso exportar relatórios.' }],
      { model: undefined, effort: undefined },
      expect.any(Function),
    )
  })

  it('nasce desabilitado com orientação quando o projeto não tem [llm] configurado (critério 5)', async () => {
    fetchLLMOptions.mockResolvedValue({ mode: 'delegation', models: [], efforts: [] })
    renderChat()

    await waitFor(() => expect(screen.getByText(/configure/i)).toBeInTheDocument())
    expect(streamChat).not.toHaveBeenCalled()
  })

  it('mostra desabilitado com orientação quando needs_provider no envio', async () => {
    streamChat.mockResolvedValue({ answered: false, needs_provider: true })
    renderChat()

    fireEvent.change(screen.getByPlaceholderText(/descreva/i), {
      target: { value: 'Oi' },
    })
    fireEvent.click(screen.getByRole('button', { name: /enviar/i }))

    await waitFor(() =>
      expect(screen.getByText(/configure/i)).toBeInTheDocument(),
    )
  })

  it('sem bloco de proposta, não mostra botão de registrar', async () => {
    streamChat.mockResolvedValue({ answered: true, reply: 'Me conta mais sobre o formato.' })
    renderChat()
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
    streamChat.mockResolvedValue({ answered: true, reply: proposal })
    registerChatCandidate.mockResolvedValue({ ok: true, feature: feature('F-0099') })
    const onRegistered = vi.fn()
    renderChat({}, { onRegistered })

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
        '',
      ),
    )
    await waitFor(() => expect(onRegistered).toHaveBeenCalledWith(feature('F-0099')))
  })

  it('F-0223: proposta com facet/components/capability mostra chips e registra as tags', async () => {
    const proposal =
      'Perfeito, aqui está a proposta:\n\n' +
      '```cerne-proposta\n' +
      '# Exportar relatórios em CSV\n' +
      'facet: code\n' +
      'components: chat, backlog\n' +
      'capability: C-0001\n\n' +
      '## Problema\nFalta exportação.\n' +
      '```\n'
    streamChat.mockResolvedValue({ answered: true, reply: proposal })
    registerChatCandidate.mockResolvedValue({ ok: true, feature: feature('F-0099') })
    renderChat()

    fireEvent.change(screen.getByPlaceholderText(/descreva/i), { target: { value: 'Oi' } })
    fireEvent.click(screen.getByRole('button', { name: /enviar/i }))

    const registerButton = await screen.findByRole('button', { name: /registrar candidata/i })
    expect(screen.getByText('code')).toBeInTheDocument()
    expect(screen.getByText('chat')).toBeInTheDocument()
    expect(screen.getByText('backlog')).toBeInTheDocument()
    expect(screen.getByText('C-0001')).toBeInTheDocument()

    fireEvent.click(registerButton)

    await waitFor(() =>
      expect(registerChatCandidate).toHaveBeenCalledWith(
        'Exportar relatórios em CSV',
        expect.stringContaining('## Problema'),
        ['code'],
        ['chat', 'backlog'],
        'C-0001',
      ),
    )
  })

  it('F-0219: carrega o transcript/seleção da conversa recebida via prop', () => {
    renderChat({ messages: [{ role: 'user', content: 'Já dito antes' }], model: 'claude-opus-5' })
    expect(screen.getByText('Já dito antes')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /nova conversa/i })).not.toBeInTheDocument()
  })

  it('F-0219: repassa toda mudança de transcript/modelo/esforço via onUpdate (quem persiste é o hook)', async () => {
    streamChat.mockResolvedValue({ answered: true, reply: 'Entendido.' })
    const { onUpdate } = renderChat()

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

  it('F-0240 critério 1: o texto parcial aparece na tela conforme os deltas chegam, antes do terminal', async () => {
    let onDelta!: (d: string) => void
    streamChat.mockImplementation((_m, _o, cb) => {
      onDelta = cb
      return new Promise(() => {}) // transmissão segue em voo
    })
    renderChat()

    fireEvent.change(screen.getByPlaceholderText(/descreva/i), { target: { value: 'Oi' } })
    fireEvent.click(screen.getByRole('button', { name: /enviar/i }))

    await waitFor(() => expect(streamChat).toHaveBeenCalled())
    act(() => onDelta('Começo da resposta'))
    await waitFor(() => expect(screen.getByText('Começo da resposta')).toBeInTheDocument())

    act(() => onDelta(' e o resto dela.'))
    await waitFor(() => expect(screen.getByText('Começo da resposta e o resto dela.')).toBeInTheDocument())
    // Ainda pendente: o indicador de processamento segue visível junto do parcial.
    expect(screen.getByText(/processando/i)).toBeInTheDocument()
  })

  it('F-0240 critério 7: proposta transmitida em N pedaços só é parseada no fim, com metadados', async () => {
    const proposal =
      'Perfeito, aqui está a proposta:\n\n' +
      '```cerne-proposta\n' +
      '# Exportar relatórios em CSV\n' +
      'facet: code\n' +
      'components: chat, backlog\n' +
      'capability: C-0001\n\n' +
      '## Problema\nFalta exportação.\n' +
      '```\n'
    const chunks = [proposal.slice(0, 40), proposal.slice(40, 90), proposal.slice(90)]
    let onDelta!: (d: string) => void
    let resolve!: (v: ChatAnswer) => void
    streamChat.mockImplementation((_m, _o, cb) => {
      onDelta = cb
      return new Promise((r) => (resolve = r))
    })
    renderChat()

    fireEvent.change(screen.getByPlaceholderText(/descreva/i), { target: { value: 'Oi' } })
    fireEvent.click(screen.getByRole('button', { name: /enviar/i }))
    await waitFor(() => expect(streamChat).toHaveBeenCalled())

    act(() => onDelta(chunks[0]))
    // Bloco incompleto: nada de botão de registrar durante a transmissão.
    expect(screen.queryByRole('button', { name: /registrar candidata/i })).not.toBeInTheDocument()

    act(() => {
      onDelta(chunks[1])
      onDelta(chunks[2])
    })
    act(() => resolve({ answered: true, reply: proposal }))

    await screen.findByRole('button', { name: /registrar candidata/i })
    expect(screen.getByDisplayValue('Exportar relatórios em CSV')).toBeInTheDocument()
    expect(screen.getByText('code')).toBeInTheDocument()
    expect(screen.getByText('chat')).toBeInTheDocument()
    expect(screen.getByText('backlog')).toBeInTheDocument()
    expect(screen.getByText('C-0001')).toBeInTheDocument()
  })

  it('critério 4: Enter envia, Shift+Enter quebra linha sem enviar', async () => {
    streamChat.mockResolvedValue({ answered: true, reply: 'ok' })
    renderChat()
    const textarea = screen.getByPlaceholderText(/descreva/i)

    fireEvent.change(textarea, { target: { value: 'Linha 1' } })
    fireEvent.keyDown(textarea, { key: 'Enter', shiftKey: true })
    expect(streamChat).not.toHaveBeenCalled()

    fireEvent.change(textarea, { target: { value: 'Linha 1\nLinha 2' } })
    fireEvent.keyDown(textarea, { key: 'Enter', shiftKey: false })
    await waitFor(() => expect(streamChat).toHaveBeenCalled())
  })

  it('critério 5: seletor de modelo/esforço envia a escolha selecionada na chamada', async () => {
    fetchLLMOptions.mockResolvedValue({
      mode: 'byo-key',
      models: ['claude-opus-5', 'claude-sonnet-5'],
      efforts: ['low', 'high'],
    })
    streamChat.mockResolvedValue({ answered: true, reply: 'ok' })
    renderChat()

    await screen.findByText('claude-opus-5')
    // F-0241: seleção pelo menu, não por change num <select>
    fireEvent.click(screen.getByLabelText(/modelo/i))
    fireEvent.click(screen.getByRole('menuitemradio', { name: /claude-opus-5/ }))
    fireEvent.click(screen.getByLabelText(/esfor/i))
    fireEvent.click(screen.getByRole('menuitemradio', { name: /high/ }))

    fireEvent.change(screen.getByPlaceholderText(/descreva/i), { target: { value: 'Oi' } })
    fireEvent.click(screen.getByRole('button', { name: /enviar/i }))

    await waitFor(() =>
      expect(streamChat).toHaveBeenCalledWith(
        [{ role: 'user', content: 'Oi' }],
        { model: 'claude-opus-5', effort: 'high' },
        expect.any(Function),
      ),
    )
  })

  it('F-0219: a conversa recebida com modelo/esforço já salvos pré-seleciona o seletor', async () => {
    fetchLLMOptions.mockResolvedValue({
      mode: 'byo-key',
      models: ['claude-opus-5', 'claude-sonnet-5'],
      efforts: ['low', 'high'],
    })
    renderChat({ model: 'claude-opus-5', effort: 'high' })

    await screen.findByText('claude-opus-5')
    // F-0241: o seletor virou botão + menu (o <select> nativo não renderiza
    // marca de selecionado nem dígito de atalho), então o valor corrente é o
    // texto do gatilho, não a `value` de um combobox. A garantia é a mesma.
    expect(screen.getByLabelText(/modelo/i)).toHaveTextContent('claude-opus-5')
    expect(screen.getByLabelText(/esfor/i)).toHaveTextContent('high')
  })

  it('F-0241: sem seleção salva, model e effort nascem com o padrão preferido', async () => {
    fetchLLMOptions.mockResolvedValue({
      mode: 'byo-key',
      models: ['claude-sonnet-5', 'claude-opus-5'],
      efforts: ['low', 'medium', 'high'],
    })
    renderChat({ model: '', effort: '' })

    // preferência é opus/high, mesmo não sendo os primeiros da lista
    await waitFor(() =>
      expect(screen.getByLabelText(/modelo/i)).toHaveTextContent('claude-opus-5'),
    )
    expect(screen.getByLabelText(/esfor/i)).toHaveTextContent('high')
  })

  it('F-0241: escolher no menu troca o valor e fecha', async () => {
    fetchLLMOptions.mockResolvedValue({
      mode: 'byo-key',
      models: ['claude-opus-5', 'claude-sonnet-5'],
      efforts: ['low', 'high'],
    })
    renderChat({ model: 'claude-opus-5', effort: 'high' })

    await screen.findByText('claude-opus-5')
    fireEvent.click(screen.getByLabelText(/modelo/i))
    fireEvent.click(screen.getByRole('menuitemradio', { name: /claude-sonnet-5/ }))

    expect(screen.getByLabelText(/modelo/i)).toHaveTextContent('claude-sonnet-5')
    expect(screen.queryByRole('menu')).toBeNull()
  })
})

describe('BacklogChat (F-0230: resposta em voo sobrevive à navegação)', () => {
  it('critério 1: desmonta durante a requisição → a resposta é gravada e aparece ao remontar', async () => {
    let resolve!: (v: ChatAnswer) => void
    streamChat.mockReturnValue(new Promise((r) => (resolve = r)))
    const { unmount, conversation } = renderChat()

    fireEvent.change(screen.getByPlaceholderText(/descreva/i), { target: { value: 'Oi' } })
    fireEvent.click(screen.getByRole('button', { name: /enviar/i }))
    unmount()

    resolve({ answered: true, reply: 'Voltei.' })
    await vi.waitFor(() => expect(getConversation(conversation.id)?.messages).toHaveLength(2))

    render(<BacklogChat conversation={conversation} onUpdate={vi.fn()} />)
    expect(await screen.findByText('Voltei.')).toBeInTheDocument()
  })

  it('critério 2: remonta com a resposta ainda em voo → o indicador de processamento reaparece', async () => {
    streamChat.mockReturnValue(new Promise(() => {}))
    const { unmount, conversation } = renderChat()

    fireEvent.change(screen.getByPlaceholderText(/descreva/i), { target: { value: 'Oi' } })
    fireEvent.click(screen.getByRole('button', { name: /enviar/i }))
    unmount()

    render(<BacklogChat conversation={conversation} onUpdate={vi.fn()} />)
    expect(screen.getByText(/processando/i)).toBeInTheDocument()
  })

  it('critério 3: o indicador mostra tempo decorrido e modelo/esforço, nunca texto estático', async () => {
    vi.useFakeTimers()
    try {
      streamChat.mockReturnValue(new Promise(() => {}))
      fetchLLMOptions.mockResolvedValue({ mode: 'byo-key', models: ['claude-opus-5'], efforts: ['medium'] })
      renderChat({ model: 'claude-opus-5', effort: 'medium' })

      await vi.waitFor(() =>
        expect(screen.getByLabelText(/modelo/i)).toHaveTextContent('claude-opus-5'),
      )
      fireEvent.change(screen.getByPlaceholderText(/descreva/i), { target: { value: 'Oi' } })
      fireEvent.click(screen.getByRole('button', { name: /enviar/i }))

      await vi.advanceTimersByTimeAsync(83_000)
      expect(screen.getByText(/1m23s/)).toBeInTheDocument()
      expect(screen.getByText(/claude-opus-5\/medium/)).toBeInTheDocument()
    } finally {
      vi.useRealTimers()
    }
  })

  it('critério 4: falha vira bolha de erro com reenviar; reenviar refaz a chamada com sucesso', async () => {
    streamChat.mockRejectedValueOnce(new Error('rede caiu'))
    renderChat()

    fireEvent.change(screen.getByPlaceholderText(/descreva/i), { target: { value: 'Oi' } })
    fireEvent.click(screen.getByRole('button', { name: /enviar/i }))
    await screen.findByText('rede caiu')

    streamChat.mockResolvedValueOnce({ answered: true, reply: 'Tudo certo agora.' })
    fireEvent.click(screen.getByRole('button', { name: /reenviar/i }))

    await waitFor(() => expect(screen.getByText('Tudo certo agora.')).toBeInTheDocument())
    expect(streamChat).toHaveBeenCalledTimes(2)
  })

  it('critério 5: um segundo envio na mesma conversa é bloqueado enquanto a primeira segue pendente', async () => {
    streamChat.mockReturnValue(new Promise(() => {}))
    renderChat()

    fireEvent.change(screen.getByPlaceholderText(/descreva/i), { target: { value: 'Oi' } })
    fireEvent.click(screen.getByRole('button', { name: /enviar/i }))

    expect(screen.getByRole('button', { name: /enviar/i })).toBeDisabled()
    expect(streamChat).toHaveBeenCalledTimes(1)
  })
})
