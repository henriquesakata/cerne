import { beforeEach, describe, expect, it, vi } from 'vitest'
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import BacklogChat, { parseCommands, parseNote } from './BacklogChat'
import { setApiProject, type ChatAnswer, type ChatOverride, type CreateOutcome, type Feature, type LLMOptions, type Note } from './api'
import { getConversation, useBacklogChats, writeChats, type Conversation } from './useBacklogChats'

const streamChat = vi.fn<
  (messages: unknown, override: ChatOverride | undefined, onDelta: (d: string) => void) => Promise<ChatAnswer>
>()
const registerChatCandidate = vi.fn<
  (
    title: string,
    specBody: string,
    facets?: string[],
    components?: string[],
    module?: string,
    capability?: string,
    parent?: string,
    featureId?: string,
  ) => Promise<CreateOutcome>
>()
const registerChatNote = vi.fn<
  (
    title: string,
    body: string,
    facets: string[],
    components: string[],
    informs?: string[],
  ) => Promise<{ ok: true; note: Note } | { ok: false; error: string }>
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
    module?: string,
    capability?: string,
    parent?: string,
    featureId?: string,
  ) => registerChatCandidate(title, specBody, facets, components, module, capability, parent, featureId),
  registerChatNote: (title: string, body: string, facets: string[], components: string[], informs?: string[]) =>
    registerChatNote(title, body, facets, components, informs),
  fetchLLMOptions: () => fetchLLMOptions(),
}))

let nextConvId = 0

beforeEach(() => {
  cleanup()
  localStorage.clear()
  setApiProject('proj-x')
  streamChat.mockReset()
  registerChatCandidate.mockReset()
  registerChatNote.mockReset()
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

  it('com bloco de proposta, mostra o rascunho no painel e só registra ao confirmar', async () => {
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

    expect(screen.getByText('Exportar relatórios em CSV')).toBeInTheDocument()
    fireEvent.click(registerButton)

    await waitFor(() =>
      expect(registerChatCandidate).toHaveBeenCalledWith(
        'Exportar relatórios em CSV',
        expect.stringContaining('## Problema'),
        [],
        [],
        '',
        '',
        '',
        '',
      ),
    )
    await waitFor(() => expect(onRegistered).toHaveBeenCalledWith(feature('F-0099')))
  })

  it('F-0223: proposta com facet/components/capability/module mostra chips e registra as tags', async () => {
    const proposal =
      'Perfeito, aqui está a proposta:\n\n' +
      '```cerne-proposta\n' +
      '# Exportar relatórios em CSV\n' +
      'facet: code\n' +
      'module: commons\n' +
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
    expect(screen.getByText('commons')).toBeInTheDocument()
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
        'commons',
        'C-0001',
        '',
        '',
      ),
    )
  })

  it('F-0254: parent no bloco vai junto no registro', async () => {
    const proposal =
      'Proposta:\n\n' +
      '```cerne-proposta\n' +
      '# Exportar relatórios em CSV\n' +
      'facet: code\n' +
      'parent: F-0252\n\n' +
      '## Problema\nFalta exportação.\n' +
      '```\n'
    streamChat.mockResolvedValue({ answered: true, reply: proposal })
    registerChatCandidate.mockResolvedValue({ ok: true, feature: feature('F-0099') })
    renderChat()

    fireEvent.change(screen.getByPlaceholderText(/descreva/i), { target: { value: 'Oi' } })
    fireEvent.click(screen.getByRole('button', { name: /enviar/i }))
    fireEvent.click(await screen.findByRole('button', { name: /registrar candidata/i }))

    await waitFor(() =>
      expect(registerChatCandidate).toHaveBeenCalledWith(
        'Exportar relatórios em CSV',
        expect.stringContaining('## Problema'),
        ['code'],
        [],
        '',
        '',
        'F-0252',
        '',
      ),
    )
  })

  it('F-0250 critérios 2/4: comentar trecho do card injeta citação no compositor sem enviar', async () => {
    const proposal =
      'Proposta:\n\n' +
      '```cerne-proposta\n' +
      '# Exportar relatórios em CSV\n\n' +
      '## Problema\nFalta exportação.\n' +
      '```\n'
    streamChat.mockResolvedValue({ answered: true, reply: proposal })
    renderChat()

    fireEvent.change(screen.getByPlaceholderText(/descreva/i), { target: { value: 'Oi' } })
    fireEvent.click(screen.getByRole('button', { name: /enviar/i }))
    await screen.findByRole('button', { name: /registrar candidata/i })
    streamChat.mockClear()

    const spec = screen.getByText('Falta exportação.')
    const range = document.createRange()
    range.selectNodeContents(spec)
    const sel = window.getSelection()!
    sel.removeAllRanges()
    sel.addRange(range)
    fireEvent.mouseUp(spec)

    fireEvent.change(screen.getByPlaceholderText(/ajustar/i), { target: { value: 'muito vago' } })
    fireEvent.click(screen.getByRole('button', { name: /^comentar$/i }))

    const composer = screen.getByPlaceholderText(/descreva/i) as HTMLTextAreaElement
    expect(composer.value).toBe('> Falta exportação.\n\nmuito vago\n\n')
    // Critério 4: nada é enviado — o turno segue sendo o normal, quando o
    // usuário mandar.
    expect(streamChat).not.toHaveBeenCalled()
  })

  it('F-0255 critério 1: a transcrição referencia o painel em vez de repetir a spec', async () => {
    const proposal =
      'Perfeito, aqui está a proposta:\n\n' +
      '```cerne-proposta\n' +
      '# Exportar relatórios em CSV\n\n' +
      '## Problema\nFalta exportação.\n' +
      '```\n'
    streamChat.mockResolvedValue({ answered: true, reply: proposal })
    renderChat()

    fireEvent.change(screen.getByPlaceholderText(/descreva/i), { target: { value: 'Oi' } })
    fireEvent.click(screen.getByRole('button', { name: /enviar/i }))
    await screen.findByRole('button', { name: /registrar candidata/i })

    expect(screen.getByText('Perfeito, aqui está a proposta:')).toBeInTheDocument()
    expect(screen.getByText(/no painel do rascunho/i)).toBeInTheDocument()
    // A spec aparece uma única vez — no painel, não duplicada no scroll.
    expect(screen.getAllByText('Falta exportação.')).toHaveLength(1)
  })

  it('F-0255 critérios 2/6: revisões saem do transcript — reabrir a conversa restaura v1 e v2', () => {
    const rev = (title: string, body: string) =>
      '```cerne-proposta\n' + `# ${title}\n\n` + `## Problema\n${body}\n` + '```\n'
    renderChat({
      messages: [
        { role: 'user', content: 'Preciso exportar.' },
        { role: 'assistant', content: rev('Exportar em CSV', 'Falta exportação.') },
        { role: 'user', content: '> Falta exportação.\n\nexportação do quê?\n\n' },
        { role: 'assistant', content: rev('Exportar relatórios em CSV', 'Falta exportar relatórios.') },
      ],
    })

    const tabs = screen.getAllByRole('tab')
    expect(tabs.map((t) => t.textContent)).toEqual(['v1', 'v2'])
    expect(tabs[1]).toHaveAttribute('aria-selected', 'true')
    expect(screen.getByText('Exportar relatórios em CSV')).toBeInTheDocument()

    fireEvent.click(tabs[0])
    expect(screen.getByText('Exportar em CSV')).toBeInTheDocument()
  })

  it('registrar usa a versão corrente; a antiga só é leitura (supera o critério 4 da F-0255)', async () => {
    const rev = (title: string) => '```cerne-proposta\n' + `# ${title}\n\n` + '## Problema\nX.\n' + '```\n'
    registerChatCandidate.mockResolvedValue({ ok: true, feature: feature('F-0099') })
    renderChat({
      messages: [
        { role: 'assistant', content: rev('Título v1') },
        { role: 'assistant', content: rev('Título v2') },
      ],
    })

    fireEvent.click(screen.getAllByRole('tab')[0])
    expect(screen.getByRole('button', { name: /registrar candidata/i })).toBeDisabled()

    fireEvent.click(screen.getAllByRole('tab')[1])
    fireEvent.click(screen.getByRole('button', { name: /registrar candidata/i }))

    await waitFor(() =>
      expect(registerChatCandidate).toHaveBeenCalledWith('Título v2', expect.stringContaining('## Problema'), [], [], '', '', '', ''),
    )
    // Registrada: o painel some até o agente emitir a próxima revisão.
    await waitFor(() =>
      expect(screen.queryByRole('button', { name: /registrar candidata/i })).not.toBeInTheDocument(),
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
    expect(screen.getByText('Exportar relatórios em CSV')).toBeInTheDocument()
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

// Harness (F-0248): o defeito do caminho 2 só aparece com o dono real da
// persistência no circuito — é a cópia em memória do componente devolvida ao
// useBacklogChats que vencia a escrita do stream. Espelha o Backlog.tsx.
function Harness() {
  const chats = useBacklogChats()
  return <BacklogChat key={chats.active.id} conversation={chats.active} onUpdate={chats.updateActive} />
}

describe('BacklogChat (F-0248): sair da tela com resposta em voo', () => {
  it('critério 10: voltar depois da conclusão mostra a resposta completa', async () => {
    let resolveStream!: (v: ChatAnswer) => void
    let onDelta!: (d: string) => void
    streamChat.mockImplementation((_m, _o, cb) => {
      onDelta = cb
      return new Promise((r) => (resolveStream = r))
    })
    let resolveOpts!: (v: LLMOptions) => void
    fetchLLMOptions.mockImplementation(() => new Promise((r) => (resolveOpts = r)))

    writeChats([{ id: 'c-nav', title: '', updatedAt: 0, messages: [], model: '', effort: '' }], 'c-nav')
    const first = render(<Harness />)
    await act(async () => resolveOpts({ mode: 'byo-key', models: ['claude-opus-5'], efforts: ['high'] }))

    fireEvent.change(screen.getByPlaceholderText(/descreva/i), { target: { value: 'Preciso de X' } })
    fireEvent.click(screen.getByRole('button', { name: /enviar/i }))
    act(() => onDelta('começo'))

    // Navega pra outra tela: o BacklogChat desmonta, o chatRequest segue vivo.
    first.unmount()

    // Volta antes de a resposta terminar; as opções de LLM resolvem DEPOIS do
    // mount, que é a ordem real e o gatilho do onUpdate extra.
    fetchLLMOptions.mockImplementation(() => new Promise((r) => (resolveOpts = r)))
    render(<Harness />)
    await act(async () => resolveStream({ answered: true, reply: 'resposta completa do agente' }))
    await act(async () => resolveOpts({ mode: 'byo-key', models: ['claude-opus-5'], efforts: ['high'] }))

    await waitFor(() => expect(screen.getByText('resposta completa do agente')).toBeInTheDocument())
    expect(getConversation('c-nav')?.messages).toEqual([
      { role: 'user', content: 'Preciso de X' },
      { role: 'assistant', content: 'resposta completa do agente' },
    ])
  })
})

// F-0256: a conversa aberta pelo card de uma candidata refina a spec dela —
// o turno leva o id (o servidor carrega a spec corrente) e a confirmação
// atualiza aquela candidata em vez de registrar outra.
describe('BacklogChat — refino de candidata (F-0256)', () => {
  const proposal =
    '```cerne-proposta\n' +
    '# Exportar relatórios em CSV\n' +
    'feature: F-0010\n\n' +
    '## Problema\nFalta exportação.\n' +
    '```\n'

  it('critério 1: o turno de uma conversa de refino carrega o id da candidata', async () => {
    streamChat.mockResolvedValue({ answered: true, reply: 'ok' })
    renderChat({ featureId: 'F-0010' })

    fireEvent.change(screen.getByPlaceholderText(/descreva/i), { target: { value: 'Ajusta o escopo.' } })
    fireEvent.click(screen.getByRole('button', { name: /enviar/i }))

    await waitFor(() =>
      expect(streamChat).toHaveBeenCalledWith(
        [{ role: 'user', content: 'Ajusta o escopo.' }],
        expect.objectContaining({ featureId: 'F-0010' }),
        expect.any(Function),
      ),
    )
  })

  it('critérios 2/3/4: confirmar atualiza a candidata em vez de registrar uma nova', async () => {
    registerChatCandidate.mockResolvedValue({ ok: true, feature: feature('F-0010') })
    renderChat({ featureId: 'F-0010', messages: [{ role: 'assistant', content: proposal }] })

    // O painel anuncia que é revisão da candidata, e o botão não promete registro.
    expect(screen.getByText('F-0010')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /registrar candidata/i })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /atualizar spec/i }))

    await waitFor(() =>
      expect(registerChatCandidate).toHaveBeenCalledWith(
        'Exportar relatórios em CSV',
        expect.stringContaining('## Problema'),
        [],
        [],
        '',
        '',
        '',
        'F-0010',
      ),
    )
  })

  it('conversa comum segue registrando candidata nova (sem id)', async () => {
    registerChatCandidate.mockResolvedValue({ ok: true, feature: feature('F-0099') })
    renderChat({ messages: [{ role: 'assistant', content: proposal }] })

    fireEvent.click(screen.getByRole('button', { name: /registrar candidata/i }))
    await waitFor(() =>
      expect(registerChatCandidate).toHaveBeenCalledWith(
        'Exportar relatórios em CSV',
        expect.any(String),
        [],
        [],
        '',
        '',
        '',
        '',
      ),
    )
  })
})

// F-0264: a conversa órfã (sem featureId) que bate no guard de duplicidade
// se vincula por clique à candidata que o próprio guard apontou.
describe('BacklogChat — vínculo com a candidata colidida (F-0264)', () => {
  const proposal =
    '```cerne-proposta\n' +
    '# Exportar relatórios em CSV\n\n' +
    '## Problema\nFalta exportação.\n' +
    '```\n'

  const refused = (matches: { id: string; state: string; title: string }[]) => ({
    ok: false as const,
    error: 'a similar registration already exists',
    matches,
  })

  it('critérios 2/3/4: 409 com uma candidata oferece a ação, e o clique grava nela', async () => {
    registerChatCandidate.mockResolvedValueOnce(
      refused([{ id: 'F-0011', state: 'candidate', title: 'Exportar relatórios' }]),
    )
    const onRegistered = vi.fn()
    renderChat({ messages: [{ role: 'assistant', content: proposal }] }, { onRegistered })

    fireEvent.click(screen.getByRole('button', { name: /registrar candidata/i }))
    const link = await screen.findByRole('button', { name: /atualizar F-0011/i })
    expect(registerChatCandidate).toHaveBeenCalledTimes(1) // crit. 7: nada gravado sem o clique

    registerChatCandidate.mockResolvedValueOnce({ ok: true, feature: feature('F-0011') })
    fireEvent.click(link)

    await waitFor(() =>
      expect(registerChatCandidate).toHaveBeenLastCalledWith(
        'Exportar relatórios em CSV',
        expect.stringContaining('## Problema'),
        [],
        [],
        '',
        '',
        '',
        'F-0011',
      ),
    )
    // crit. 4: a conversa passa a apontar pra candidata (appendRegisteredId).
    await waitFor(() => expect(onRegistered).toHaveBeenCalledWith(feature('F-0011')))
  })

  it('critério 5: colisão fora de candidate não oferece a ação', async () => {
    registerChatCandidate.mockResolvedValue(
      refused([{ id: 'F-0011', state: 'approved', title: 'Exportar relatórios' }]),
    )
    renderChat({ messages: [{ role: 'assistant', content: proposal }] })

    fireEvent.click(screen.getByRole('button', { name: /registrar candidata/i }))
    await screen.findByText(/already exists/i)
    expect(screen.queryByRole('button', { name: /atualizar F-0011/i })).not.toBeInTheDocument()
  })

  it('critérios 6/7: duas candidatas exigem escolha explícita', async () => {
    registerChatCandidate.mockResolvedValue(
      refused([
        { id: 'F-0011', state: 'candidate', title: 'Exportar relatórios' },
        { id: 'F-0012', state: 'candidate', title: 'Exportar relatórios em CSV' },
      ]),
    )
    renderChat({ messages: [{ role: 'assistant', content: proposal }] })

    fireEvent.click(screen.getByRole('button', { name: /registrar candidata/i }))
    await screen.findByRole('button', { name: /atualizar F-0011/i })
    expect(screen.getByRole('button', { name: /atualizar F-0012/i })).toBeInTheDocument()
    expect(registerChatCandidate).toHaveBeenCalledTimes(1)
  })
})

// F-0249: o segundo bloco cercado da conversa — nota de conhecimento durável,
// com o mesmo gate humano da candidata (o agente propõe, o clique grava).
describe('BacklogChat — proposta de nota (F-0249)', () => {
  const noteReply =
    'Boa — isso é vocabulário do domínio, vale registrar:\n\n' +
    '```cerne-nota\n' +
    '# Candidata é a proposta antes do approve\n' +
    'facet: code\n' +
    'components: backlog, chat\n' +
    'informs: F-0001\n\n' +
    'Neste produto, candidata é a feature proposta e ainda não aprovada.\n' +
    '```\n'

  async function sendAndGetReply(reply: string) {
    streamChat.mockResolvedValue({ answered: true, reply })
    renderChat()
    fireEvent.change(screen.getByPlaceholderText(/descreva/i), { target: { value: 'Oi' } })
    fireEvent.click(screen.getByRole('button', { name: /enviar/i }))
  }

  it('parseNote extrai título, corpo, facet, components e informs (critério 2)', () => {
    const n = parseNote(noteReply)
    expect(n).not.toBeNull()
    expect(n!.title).toBe('Candidata é a proposta antes do approve')
    expect(n!.facet).toBe('code')
    expect(n!.components).toEqual(['backlog', 'chat'])
    expect(n!.informs).toEqual(['F-0001'])
    expect(n!.body).toBe('Neste produto, candidata é a feature proposta e ainda não aprovada.')
  })

  it('resposta sem bloco de nota não renderiza card (critério 4)', async () => {
    await sendAndGetReply('Me conta mais sobre o formato.')
    await waitFor(() => expect(screen.getByText('Me conta mais sobre o formato.')).toBeInTheDocument())
    expect(screen.queryByRole('button', { name: /registrar nota/i })).not.toBeInTheDocument()
    expect(registerChatNote).not.toHaveBeenCalled()
  })

  it('renderiza o card com chips e só grava ao confirmar (critérios 2/3/4)', async () => {
    registerChatNote.mockResolvedValue({
      ok: true,
      note: {
        id: 'N-0044',
        title: 'Candidata é a proposta antes do approve',
        state: 'active',
        created_at: '2026-08-09T00:00:00Z',
        updated_at: '2026-08-09T00:00:00Z',
      },
    })
    await sendAndGetReply(noteReply)

    const button = await screen.findByRole('button', { name: /registrar nota/i })
    // O bloco cercado sai da transcrição — quem mostra o conteúdo é o card.
    expect(screen.getByText('Candidata é a proposta antes do approve')).toBeInTheDocument()
    expect(screen.getByText('backlog')).toBeInTheDocument()
    expect(screen.getByText('F-0001')).toBeInTheDocument()
    expect(screen.queryByText(/```/)).not.toBeInTheDocument()
    expect(registerChatNote).not.toHaveBeenCalled()

    fireEvent.click(button)
    await waitFor(() => expect(registerChatNote).toHaveBeenCalledWith(
      'Candidata é a proposta antes do approve',
      'Neste produto, candidata é a feature proposta e ainda não aprovada.',
      ['code'],
      ['backlog', 'chat'],
      ['F-0001'],
    ))
    // Critério 3: o id criado aparece na conversa.
    await waitFor(() => expect(screen.getByText(/N-0044/)).toBeInTheDocument())
  })

  it('recusa do servidor por falta de tag vira erro visível (critério 5)', async () => {
    registerChatNote.mockResolvedValue({
      ok: false,
      error: 'facet is required: an untagged note is born as lint debt (untagged-live)',
    })
    await sendAndGetReply(noteReply)

    fireEvent.click(await screen.findByRole('button', { name: /registrar nota/i }))
    await waitFor(() => expect(screen.getByText(/facet is required/)).toBeInTheDocument())
    expect(screen.queryByText(/N-0044/)).not.toBeInTheDocument()
  })
})

// F-0260: o terceiro bloco cercado — os comandos que o agente pretende rodar,
// ecoados para o usuário confirmar. O eco é o que torna a confirmação
// informada; o clique é o que arma a escrita no servidor.
describe('BacklogChat — confirmação de comandos (F-0260)', () => {
  const commandReply =
    'Posso registrar por aqui. Pretendo rodar:\n\n' +
    '```cerne-comando\n' +
    'cerne feature new "Exportar CSV" --context "Falta exportação" --facet code --component ui\n' +
    'cerne note new "Exportação é sempre CSV" --facet process --component ui\n' +
    '```\n'

  async function sendAndGetReply(reply: string) {
    streamChat.mockResolvedValue({ answered: true, reply })
    renderChat()
    fireEvent.change(screen.getByPlaceholderText(/descreva/i), { target: { value: 'Registra isso pra mim' } })
    fireEvent.click(screen.getByRole('button', { name: /enviar/i }))
  }

  it('parseCommands extrai uma linha por comando (critério 1)', () => {
    expect(parseCommands(commandReply)).toEqual([
      'cerne feature new "Exportar CSV" --context "Falta exportação" --facet code --component ui',
      'cerne note new "Exportação é sempre CSV" --facet process --component ui',
    ])
    expect(parseCommands('Sem bloco nenhum.')).toBeNull()
  })

  it('mostra os comandos e NÃO manda turno confirmado sem o clique (critério 3)', async () => {
    await sendAndGetReply(commandReply)

    await screen.findByRole('button', { name: /executar/i })
    expect(screen.getByText(/cerne feature new "Exportar CSV"/)).toBeInTheDocument()
    // O bloco cercado sai da transcrição — quem mostra é o card.
    expect(screen.queryByText(/```/)).not.toBeInTheDocument()
    // O único turno enviado é o do usuário, sem confirmação.
    expect(streamChat).toHaveBeenCalledTimes(1)
    expect(streamChat.mock.calls[0][1]?.confirm).toBeFalsy()
  })

  it('clicar em executar manda um turno com confirm (critérios 1/3)', async () => {
    await sendAndGetReply(commandReply)

    fireEvent.click(await screen.findByRole('button', { name: /executar/i }))

    await waitFor(() => expect(streamChat).toHaveBeenCalledTimes(2))
    const [messages, override] = streamChat.mock.calls[1]
    expect(override?.confirm).toBe(true)
    const sent = messages as { role: string; content: string }[]
    expect(sent[sent.length - 1].role).toBe('user')
  })

  it('resposta sem bloco de comando não renderiza card', async () => {
    await sendAndGetReply('Me conta mais sobre o formato.')
    await waitFor(() => expect(screen.getByText('Me conta mais sobre o formato.')).toBeInTheDocument())
    expect(screen.queryByRole('button', { name: /executar/i })).not.toBeInTheDocument()
  })
})
