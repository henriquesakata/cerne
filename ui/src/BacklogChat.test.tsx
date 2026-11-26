import type { ProposalIssue } from './api'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import BacklogChat, { AnsweredByLine, parseCommands, parseNote, providerName, TurnContextDisclosure } from './BacklogChat'
import { ChatRunsError, setApiProject, type ChatAnswer, type ChatOverride, type ChatTitle, type CreateOutcome, type Feature, type FeatureDetail, type LLMOptions, type ModelEntry, type Note } from './api'
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
    model?: string,
    provider?: string,
  ) => Promise<CreateOutcome & { warning?: string; unchanged?: boolean; missing?: string[] }>
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
const fetchFeature = vi.fn<(id: string) => Promise<FeatureDetail>>()
const setPreferredProvider = vi.fn<(p: string) => Promise<void>>()
// F-0310: nenhum teste depende de fetch real para o resumo do título.
const summarizeChatTitle = vi.fn<() => Promise<ChatTitle>>(async () => ({ generated: false }))

// CATALOG (F-0311): o catálogo como o servidor o serve — correntes, legados
// (submenu), um delistado (nunca ofertado) e um segundo provedor.
const CATALOG: ModelEntry[] = [
  { id: 'claude-opus-5-5', label: 'Claude Opus 5.5', provider: 'anthropic', efforts: ['low', 'medium', 'high'], default_effort: 'medium', visibility: 'current' },
  { id: 'claude-sonnet-5-5', label: 'Claude Sonnet 5.5', provider: 'anthropic', efforts: ['low', 'high'], default_effort: 'high', visibility: 'current' },
  { id: 'claude-opus-5', label: 'Claude Opus 5', provider: 'anthropic', efforts: ['low', 'medium', 'high', 'max'], default_effort: 'high', visibility: 'legacy' },
  { id: 'claude-haiku-4-5', label: 'Claude Haiku 4.5', provider: 'anthropic', visibility: 'legacy' },
  { id: 'claude-gone-1', label: 'Claude Gone 1', provider: 'anthropic', efforts: ['low'], visibility: 'delisted' },
  { id: 'vendor-x-1', label: 'Vendor X 1', provider: 'vendor', efforts: ['fast'], default_effort: 'fast', visibility: 'current' },
]

const opts = (patch: Partial<LLMOptions> = {}): LLMOptions => ({
  mode: 'byo-key',
  models: CATALOG,
  defaultModel: 'claude-opus-5-5',
  providers: ['anthropic'],
  preferredProvider: '',
  imageProviders: [],
  ...patch,
})

const uploadChatAttachment = vi.fn<(conversationId: string, file: Blob) => Promise<string>>()

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
    model?: string,
    provider?: string,
  ) => (model ? registerChatCandidate(title, specBody, facets, components, module, capability, parent, featureId, model, provider) : registerChatCandidate(title, specBody, facets, components, module, capability, parent, featureId)),
  registerChatNote: (title: string, body: string, facets: string[], components: string[], informs?: string[]) =>
    registerChatNote(title, body, facets, components, informs),
  fetchLLMOptions: () => fetchLLMOptions(),
  uploadChatAttachment: (conversationId: string, file: Blob) => uploadChatAttachment(conversationId, file),
  fetchFeature: (id: string) => fetchFeature(id),
  setPreferredProvider: (p: string) => setPreferredProvider(p),
  summarizeChatTitle: () => summarizeChatTitle(),
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
  fetchFeature.mockReset()
  fetchFeature.mockRejectedValue(new Error('not mocked'))
  fetchLLMOptions.mockResolvedValue(opts({ models: [], defaultModel: '', providers: [] }))
  setPreferredProvider.mockReset()
  setPreferredProvider.mockResolvedValue()
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
    const { conversation } = renderChat()

    fireEvent.change(screen.getByPlaceholderText(/descreva/i), {
      target: { value: 'Preciso exportar relatórios.' },
    })
    fireEvent.click(screen.getByRole('button', { name: /enviar/i }))

    await waitFor(() => expect(screen.getByText('Em qual formato?')).toBeInTheDocument())
    expect(streamChat).toHaveBeenCalledWith(
      [{ role: 'user', content: 'Preciso exportar relatórios.' }],
      // F-0323: o id da conversa vai em todo turno (proveniência das escritas).
      { model: undefined, effort: undefined, conversationId: conversation.id },
      expect.any(Function),
    )
  })

  it('nasce desabilitado com orientação quando o projeto não tem [llm] configurado (critério 5)', async () => {
    fetchLLMOptions.mockResolvedValue(opts({ mode: 'delegation', models: [], defaultModel: '', providers: [] }))
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

  it('F-0435: a 422 with missing sections is listed in the card and the conversation stays open', async () => {
    streamChat.mockResolvedValue({
      answered: true,
      reply: '````cerne-proposta\n# Exportar CSV\n\n## Problema\nFalta.\n````\n',
    })
    registerChatCandidate.mockResolvedValue({
      ok: false,
      error: 'the proposal is missing template sections: Verificação, Fora de escopo',
      missing: ['Verificação', 'Fora de escopo'],
    })
    renderChat({}, { onRegistered: vi.fn() })
    fireEvent.change(screen.getByPlaceholderText(/descreva/i), { target: { value: 'Oi' } })
    fireEvent.click(screen.getByRole('button', { name: /enviar/i }))
    fireEvent.click(await screen.findByRole('button', { name: /registrar candidata/i }))
    const alerts = await screen.findAllByRole('alert')
    expect(alerts.some((a) => a.textContent?.includes('Fora de escopo') && a.tagName === 'DIV')).toBe(true)
    expect(screen.getByRole('button', { name: /registrar candidata/i })).toBeEnabled()
    expect(screen.getByPlaceholderText(/descreva/i)).toBeEnabled()
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

  it('F-0445: com o Copilot escolhido, nenhum chip de modelo/esforço aparece e nenhum é enviado', async () => {
    streamChat.mockResolvedValue({ answered: true, reply: 'ok' })
    fetchLLMOptions.mockResolvedValue(opts({ providers: ['anthropic', 'copilot'] }))
    renderChat({ model: 'claude-opus-5-5', effort: 'medium' })
    await screen.findByText('Claude Opus 5.5')
    fireEvent.click(screen.getByLabelText(/provedor/i))
    fireEvent.click(screen.getByRole('menuitemradio', { name: /GitHub Copilot/ }))
    expect(screen.getByLabelText(/provedor/i)).toHaveTextContent('GitHub Copilot')
    expect(screen.queryByLabelText(/modelo/i)).toBeNull()
    expect(screen.queryByLabelText(/esfor/i)).toBeNull()
    fireEvent.change(screen.getByPlaceholderText(/descreva/i), { target: { value: 'Oi' } })
    fireEvent.click(screen.getByRole('button', { name: /enviar/i }))
    await waitFor(() => expect(streamChat).toHaveBeenCalled())
    const sent = streamChat.mock.calls.at(-1)![1]
    expect(sent).toMatchObject({ provider: 'copilot' })
    expect(sent?.model).toBeUndefined()
    expect(sent?.effort).toBeUndefined()
  })

  it('F-0445: ao registrar depois de um turno do Copilot, o modelo que respondeu vai como autor', async () => {
    const proposal = '```cerne-proposta\n# Exportar CSV\n\n## Problema\nFalta.\n```\n'
    streamChat.mockResolvedValue({ answered: true, reply: proposal, answered_by: { provider: 'copilot', model: 'mai-code-1.1-flash' } })
    registerChatCandidate.mockResolvedValue({ ok: true, feature: feature('F-0099') })
    fetchLLMOptions.mockResolvedValue(opts({ providers: ['anthropic', 'copilot'] }))
    renderChat({ model: 'claude-opus-5-5', effort: 'medium' })
    await screen.findByText('Claude Opus 5.5')
    fireEvent.click(screen.getByLabelText(/provedor/i))
    fireEvent.click(screen.getByRole('menuitemradio', { name: /GitHub Copilot/ }))
    fireEvent.change(screen.getByPlaceholderText(/descreva/i), { target: { value: 'Oi' } })
    fireEvent.click(screen.getByRole('button', { name: /enviar/i }))
    fireEvent.click(await screen.findByRole('button', { name: /registrar candidata/i }))
    await waitFor(() => expect(registerChatCandidate).toHaveBeenCalled())
    const args = registerChatCandidate.mock.calls.at(-1)!
    expect(args[8]).toBe('mai-code-1.1-flash')
    expect(args[9]).toBe('copilot')
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
    fetchLLMOptions.mockResolvedValue(opts())
    streamChat.mockResolvedValue({ answered: true, reply: 'ok' })
    renderChat()

    await screen.findByText('Claude Opus 5.5')
    // F-0241: seleção pelo menu, não por change num <select>
    fireEvent.click(screen.getByLabelText(/modelo/i))
    fireEvent.click(screen.getByRole('menuitemradio', { name: /Claude Sonnet 5\.5/ }))
    fireEvent.click(screen.getByLabelText(/esfor/i))
    fireEvent.click(screen.getByRole('menuitemradio', { name: /low/ }))

    fireEvent.change(screen.getByPlaceholderText(/descreva/i), { target: { value: 'Oi' } })
    fireEvent.click(screen.getByRole('button', { name: /enviar/i }))

    await waitFor(() =>
      expect(streamChat).toHaveBeenCalledWith(
        [{ role: 'user', content: 'Oi' }],
        { provider: 'anthropic', model: 'claude-sonnet-5-5', effort: 'low', conversationId: expect.any(String) },
        expect.any(Function),
      ),
    )
  })

  it('F-0219: a conversa recebida com modelo/esforço já salvos pré-seleciona o seletor', async () => {
    fetchLLMOptions.mockResolvedValue(opts())
    renderChat({ model: 'claude-sonnet-5-5', effort: 'low' })

    // F-0241: o seletor virou botão + menu, então o valor corrente é o texto
    // do gatilho; F-0311: o gatilho mostra o rótulo, nunca o id cru.
    await waitFor(() => expect(screen.getByLabelText(/modelo/i)).toHaveTextContent('Claude Sonnet 5.5'))
    expect(screen.getByLabelText(/esfor/i)).toHaveTextContent('low')
  })

  it('F-0311: sem seleção salva, model e effort nascem do default do catálogo', async () => {
    fetchLLMOptions.mockResolvedValue(opts())
    renderChat({ model: '', effort: '' })

    await waitFor(() => expect(screen.getByLabelText(/modelo/i)).toHaveTextContent('Claude Opus 5.5'))
    // o esforço default declarado do modelo, não um palpite global
    expect(screen.getByLabelText(/esfor/i)).toHaveTextContent('medium')
  })

  it('F-0241: escolher no menu troca o valor e fecha', async () => {
    fetchLLMOptions.mockResolvedValue(opts())
    renderChat({ model: 'claude-opus-5-5', effort: 'high' })

    await screen.findByText('Claude Opus 5.5')
    fireEvent.click(screen.getByLabelText(/modelo/i))
    fireEvent.click(screen.getByRole('menuitemradio', { name: /Claude Sonnet 5\.5/ }))

    expect(screen.getByLabelText(/modelo/i)).toHaveTextContent('Claude Sonnet 5.5')
    expect(screen.queryByRole('menu')).toBeNull()
  })
})

describe('BacklogChat — menu hierárquico de modelos (F-0311)', () => {
  const openModels = async () => {
    await screen.findByText('Claude Opus 5.5')
    fireEvent.click(screen.getByLabelText(/modelo/i))
  }

  it('critérios 8 e 10: correntes no primeiro nível, legados só no submenu, delistados em lugar nenhum', async () => {
    fetchLLMOptions.mockResolvedValue(opts())
    renderChat()
    await openModels()

    const first = screen.getAllByRole('menuitemradio').map((el) => el.textContent)
    expect(first).toHaveLength(2)
    expect(first[0]).toMatch(/Claude Opus 5\.5/)
    expect(first[1]).toMatch(/Claude Sonnet 5\.5/)
    expect(screen.queryByText('Claude Opus 5')).toBeNull()

    fireEvent.click(screen.getByRole('menuitem', { name: /modelos anteriores/i }))
    const legacy = within(screen.getByRole('menu', { name: /modelos anteriores/i }))
    expect(legacy.getByRole('menuitemradio', { name: /Claude Opus 5\b(?!\.)/ })).toBeInTheDocument()
    expect(legacy.getByRole('menuitemradio', { name: /Claude Haiku 4\.5/ })).toBeInTheDocument()
    expect(screen.queryByText('Claude Gone 1')).toBeNull()
  })

  it('C2: só desenha atalho de dígito de 1 a 9 — nunca um atalho que o teclado não atende', async () => {
    const many: ModelEntry[] = Array.from({ length: 11 }, (_, i) => ({
      id: `m-${i + 1}`,
      label: `Modelo ${String.fromCharCode(65 + i)}`,
      provider: 'anthropic',
      visibility: 'current',
    }))
    fetchLLMOptions.mockResolvedValue(opts({ models: many, defaultModel: 'm-1' }))
    renderChat()
    await screen.findByText('Modelo A')
    fireEvent.click(screen.getByLabelText(/modelo/i))

    const keys = screen.getAllByRole('menuitemradio').map((el) => el.querySelector('.chat-menu-item-key')?.textContent)
    expect(keys).toEqual(['✓', '2', '3', '4', '5', '6', '7', '8', '9', '', ''])
  })

  it('critério 9: um legado escolhido no submenu é seleção válida, com os esforços declarados dele', async () => {
    fetchLLMOptions.mockResolvedValue(opts())
    renderChat()
    await openModels()
    fireEvent.click(screen.getByRole('menuitem', { name: /modelos anteriores/i }))
    fireEvent.click(screen.getByRole('menuitemradio', { name: /Claude Opus 5\b(?!\.)/ }))

    expect(screen.getByLabelText(/modelo/i)).toHaveTextContent(/^Claude Opus 5$/)
    fireEvent.click(screen.getByLabelText(/esfor/i))
    expect(screen.getAllByRole('menuitemradio').map((el) => el.textContent?.replace(/[✓\d]+$/, ''))).toEqual([
      'low',
      'medium',
      'high',
      'max',
    ])
  })

  it('critério 11: cada entrada mostra rótulo e provedor, nunca o id cru', async () => {
    fetchLLMOptions.mockResolvedValue(opts())
    renderChat()
    await openModels()

    for (const item of screen.getAllByRole('menuitemradio')) {
      expect(item).toHaveTextContent(/Anthropic/)
    }
    expect(screen.queryByText(/claude-opus-5-5|claude-sonnet-5-5/)).toBeNull()
  })

  it('critérios 12 e 13: o esforço segue o modelo — fora da matriz cai no default declarado, sem seleção vazia', async () => {
    fetchLLMOptions.mockResolvedValue(opts())
    renderChat({ model: 'claude-opus-5-5', effort: 'medium' })
    await openModels()
    // Sonnet 5.5 não aceita "medium": a seleção vira o default dele, sem erro
    fireEvent.click(screen.getByRole('menuitemradio', { name: /Claude Sonnet 5\.5/ }))
    expect(screen.getByLabelText(/esfor/i)).toHaveTextContent('high')

    fireEvent.click(screen.getByLabelText(/esfor/i))
    expect(screen.getAllByRole('menuitemradio').map((el) => el.textContent?.replace(/[✓\d]+$/, ''))).toEqual(['low', 'high'])
    expect(screen.queryByRole('menuitemradio', { name: /medium/ })).toBeNull()
  })

  it('critérios 14 e 15: o seletor de provedor lista os configurados e restringe a lista de modelos', async () => {
    fetchLLMOptions.mockResolvedValue(opts({ providers: ['anthropic', 'vendor'] }))
    renderChat()
    await screen.findByText('Claude Opus 5.5')

    fireEvent.click(screen.getByLabelText(/provedor/i))
    expect(screen.getAllByRole('menuitemradio').map((el) => el.textContent?.replace(/[✓\d]+$/, ''))).toEqual([
      'Anthropic',
      'Vendor',
    ])
    fireEvent.click(screen.getByRole('menuitemradio', { name: /Vendor/ }))

    expect(screen.getByLabelText(/modelo/i)).toHaveTextContent('Vendor X 1')
    expect(screen.getByLabelText(/esfor/i)).toHaveTextContent('fast')
    fireEvent.click(screen.getByLabelText(/modelo/i))
    expect(screen.getAllByRole('menuitemradio').map((el) => el.textContent)).toEqual([expect.stringMatching(/Vendor X 1/)])
  })

  it('F-0440: escolher OpenAI mostra os modelos do Codex e o turno leva o provedor escolhido', async () => {
    streamChat.mockResolvedValue({ answered: true, reply: 'ok' })
    const codex: ModelEntry = { id: 'gpt-x', label: 'GPT X', provider: 'openai', efforts: ['low', 'high'], default_effort: 'high', visibility: 'current' }
    fetchLLMOptions.mockResolvedValue(opts({ providers: ['anthropic', 'openai'], models: [...CATALOG, codex] }))
    renderChat()
    await screen.findByText('Claude Opus 5.5')
    fireEvent.click(screen.getByLabelText(/provedor/i))
    fireEvent.click(screen.getByRole('menuitemradio', { name: /Openai/i }))
    expect(screen.getByLabelText(/modelo/i)).toHaveTextContent('GPT X')

    fireEvent.change(screen.getByPlaceholderText(/descreva/i), { target: { value: 'Oi' } })
    fireEvent.click(screen.getByRole('button', { name: /enviar/i }))
    await waitFor(() => expect(streamChat).toHaveBeenCalled())
    expect(streamChat.mock.calls[0][1]).toMatchObject({ provider: 'openai', model: 'gpt-x', effort: 'high' })
  })

  it('F-0445: o Copilot entra no seletor como GitHub Copilot e o pedido leva provider copilot', async () => {
    streamChat.mockResolvedValue({ answered: true, reply: 'ok' })
    fetchLLMOptions.mockResolvedValue(opts({ providers: ['anthropic', 'copilot'] }))
    renderChat({ model: 'claude-opus-5-5', effort: 'medium' })
    await screen.findByText('Claude Opus 5.5')
    fireEvent.click(screen.getByLabelText(/provedor/i))
    fireEvent.click(screen.getByRole('menuitemradio', { name: /GitHub Copilot/ }))
    fireEvent.change(screen.getByPlaceholderText(/descreva/i), { target: { value: 'Oi' } })
    fireEvent.click(screen.getByRole('button', { name: /enviar/i }))
    await waitFor(() => expect(streamChat).toHaveBeenCalled())
    expect(streamChat.mock.calls.at(-1)![1]).toMatchObject({ provider: 'copilot' })
  })

  it('F-0440: um provedor sem modelos no catálogo (Cursor) mantém o seletor, esconde modelo/esforço e não manda nenhum', async () => {
    streamChat.mockResolvedValue({ answered: true, reply: 'ok' })
    fetchLLMOptions.mockResolvedValue(opts({ providers: ['anthropic', 'openai', 'cursor'] }))
    renderChat({ model: 'claude-opus-5-5', effort: 'medium' })
    await screen.findByText('Claude Opus 5.5')
    fireEvent.click(screen.getByLabelText(/provedor/i))
    fireEvent.click(screen.getByRole('menuitemradio', { name: /Cursor/ }))

    expect(screen.getByLabelText(/provedor/i)).toHaveTextContent('Cursor')
    expect(screen.queryByLabelText(/modelo/i)).toBeNull()
    expect(screen.queryByLabelText(/esfor/i)).toBeNull()

    fireEvent.change(screen.getByPlaceholderText(/descreva/i), { target: { value: 'Oi' } })
    fireEvent.click(screen.getByRole('button', { name: /enviar/i }))
    await waitFor(() => expect(streamChat).toHaveBeenCalled())
    const sent = streamChat.mock.calls.at(-1)![1]
    expect(sent).toMatchObject({ provider: 'cursor' })
    expect(sent?.model).toBeUndefined()
    expect(sent?.effort).toBeUndefined()

    fireEvent.click(screen.getByLabelText(/provedor/i))
    fireEvent.click(screen.getByRole('menuitemradio', { name: /Anthropic/ }))
    expect(screen.getByLabelText(/modelo/i)).toHaveTextContent('Claude Opus 5.5')
  })

  it('C4: sem rota de preferência (cerne serve), a escolha vale na sessão e nada quebra na tela', async () => {
    fetchLLMOptions.mockResolvedValue(opts({ providers: ['anthropic', 'vendor'] }))
    setPreferredProvider.mockRejectedValue(new Error('route not found'))
    renderChat()
    await screen.findByText('Claude Opus 5.5')
    fireEvent.click(screen.getByLabelText(/provedor/i))
    fireEvent.click(screen.getByRole('menuitemradio', { name: /Vendor/ }))

    await waitFor(() => expect(setPreferredProvider).toHaveBeenCalledWith('vendor'))
    expect(screen.getByLabelText(/provedor/i)).toHaveTextContent('Vendor')
    expect(screen.getByLabelText(/modelo/i)).toHaveTextContent('Vendor X 1')
    expect(screen.queryByText(/route not found/)).toBeNull()
    expect(document.querySelector('.backlog-chat-error')).toBeNull()
  })

  it('critério 16: a escolha de provedor é persistida e volta como seleção inicial', async () => {
    fetchLLMOptions.mockResolvedValue(opts({ providers: ['anthropic', 'vendor'] }))
    const first = renderChat()
    await screen.findByText('Claude Opus 5.5')
    fireEvent.click(screen.getByLabelText(/provedor/i))
    fireEvent.click(screen.getByRole('menuitemradio', { name: /Vendor/ }))
    expect(setPreferredProvider).toHaveBeenCalledWith('vendor')
    first.unmount()

    // "Reinício": o servidor devolve a preferência gravada no arquivo global.
    fetchLLMOptions.mockResolvedValue(opts({ providers: ['anthropic', 'vendor'], preferredProvider: 'vendor' }))
    renderChat()
    await waitFor(() => expect(screen.getByLabelText(/provedor/i)).toHaveTextContent('Vendor'))
    expect(screen.getByLabelText(/modelo/i)).toHaveTextContent('Vendor X 1')
  })
})

describe('BacklogChat — teclado do menu de modelos (C12, paridade com o /model)', () => {
  const open = async () => {
    fetchLLMOptions.mockResolvedValue(opts())
    renderChat({ model: 'claude-opus-5-5', effort: 'medium' })
    await screen.findByText('Claude Opus 5.5')
    fireEvent.click(screen.getByLabelText(/modelo/i))
    return screen.getAllByRole('menu')[0]
  }
  const focused = () => document.activeElement?.textContent ?? ''

  it('ArrowDown/ArrowUp andam entre os itens do primeiro nível', async () => {
    const menu = await open()
    fireEvent.keyDown(menu, { key: 'ArrowDown' })
    expect(focused()).toMatch(/^Claude Opus 5\.5/)
    fireEvent.keyDown(menu, { key: 'ArrowDown' })
    expect(focused()).toMatch(/^Claude Sonnet 5\.5/)
    fireEvent.keyDown(menu, { key: 'ArrowDown' })
    expect(focused()).toMatch(/^Modelos anteriores/)
    fireEvent.keyDown(menu, { key: 'ArrowUp' })
    expect(focused()).toMatch(/^Claude Sonnet 5\.5/)
  })

  it('Enter escolhe o item em foco e o foco volta ao gatilho', async () => {
    const menu = await open()
    fireEvent.keyDown(menu, { key: 'ArrowDown' })
    fireEvent.keyDown(menu, { key: 'ArrowDown' })
    fireEvent.keyDown(menu, { key: 'Enter' })
    const trigger = screen.getByLabelText(/modelo/i)
    expect(trigger).toHaveTextContent('Claude Sonnet 5.5')
    expect(screen.queryByRole('menu')).toBeNull()
    expect(document.activeElement).toBe(trigger)
  })

  it('ArrowRight em "Modelos anteriores" abre o submenu e foca o primeiro legado', async () => {
    const menu = await open()
    for (let i = 0; i < 3; i++) fireEvent.keyDown(menu, { key: 'ArrowDown' })
    fireEvent.keyDown(menu, { key: 'ArrowRight' })
    expect(screen.getByRole('menu', { name: /modelos anteriores/i })).toBeInTheDocument()
    expect(focused()).toMatch(/^Claude Opus 5 /)
    fireEvent.keyDown(menu, { key: 'ArrowDown' })
    expect(focused()).toMatch(/^Claude Haiku 4\.5/)
  })

  it('Enter em "Modelos anteriores" também abre o submenu', async () => {
    const menu = await open()
    for (let i = 0; i < 3; i++) fireEvent.keyDown(menu, { key: 'ArrowDown' })
    fireEvent.keyDown(menu, { key: 'Enter' })
    expect(screen.getByRole('menu', { name: /modelos anteriores/i })).toBeInTheDocument()
    expect(focused()).toMatch(/^Claude Opus 5 /)
  })

  it('ArrowLeft e Escape no submenu voltam ao primeiro nível', async () => {
    const menu = await open()
    for (let i = 0; i < 3; i++) fireEvent.keyDown(menu, { key: 'ArrowDown' })
    fireEvent.keyDown(menu, { key: 'ArrowRight' })
    fireEvent.keyDown(menu, { key: 'ArrowLeft' })
    expect(screen.queryByRole('menu', { name: /modelos anteriores/i })).toBeNull()
    expect(focused()).toMatch(/^Modelos anteriores/)

    fireEvent.keyDown(menu, { key: 'ArrowRight' })
    fireEvent.keyDown(menu, { key: 'Escape' })
    expect(screen.queryByRole('menu', { name: /modelos anteriores/i })).toBeNull()
    expect(screen.getAllByRole('menu')).toHaveLength(1)
    expect(focused()).toMatch(/^Modelos anteriores/)
  })

  it('escolher um legado pelo teclado também devolve o foco ao gatilho', async () => {
    const menu = await open()
    for (let i = 0; i < 3; i++) fireEvent.keyDown(menu, { key: 'ArrowDown' })
    fireEvent.keyDown(menu, { key: 'ArrowRight' })
    fireEvent.keyDown(menu, { key: 'Enter' })
    const trigger = screen.getByLabelText(/modelo/i)
    expect(trigger).toHaveTextContent(/^Claude Opus 5$/)
    expect(document.activeElement).toBe(trigger)
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
      fetchLLMOptions.mockResolvedValue(opts())
      renderChat({ model: 'claude-opus-5', effort: 'medium' })

      await vi.waitFor(() =>
        expect(screen.getByLabelText(/modelo/i)).toHaveTextContent('Claude Opus 5'),
      )
      fireEvent.change(screen.getByPlaceholderText(/descreva/i), { target: { value: 'Oi' } })
      fireEvent.click(screen.getByRole('button', { name: /enviar/i }))

      await vi.advanceTimersByTimeAsync(83_000)
      expect(screen.getByText(/1m23s/)).toBeInTheDocument()
      // F-0311: o indicador nomeia o modelo pelo rótulo do catálogo
      expect(screen.getByText(/Claude Opus 5\/medium/)).toBeInTheDocument()
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

  // B-0049 replaced the block: a second send while the first is in flight is
  // queued, never a concurrent request (the F-0230 invariant still holds).
  it('critério 5: um segundo envio com a primeira pendente não abre outra requisição', async () => {
    streamChat.mockReturnValue(new Promise(() => {}))
    renderChat()

    fireEvent.change(screen.getByPlaceholderText(/descreva/i), { target: { value: 'Oi' } })
    fireEvent.click(screen.getByRole('button', { name: /enviar/i }))
    fireEvent.change(screen.getByPlaceholderText(/descreva/i), { target: { value: 'E mais isto' } })
    fireEvent.click(screen.getByRole('button', { name: /pôr na fila/i }))

    expect(streamChat).toHaveBeenCalledTimes(1)
    expect(screen.getByText('E mais isto')).toBeInTheDocument()
  })
})

// B-0049: the composer stays open while the agent works, the way Claude Code,
// Codex and Cursor take a message mid-turn: it waits in a visible queue and
// goes out as the next turn when the current one ends.
describe('BacklogChat — fila durante o processamento (B-0049)', () => {
  const pendingStream = () => {
    const box: { resolve: ((v: ChatAnswer) => void) | null } = { resolve: null }
    streamChat.mockImplementation(() => new Promise((r) => (box.resolve = r)))
    return box
  }
  const type = (text: string) => fireEvent.change(screen.getByPlaceholderText(/descreva/i), { target: { value: text } })

  it('Enter com o agente ocupado enfileira, limpa o compositor e mostra a mensagem na fila', async () => {
    pendingStream()
    renderChat()
    type('primeira')
    fireEvent.keyDown(screen.getByPlaceholderText(/descreva/i), { key: 'Enter' })
    await screen.findByText(/processando/i)

    type('segunda')
    fireEvent.keyDown(screen.getByPlaceholderText(/descreva/i), { key: 'Enter' })

    expect(screen.getByPlaceholderText(/descreva/i)).toHaveValue('')
    expect(screen.getByText('segunda')).toBeInTheDocument()
    expect(screen.getByText(/na fila/i)).toBeInTheDocument()
    expect(streamChat).toHaveBeenCalledTimes(1)
  })

  it('ao terminar o turno, a fila vai como um único turno seguinte', async () => {
    const stream = pendingStream()
    renderChat()
    type('primeira')
    fireEvent.click(screen.getByRole('button', { name: /enviar/i }))
    await screen.findByText(/processando/i)
    type('segunda')
    fireEvent.click(screen.getByRole('button', { name: /pôr na fila/i }))
    type('terceira')
    fireEvent.click(screen.getByRole('button', { name: /pôr na fila/i }))

    const first = stream.resolve!
    await act(async () => first({ answered: true, reply: 'resposta um' }))

    await waitFor(() => expect(streamChat).toHaveBeenCalledTimes(2))
    const sent = streamChat.mock.calls[1][0] as { role: string; content: string }[]
    expect(sent.map((m) => m.role)).toEqual(['user', 'assistant', 'user'])
    expect(sent[2].content).toBe('segunda\n\nterceira')
    expect(screen.queryByText(/na fila/i)).not.toBeInTheDocument()
  })

  it('tirar da fila remove a mensagem e nada é enviado depois', async () => {
    const stream = pendingStream()
    renderChat()
    type('primeira')
    fireEvent.click(screen.getByRole('button', { name: /enviar/i }))
    await screen.findByText(/processando/i)
    type('arrependi')
    fireEvent.click(screen.getByRole('button', { name: /pôr na fila/i }))
    fireEvent.click(screen.getByRole('button', { name: /tirar da fila/i }))
    expect(screen.queryByText('arrependi')).not.toBeInTheDocument()

    const first = stream.resolve!
    await act(async () => first({ answered: true, reply: 'resposta um' }))
    await screen.findByText('resposta um')
    expect(streamChat).toHaveBeenCalledTimes(1)
  })

  it('sair da conversa com mensagem na fila não a perde: ela vai quando o turno termina', async () => {
    const stream = pendingStream()
    const { unmount } = renderChat()
    type('primeira')
    fireEvent.click(screen.getByRole('button', { name: /enviar/i }))
    await screen.findByText(/processando/i)
    type('vai depois')
    fireEvent.click(screen.getByRole('button', { name: /pôr na fila/i }))
    unmount()

    const first = stream.resolve!
    await act(async () => first({ answered: true, reply: 'resposta um' }))
    await waitFor(() => expect(streamChat).toHaveBeenCalledTimes(2))
    const sent = streamChat.mock.calls[1][0] as { role: string; content: string }[]
    expect(sent.at(-1)).toEqual({ role: 'user', content: 'vai depois' })
  })

  it('se o turno falhar, a fila volta ao compositor em vez de sumir', async () => {
    let reject!: (e: Error) => void
    streamChat.mockImplementation(() => new Promise((_r, j) => (reject = j)))
    renderChat()
    type('primeira')
    fireEvent.click(screen.getByRole('button', { name: /enviar/i }))
    await screen.findByText(/processando/i)
    type('não perca isto')
    fireEvent.click(screen.getByRole('button', { name: /pôr na fila/i }))

    await act(async () => reject(new Error('rede caiu')))
    await screen.findByText('rede caiu')
    expect(screen.getByPlaceholderText(/descreva/i)).toHaveValue('não perca isto')
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
    await act(async () => resolveOpts(opts()))

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
    await act(async () => resolveOpts(opts()))

    await waitFor(() => expect(screen.getByText('resposta completa do agente')).toBeInTheDocument())
    expect(getConversation('c-nav')?.messages).toEqual([
      { role: 'user', content: 'Preciso de X' },
      { role: 'assistant', content: 'resposta completa do agente' },
    ])
  })
})

describe('BacklogChat (F-0310): título desde a primeira mensagem', () => {
  it('F-0310 critério 14: o envio repassa o transcript com a mensagem do usuário antes da resposta', async () => {
    streamChat.mockReturnValue(new Promise(() => {}))
    const { onUpdate } = renderChat()
    fireEvent.change(screen.getByPlaceholderText(/descreva/i), { target: { value: 'Preciso de X.' } })
    fireEvent.click(screen.getByRole('button', { name: /enviar/i }))
    expect(onUpdate).toHaveBeenCalledWith(
      expect.objectContaining({ messages: [{ role: 'user', content: 'Preciso de X.' }] }),
    )
  })

  it('F-0310 critério 14 (Harness): durante a pendência a conversa já tem o título truncado', async () => {
    streamChat.mockReturnValue(new Promise(() => {}))
    writeChats([{ id: 'c-title', title: '', updatedAt: 0, messages: [], model: '', effort: '' }], 'c-title')
    render(<Harness />)
    fireEvent.change(await screen.findByPlaceholderText(/descreva/i), { target: { value: 'Preciso de X.' } })
    fireEvent.click(screen.getByRole('button', { name: /enviar/i }))
    await waitFor(() => expect(getConversation('c-title')?.title).toBe('Preciso de X.'))
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

  // F-0318 critério 5: a conversa de refino descobre o estado do alvo e, se
  // ele já está aprovado, o painel avisa antes do clique.
  it('F-0318: refino de feature aprovada avisa que atualizar vence a aprovação', async () => {
    fetchFeature.mockResolvedValue({ feature: { ...feature('F-0010'), state: 'approved' } } as FeatureDetail)
    renderChat({ featureId: 'F-0010', messages: [{ role: 'assistant', content: proposal }] })
    expect(await screen.findByText(/vence a aprovação/i)).toBeInTheDocument()
    expect(fetchFeature).toHaveBeenCalledWith('F-0010')
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

  // F-0328 (crítico C12): a spec confirmada já é a gravada — o servidor não
  // grava nada e responde "unchanged". Não é erro nem registro novo: aviso
  // neutro, sem a faixa de erro, e onRegistered não dispara.
  it('F-0328: spec igual mostra aviso neutro, não erro nem registro', async () => {
    registerChatCandidate.mockResolvedValue({ ok: true, feature: feature('F-0010'), unchanged: true })
    const onRegistered = vi.fn()
    renderChat({ featureId: 'F-0010', messages: [{ role: 'assistant', content: proposal }] }, { onRegistered })

    fireEvent.click(screen.getByRole('button', { name: /atualizar spec/i }))

    const notice = await screen.findByText('A spec já está igual; nada foi alterado.')
    expect(notice).not.toHaveClass('error')
    expect(notice.closest('.error')).toBeNull()
    expect(onRegistered).not.toHaveBeenCalled()
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

  // F-0323 C3: a dica do card só promete o que o modo cumpre — no modo API o
  // servidor executa só os comandos do card; no modo assinatura, não.
  it('a dica do card é verdadeira em cada modo', async () => {
    fetchLLMOptions.mockResolvedValue(opts({ models: [], defaultModel: '', providers: [] }))
    await sendAndGetReply(commandReply)
    expect(await screen.findByText(/só executa os comandos desta lista/i)).toBeInTheDocument()

    cleanup()
    fetchLLMOptions.mockResolvedValue(opts({ mode: 'claude-cli', models: [], defaultModel: '', providers: [] }))
    nextConvId += 1
    await sendAndGetReply(commandReply)
    expect(await screen.findByText(/modo assinatura/i)).toBeInTheDocument()
    expect(screen.queryByText(/só executa os comandos desta lista/i)).not.toBeInTheDocument()
  })

  // F-0323 critérios 5/8: a confirmação vai presa ao card — o id da conversa
  // e o índice da mensagem do agente que o trazia.
  it('Executar envia conversation_id e confirm_turn', async () => {
    streamChat.mockResolvedValue({ answered: true, reply: commandReply })
    const { conversation } = renderChat()
    fireEvent.change(screen.getByPlaceholderText(/descreva/i), { target: { value: 'Registra isso pra mim' } })
    fireEvent.click(screen.getByRole('button', { name: /enviar/i }))
    fireEvent.click(await screen.findByRole('button', { name: /executar/i }))

    await waitFor(() => expect(streamChat).toHaveBeenCalledTimes(2))
    const [messages, override] = streamChat.mock.calls[1]
    expect(override?.confirm).toBe(true)
    expect(override?.confirmTurn).toBe(1)
    const sent = messages as { role: string; content: string }[]
    expect(sent[1].content).toBe(commandReply)
    expect(conversation.id).toBeTruthy()
  })

  // F-0323 critério 7: sem canal de execução não há card que prometa rodar
  // nada — os comandos viram bloco para copiar.
  it('mensagem noExec não desenha Executar e mostra os comandos copiáveis', async () => {
    streamChat.mockResolvedValue({ answered: true, reply: commandReply, no_exec: true })
    renderChat()
    fireEvent.change(screen.getByPlaceholderText(/descreva/i), { target: { value: 'Registra isso pra mim' } })
    fireEvent.click(screen.getByRole('button', { name: /enviar/i }))

    expect(await screen.findByText(/copie e rode no seu terminal/i)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /executar/i })).not.toBeInTheDocument()
    const pre = document.querySelector('pre')
    expect(pre?.textContent).toContain('cerne feature new "Exportar CSV"')
    expect(pre?.textContent).toContain('cerne note new "Exportação é sempre CSV"')
    expect(screen.getByRole('button', { name: /copiar bloco/i })).toBeInTheDocument()
  })

  // F-0323 critérios 1/3: o painel de execuções mostra o id que o cerne
  // devolveu e o erro nomeado — nunca um id da prosa do agente.
  it('runs mostram o id devolvido e o erro nomeado', async () => {
    streamChat.mockResolvedValue({
      answered: true,
      reply: 'Feito.',
      runs: [
        { tool: 'cerne', verb: 'reference new', args: ['T', '--url', 'u'], result: { ok: true, id: 'R-0099', output: 'R-0099 registered.' } },
        { tool: 'cerne', verb: 'feature new', args: ['X'], result: { ok: true, id: 'F-0004', ids: ['F-0004', 'P-0001'], output: '' } },
        { tool: 'cerne', verb: 'note new', args: ["t; cerne feature approve F-0001 && echo it's"], result: { ok: true, id: 'N-0002', output: '' } },
        { tool: 'cerne', verb: 'feature approve', args: ['F-0001'], result: { ok: false, output: '', error: { code: 'lifecycle_denied', message: 'lifecycle is a human act' } } },
      ],
    })
    renderChat()
    fireEvent.change(screen.getByPlaceholderText(/descreva/i), { target: { value: 'Registra' } })
    fireEvent.click(screen.getByRole('button', { name: /enviar/i }))

    expect(await screen.findByText(/R-0099/)).toBeInTheDocument()
    expect(screen.getByText(/F-0004, P-0001/)).toBeInTheDocument()
    expect(screen.getByText(/lifecycle_denied: lifecycle is a human act/)).toBeInTheDocument()
    expect(screen.getByText('cerne reference new T --url u')).toBeInTheDocument()
    // C15: argumentos com espaço ou metacaractere aparecem citados como no shell.
    expect(screen.getByText(`cerne note new 't; cerne feature approve F-0001 && echo it'\\''s'`)).toBeInTheDocument()
  })

  // F-0323 C5: um turno confirmado que não executou nada não parece sucesso.
  it('turno confirmado sem execuções e resposta truncada avisam que nada rodou', async () => {
    streamChat.mockResolvedValue({ answered: true, reply: 'Pronto! Registrei a R-0042.', no_runs: true, truncated: true })
    renderChat()
    fireEvent.change(screen.getByPlaceholderText(/descreva/i), { target: { value: 'Registra' } })
    fireEvent.click(screen.getByRole('button', { name: /enviar/i }))

    expect(await screen.findByText(/nenhum comando foi executado/i)).toBeInTheDocument()
    expect(screen.getByText(/truncated/)).toBeInTheDocument()
  })

  // F-0323 C6: o que rodou antes de uma falha aparece com o erro.
  it('erro com execuções mostra o painel de execuções e o erro', async () => {
    streamChat.mockRejectedValue(
      new ChatRunsError('tool-use loop exceeded its round limit', [
        { tool: 'cerne', verb: 'reference new', args: ['T'], result: { ok: true, id: 'R-0004', output: '' } },
      ]),
    )
    renderChat()
    fireEvent.change(screen.getByPlaceholderText(/descreva/i), { target: { value: 'Registra' } })
    fireEvent.click(screen.getByRole('button', { name: /enviar/i }))

    expect(await screen.findByText(/R-0004/)).toBeInTheDocument()
    expect(screen.getByText(/round limit/)).toBeInTheDocument()
  })

  it('resposta sem bloco de comando não renderiza card', async () => {
    await sendAndGetReply('Me conta mais sobre o formato.')
    await waitFor(() => expect(screen.getByText('Me conta mais sobre o formato.')).toBeInTheDocument())
    expect(screen.queryByRole('button', { name: /executar/i })).not.toBeInTheDocument()
  })
})

describe('TurnContextDisclosure (F-0436)', () => {
  const sources = [
    { id: 'D-0001', title: 'Backend git é a fonte da verdade', kind: 'decision' as const, state: 'active' },
    { id: 'F-0001', title: 'Conciliação', kind: 'retrieved' as const, state: 'implemented' },
  ]

  it('critério 2: <details> recolhido com a contagem e um link por fonte', () => {
    cleanup()
    const { container } = render(<TurnContextDisclosure sources={sources} />)
    const details = container.querySelector('details')
    expect(details).not.toHaveAttribute('open')
    expect(screen.getByText('Contexto deste turno (2)')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'D-0001' })).toHaveAttribute('href', '#/d/D-0001')
    expect(screen.getByRole('link', { name: 'F-0001' })).toHaveAttribute('href', '#/f/F-0001')
    expect(container.textContent).toContain('· Pronta')
    expect(container.textContent).not.toContain('Ativa')
  })

  it('critério 2: grupos por tipo na ordem recuperadas, decisões, convenções, com contagem e legenda', () => {
    cleanup()
    const { container } = render(
      <TurnContextDisclosure
        sources={[
          { id: 'D-0001', title: 'D', kind: 'decision' as const, state: 'active' },
          { id: 'N-0001', title: 'N', kind: 'convention' as const, state: 'active' },
          { id: 'F-0001', title: 'A', kind: 'retrieved' as const, state: 'implemented' },
          { id: 'F-0002', title: 'B', kind: 'retrieved' as const, state: 'implemented' },
        ]}
      />,
    )
    expect(screen.getByText('Injetado pelo cerne neste turno')).toBeInTheDocument()
    const labels = [...container.querySelectorAll('.turn-context-group, .turn-context-group-details > summary')].map((e) => e.textContent)
    expect(labels).toEqual(['Recuperado para esta mensagem (2)', 'Decisões ativas (1)', 'Convenções (1)'])
    const ids = [...container.querySelectorAll('.turn-context a')].map((e) => e.textContent)
    expect(ids).toEqual(['F-0001', 'F-0002', 'D-0001', 'N-0001'])
    expect(container.querySelectorAll('.turn-context-group-details[open]')).toHaveLength(0)
  })

  it('critério 2: cada prefixo vai à rota do roteador; tipo desconhecido vira texto', () => {
    cleanup()
    const mixed = [
      { id: 'N-0007', kind: 'convention' as const },
      { id: 'R-0003', kind: 'retrieved' as const },
      { id: 'wiki:guia/um.md', kind: 'retrieved' as const },
      { id: 'X-0001', kind: 'retrieved' as const },
    ]
    render(<TurnContextDisclosure sources={mixed} />)
    expect(screen.getByRole('link', { name: 'N-0007' })).toHaveAttribute('href', '#/n/N-0007')
    expect(screen.getByRole('link', { name: 'R-0003' })).toHaveAttribute('href', '#/r/R-0003')
    expect(screen.getByRole('link', { name: 'wiki:guia/um.md' })).toHaveAttribute('href', '#/wiki/guia%2Fum.md')
    expect(screen.getByText(/X-0001/)).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'X-0001' })).not.toBeInTheDocument()
  })

  it('critério 2: dois trechos da mesma entidade não duplicam chave do React', () => {
    cleanup()
    const err = vi.spyOn(console, 'error').mockImplementation(() => {})
    const twice = [
      { id: 'F-0001', title: 'A', kind: 'retrieved' as const, state: 'implemented' },
      { id: 'F-0001', title: 'A', kind: 'retrieved' as const, state: 'implemented' },
    ]
    render(<TurnContextDisclosure sources={twice} />)
    expect(err).not.toHaveBeenCalled()
    err.mockRestore()
  })

  it('critério 3: sem fontes não desenha nada', () => {
    cleanup()
    const { container } = render(<TurnContextDisclosure sources={[]} />)
    expect(container).toBeEmptyDOMElement()
  })

  it('critério 4: conversa gravada no formato antigo (sem context) abre sem o bloco', () => {
    cleanup()
    renderChat({ messages: [{ role: 'user', content: 'oi' }, { role: 'assistant', content: 'olá' }] })
    expect(screen.getByText('olá')).toBeInTheDocument()
    expect(document.querySelector('details.turn-context')).toBeNull()
  })
})

describe('BacklogChat proposal issues (F-0438)', () => {
  const issues: ProposalIssue[] = [
    { code: 'bar-missing', severity: 'error', detail: 'no "Barra:" line' },
    { code: 'open-clarification', severity: 'warning', detail: 'qual o limite?' },
  ]
  const send = async (text: string) => {
    fireEvent.change(screen.getByPlaceholderText(/descreva/i), { target: { value: text } })
    fireEvent.click(screen.getByRole('button', { name: /enviar/i }))
  }

  it('persists the issues on the message, and Pedir correção sends them as a user turn', async () => {
    streamChat.mockResolvedValueOnce({
      answered: true,
      reply: '````cerne-proposta\n# Exportar CSV\n\n## Problema\nFalta.\n````\n',
      proposal_issues: issues,
    }).mockResolvedValueOnce({ answered: true, reply: 'Corrigido.' })
    const { onUpdate } = renderChat()
    await send('Oi')
    fireEvent.click(await screen.findByRole('button', { name: /pedir correção/i }))
    await waitFor(() => expect(streamChat).toHaveBeenCalledTimes(2))
    const sent = streamChat.mock.calls[1][0] as { role: string; content: string }[]
    expect(sent[sent.length - 1].role).toBe('user')
    expect(sent[sent.length - 1].content).toContain('no "Barra:" line')
    // B-0035: the fix turn gets its own reply; without one the mock returned undefined
    expect(await screen.findByText('Corrigido.')).toBeInTheDocument()
    // the stored message carries them (a reload shows the card with its issues)
    const stored = JSON.stringify(window.localStorage)
    expect(stored).toContain('proposalIssues')
    expect(onUpdate).toHaveBeenCalled()
  })

  it('a later reply without a proposal clears the stale issues', async () => {
    streamChat.mockResolvedValueOnce({
      answered: true,
      reply: '````cerne-proposta\n# Exportar CSV\n\n## Problema\nFalta.\n````\n',
      proposal_issues: issues,
    })
    renderChat()
    await send('Oi')
    await screen.findByRole('button', { name: /pedir correção/i })
    streamChat.mockResolvedValueOnce({ answered: true, reply: 'Qual formato?' })
    await send('e agora?')
    await screen.findByText('Qual formato?')
    expect(screen.queryByRole('button', { name: /pedir correção/i })).not.toBeInTheDocument()
  })
})

describe('quem respondeu (F-0440)', () => {
  it('providerName usa o nome de marca, com fallback capitalizado', () => {
    expect(['anthropic', 'openai', 'gemini', 'cursor', 'copilot', 'vendor'].map(providerName)).toEqual(['Anthropic', 'OpenAI', 'Gemini', 'Cursor', 'GitHub Copilot', 'Vendor'])
  })

  it('AnsweredByLine mostra provedor, modelo (rótulo do catálogo) e esforço; sem dado, nada', () => {
    const entry: ModelEntry = { id: 'gpt-x', label: 'GPT X', provider: 'openai', visibility: 'current' }
    const { container, rerender } = render(<AnsweredByLine by={{ provider: 'openai', model: 'gpt-x', effort: 'medium' }} catalog={[entry]} />)
    expect(container.textContent).toBe('OpenAI · GPT X · medium')
    rerender(<AnsweredByLine by={{ provider: 'anthropic' }} catalog={[entry]} />)
    expect(container.textContent).toBe('Anthropic')
    rerender(<AnsweredByLine by={undefined} catalog={[entry]} />)
    expect(container.textContent).toBe('')
  })

  it('a mensagem gravada mostra a linha ao reabrir; a antiga sem o dado não mostra', async () => {
    const entry: ModelEntry = { id: 'gpt-x', label: 'GPT X', provider: 'openai', efforts: ['low'], default_effort: 'low', visibility: 'current' }
    fetchLLMOptions.mockResolvedValue(opts({ providers: ['anthropic', 'openai'], models: [...CATALOG, entry] }))
    renderChat({ messages: [
      { role: 'user', content: 'a' },
      { role: 'assistant', content: 'nova', answeredBy: { provider: 'openai', model: 'gpt-x', effort: 'low' } },
      { role: 'user', content: 'b' },
      { role: 'assistant', content: 'antiga' },
    ] } as never)
    expect(await screen.findByText('OpenAI · GPT X · low')).toBeInTheDocument()
    expect(document.querySelectorAll('.answered-by')).toHaveLength(1)
  })
})

describe('cartão de pergunta (F-0437)', () => {
  const reply =
    'Antes de seguir:\n\n````cerne-pergunta\n# Qual canal?\n- Email — chega a todos\n- Push — só quem instalou (recomendada)\n````\n'
  const send = (text: string) => {
    fireEvent.change(screen.getByPlaceholderText(/descreva/i), { target: { value: text } })
    fireEvent.click(screen.getByRole('button', { name: /^enviar$/i }))
  }
  const ask = async () => {
    streamChat.mockResolvedValueOnce({ answered: true, reply })
    renderChat()
    send('Notificações')
    await screen.findByRole('group', { name: 'Qual canal?' })
  }
  const lastTurn = () => {
    const sent = streamChat.mock.calls[streamChat.mock.calls.length - 1][0] as { role: string; content: string }[]
    return sent[sent.length - 1].content
  }

  it('draws one button per option, the recommended marked, and hides the raw block', async () => {
    await ask()
    expect(screen.getByRole('button', { name: /Email/ })).toBeInTheDocument()
    const rec = screen.getByRole('button', { name: /Push/ })
    expect(rec).toHaveClass('question-card-option-recommended')
    expect(screen.getByText('só quem instalou')).toBeInTheDocument()
    expect(screen.queryByText(/cerne-pergunta/)).not.toBeInTheDocument()
  })

  it('clicking the recommended sends the turn with "recomendação aceita" and disables the card', async () => {
    await ask()
    streamChat.mockResolvedValueOnce({ answered: true, reply: 'Anotado.' })
    fireEvent.click(screen.getByRole('button', { name: /Push/ }))
    await waitFor(() => expect(streamChat).toHaveBeenCalledTimes(2))
    expect(lastTurn()).toBe('**Qual canal?** → Push (recomendação aceita)')
    await screen.findByText('Anotado.')
    expect(screen.getByRole('button', { name: /Email/ })).toBeDisabled()
    expect(screen.getByRole('button', { name: /Push/ })).toBeDisabled()
  })

  it('another option is flagged "outra opção"', async () => {
    await ask()
    streamChat.mockResolvedValueOnce({ answered: true, reply: 'Ok.' })
    fireEvent.click(screen.getByRole('button', { name: /Email/ }))
    await waitFor(() => expect(streamChat).toHaveBeenCalledTimes(2))
    expect(lastTurn()).toBe('**Qual canal?** → Email (outra opção)')
  })

  it('"Outra resposta" sends the free text as "resposta livre"', async () => {
    await ask()
    streamChat.mockResolvedValueOnce({ answered: true, reply: 'Ok.' })
    fireEvent.change(screen.getByLabelText('Outra resposta'), { target: { value: 'SMS para quem não tem app' } })
    fireEvent.click(screen.getByRole('button', { name: 'Responder' }))
    await waitFor(() => expect(streamChat).toHaveBeenCalledTimes(2))
    expect(lastTurn()).toBe('**Qual canal?** → SMS para quem não tem app (resposta livre)')
  })

  it('QuestionCard stores nothing new: same storage keys, no request besides the turn', async () => {
    await ask()
    const keysBefore = Object.keys(localStorage).sort()
    streamChat.mockResolvedValueOnce({ answered: true, reply: 'Ok.' })
    fireEvent.click(screen.getByRole('button', { name: /Push/ }))
    await screen.findByText('Ok.')
    expect(Object.keys(localStorage).sort()).toEqual(keysBefore)
    expect(streamChat).toHaveBeenCalledTimes(2)
    expect(registerChatCandidate).not.toHaveBeenCalled()
    expect(registerChatNote).not.toHaveBeenCalled()
  })

  const two = reply + '\n````cerne-pergunta\n# Quando?\n- Hoje — já\n- Amanhã — depois (recomendada)\n````\n'
  const askTwo = async () => {
    streamChat.mockResolvedValueOnce({ answered: true, reply: two })
    renderChat()
    send('Oi')
    await screen.findByRole('group', { name: 'Qual canal?' })
  }

  // B-0047: one question at a time. A turn that still carries several cards
  // shows only the first open one; answering it sends at once, and the rest of
  // that turn never shows — the analyst asks again knowing the answer.
  it('a turn with several questions shows only the first one, with no round button', async () => {
    await askTwo()
    expect(screen.getByText('Pergunta 1 de 1')).toBeInTheDocument()
    expect(screen.queryByRole('group', { name: 'Quando?' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /Enviar respostas/ })).not.toBeInTheDocument()
  })

  it('answering the first question sends it at once and the rest of that turn stays hidden', async () => {
    await askTwo()
    streamChat.mockResolvedValueOnce({ answered: true, reply: 'Anotado.' })
    fireEvent.click(screen.getByRole('button', { name: /Push/ }))
    await waitFor(() => expect(streamChat).toHaveBeenCalledTimes(2))
    expect(lastTurn()).toBe('**Qual canal?** → Push (recomendação aceita)')
    await screen.findByText('Anotado.')
    expect(screen.getByText(/Pergunta 1 de 1 · Respondida/)).toBeInTheDocument()
    expect(screen.queryByRole('group', { name: 'Quando?' })).not.toBeInTheDocument()
  })

  it('a combined answer turn spends every card of the round (also after a reload) and renders one line per question', () => {
    renderChat({
      messages: [
        { role: 'user', content: 'x' },
        { role: 'assistant', content: two },
        { role: 'user', content: '**Qual canal?** → Email (outra opção)\n**Quando?** → Amanhã (recomendação aceita)' },
      ],
    })
    expect(screen.getByText(/Pergunta 1 de 2 · Respondida/)).toBeInTheDocument()
    expect(screen.getByText(/Pergunta 2 de 2 · Respondida/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Email/ })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByRole('button', { name: /Amanhã/ })).toHaveAttribute('aria-pressed', 'true')
    const strongs = document.querySelectorAll('.backlog-chat-msg-user strong')
    expect([...strongs].map((s) => s.textContent)).toEqual(['Qual canal?', 'Quando?'])
  })

  it('a spent card shows the chosen option pressed (also after a reload)', () => {
    renderChat({
      messages: [
        { role: 'user', content: 'Notificações' },
        { role: 'assistant', content: reply },
        { role: 'user', content: '**Qual canal?** → Email (outra opção)' },
      ],
    })
    expect(screen.getByRole('button', { name: /Email/ })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByRole('button', { name: /Email/ })).toHaveClass('question-card-option-chosen')
    expect(screen.getByRole('button', { name: /Push/ })).not.toHaveAttribute('aria-pressed', 'true')
    expect(screen.queryByLabelText('Outra resposta')).not.toBeInTheDocument()
  })

  it('a spent card with a free answer shows the text and no form', () => {
    renderChat({
      messages: [
        { role: 'user', content: 'x' },
        { role: 'assistant', content: reply },
        { role: 'user', content: '**Qual canal?** → SMS para todos (resposta livre)' },
      ],
    })
    expect(screen.getByText('Sua resposta')).toBeInTheDocument()
    expect(screen.getByText('SMS para todos')).toBeInTheDocument()
    expect(screen.queryByLabelText('Outra resposta')).not.toBeInTheDocument()
  })

  it('a duplicated question: an answer spends only the first unanswered card', () => {
    const dup = reply + '\n' + reply
    renderChat({
      messages: [
        { role: 'user', content: 'x' },
        { role: 'assistant', content: dup },
        { role: 'user', content: '**Qual canal?** → Push (recomendação aceita)' },
      ],
    })
    // B-0047: the spent card shows; the second copy is the rest of the turn,
    // hidden once one card of it was answered.
    expect(screen.getByText(/Pergunta 1 de 1 · Respondida/)).toBeInTheDocument()
    expect(screen.getAllByRole('group', { name: 'Qual canal?' })).toHaveLength(1)
  })

  it('the answer turn renders the question in <strong> with no asterisks; other user text stays literal', () => {
    renderChat({
      messages: [
        { role: 'user', content: 'preciso de **negrito** literal' },
        { role: 'assistant', content: reply },
        { role: 'user', content: '**Qual canal?** → Push (recomendação aceita)' },
      ],
    })
    const strong = screen.getAllByText('Qual canal?').find((e) => e.tagName === 'STRONG')
    expect(strong).toBeDefined()
    expect(strong!.closest('.backlog-chat-msg-user')!.textContent).toBe('Qual canal? → Push (recomendação aceita)')
    expect(screen.getByText('preciso de **negrito** literal')).toBeInTheDocument()
  })

  it('only the latest assistant message has live cards; an earlier unanswered one is locked', () => {
    renderChat({
      messages: [
        { role: 'user', content: 'x' },
        { role: 'assistant', content: reply },
        { role: 'user', content: 'e então?' },
        { role: 'assistant', content: reply },
      ],
    })
    const groups = screen.getAllByRole('group', { name: 'Qual canal?' })
    const [old, latest] = groups
    expect(within(old).getByText(/sem resposta/)).toBeInTheDocument()
    expect(within(old).getByRole('button', { name: /Push/ })).toBeDisabled()
    expect(within(old).queryByLabelText('Outra resposta')).not.toBeInTheDocument()
    expect(within(latest).getByRole('button', { name: /Push/ })).toBeEnabled()
    expect(within(latest).queryByText(/sem resposta/)).not.toBeInTheDocument()
  })

  it('an earlier answered card keeps its chosen state', () => {
    renderChat({
      messages: [
        { role: 'user', content: 'x' },
        { role: 'assistant', content: reply },
        { role: 'user', content: '**Qual canal?** → Email (outra opção)' },
        { role: 'assistant', content: 'Ok, mais algo?' },
      ],
    })
    expect(screen.getByRole('button', { name: /Email/ })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.queryByText(/sem resposta/)).not.toBeInTheDocument()
  })

  it('a malformed block draws no card and stays as text', async () => {
    streamChat.mockResolvedValueOnce({ answered: true, reply: '````cerne-pergunta\n# Sem opções\n````\n' })
    renderChat()
    send('Oi')
    await screen.findByText(/Sem opções/)
    expect(screen.queryByRole('group', { name: 'Sem opções' })).not.toBeInTheDocument()
  })
})

describe('BacklogChat: anexar imagens (F-0520)', () => {
  const png = (name = 'tela.png') => new File(['x'], name, { type: 'image/png' })
  const withImages = () => fetchLLMOptions.mockResolvedValue(opts({ imageProviders: ['anthropic'] }))
  const thumbs = () => screen.queryAllByAltText('Imagem anexada')
  const attachButton = () => screen.getByRole('button', { name: 'Anexar imagem' })

  beforeEach(() => {
    uploadChatAttachment.mockReset()
    let n = 0
    uploadChatAttachment.mockImplementation(async () => `${String(++n).padStart(16, '0')}.png`)
  })

  // F-0520 crit. 1
  it('colar uma imagem mostra a miniatura e não envia a mensagem', async () => {
    withImages()
    const { conversation } = renderChat()
    await waitFor(() => expect(attachButton()).toBeEnabled())
    fireEvent.paste(screen.getByRole('textbox'), { clipboardData: { files: [png()] } })
    await waitFor(() => expect(thumbs()).toHaveLength(1))
    expect(uploadChatAttachment).toHaveBeenCalledWith(conversation.id, expect.any(File))
    expect(thumbs()[0].getAttribute('src')).toContain(`/chat/attachments/${conversation.id}/0000000000000001.png`)
    expect(streamChat).not.toHaveBeenCalled()
  })

  // F-0520 crit. 2
  it('soltar um arquivo no painel e escolher pelo botão mostram a miniatura, que sai ao remover', async () => {
    withImages()
    const { container } = renderChat()
    await waitFor(() => expect(attachButton()).toBeEnabled())
    fireEvent.drop(container.querySelector('.backlog-chat')!, { dataTransfer: { files: [png('a.png')], types: ['Files'] } })
    await waitFor(() => expect(thumbs()).toHaveLength(1))
    fireEvent.change(container.querySelector('input[type="file"]')!, { target: { files: [png('b.png')] } })
    await waitFor(() => expect(thumbs()).toHaveLength(2))
    expect(streamChat).not.toHaveBeenCalled()

    fireEvent.click(screen.getAllByRole('button', { name: 'Remover imagem' })[0])
    expect(thumbs()).toHaveLength(1)
    expect(thumbs()[0].getAttribute('src')).toContain('0000000000000002.png')
  })

  // F-0520 crit. 5
  it('envia os ids das imagens na mensagem e limpa o compositor', async () => {
    withImages()
    streamChat.mockResolvedValue({ answered: true, reply: 'Vi a tela.' })
    renderChat()
    await waitFor(() => expect(attachButton()).toBeEnabled())
    fireEvent.paste(screen.getByRole('textbox'), { clipboardData: { files: [png()] } })
    await waitFor(() => expect(thumbs()).toHaveLength(1))
    fireEvent.change(screen.getByRole('textbox'), { target: { value: 'o que está errado aqui?' } })
    fireEvent.keyDown(screen.getByRole('textbox'), { key: 'Enter' })
    await waitFor(() => expect(streamChat).toHaveBeenCalled())
    const sent = streamChat.mock.calls[0][0] as { role: string; content: string; attachments?: string[] }[]
    expect(sent[sent.length - 1]).toMatchObject({ role: 'user', content: 'o que está errado aqui?', attachments: ['0000000000000001.png'] })
    // a miniatura agora está na bolha da mensagem, não mais no compositor
    expect(screen.queryByRole('button', { name: 'Remover imagem' })).toBeNull()
    expect(thumbs()).toHaveLength(1)
  })

  // F-0520 crit. 4
  it('recusa do servidor, tipo errado e sexto anexo mostram o motivo e mantêm o texto', async () => {
    withImages()
    renderChat()
    await waitFor(() => expect(attachButton()).toBeEnabled())
    fireEvent.change(screen.getByRole('textbox'), { target: { value: 'rascunho' } })

    uploadChatAttachment.mockRejectedValueOnce(new Error('image larger than 10 MB'))
    fireEvent.paste(screen.getByRole('textbox'), { clipboardData: { files: [png()] } })
    expect(await screen.findByRole('alert')).toHaveTextContent('image larger than 10 MB')
    expect(thumbs()).toHaveLength(0)

    fireEvent.paste(screen.getByRole('textbox'), { clipboardData: { files: [new File(['x'], 'a.pdf', { type: 'application/pdf' })] } })
    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('Só PNG, JPEG, WebP ou GIF'))

    fireEvent.paste(screen.getByRole('textbox'), { clipboardData: { files: [png(), png(), png(), png(), png(), png()] } })
    await waitFor(() => expect(thumbs()).toHaveLength(5))
    expect(screen.getByRole('alert')).toHaveTextContent('No máximo 5 imagens por mensagem')
    expect(screen.getByRole('textbox')).toHaveValue('rascunho')
  })

  // F-0520 crit. 8
  it('provedor que não recebe imagem deixa o anexo desabilitado, com o motivo', async () => {
    fetchLLMOptions.mockResolvedValue(opts({ imageProviders: [] }))
    renderChat()
    await waitFor(() => expect(fetchLLMOptions).toHaveBeenCalled())
    expect(attachButton()).toBeDisabled()
    expect(attachButton()).toHaveAttribute('title', 'Este provedor não recebe imagem no chat')
    fireEvent.paste(screen.getByRole('textbox'), { clipboardData: { files: [png()] } })
    await Promise.resolve()
    expect(uploadChatAttachment).not.toHaveBeenCalled()
  })

  // F-0520 crit. 9
  it('conversa recarregada mostra a miniatura da mensagem antiga, ou o marcador se ela expirou', async () => {
    withImages()
    const { conversation } = renderChat({
      messages: [{ role: 'user', content: 'veja', attachments: ['00000000000000aa.png'] }],
    })
    const img = await screen.findByAltText('Imagem anexada')
    expect(img.getAttribute('src')).toContain(`/chat/attachments/${conversation.id}/00000000000000aa.png`)
    fireEvent.error(img)
    expect(await screen.findByText('Imagem expirada')).toBeInTheDocument()
    expect(thumbs()).toHaveLength(0)
  })
})
