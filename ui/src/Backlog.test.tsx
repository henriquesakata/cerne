import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import Backlog from './Backlog'
import { readChats, writeChats, type Conversation } from './useBacklogChats'
import type { ChatAnswer, ChatOverride, ChatTitle, CreateOutcome, Feature, Idea, LLMOptions, StateResponse } from './api'

const feature = (id: string, extra: Partial<Feature> = {}): Feature => ({
  id,
  title: `Feature ${id}`,
  module: '',
  state: 'candidate',
  created_at: '2026-07-25T00:00:00Z',
  updated_at: '2026-07-25T00:00:00Z',
  ...extra,
})

const state = (features: Feature[]): StateResponse => ({
  project: { name: 'Projeto Demo', modules: [] },
  features,
  decisions: [],
  references: [],
  runbooks: [],
  notes: [],
})

let payload: StateResponse = state([])
let ideasPayload: Idea[] = []
const streamChat = vi.fn<(messages: unknown, override: ChatOverride | undefined, onDelta: (d: string) => void) => Promise<ChatAnswer>>()
const registerChatCandidate = vi.fn<
  (
    title: string,
    specBody: string,
    facets?: string[],
    components?: string[],
    capability?: string,
    parent?: string,
    featureId?: string,
  ) => Promise<CreateOutcome>
>()
const fetchLLMOptions = vi.fn<() => Promise<LLMOptions>>()
// F-0310: nenhum teste depende de fetch real para o resumo do título.
const summarizeChatTitle = vi.fn<() => Promise<ChatTitle>>()

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  fetchState: vi.fn(async () => payload),
  fetchIdeas: vi.fn(async () => ideasPayload),
  streamChat: (messages: unknown, override: ChatOverride | undefined, onDelta: (d: string) => void) => streamChat(messages, override, onDelta),
  registerChatCandidate: (
    title: string,
    specBody: string,
    facets?: string[],
    components?: string[],
    capability?: string,
    parent?: string,
    featureId?: string,
  ) => registerChatCandidate(title, specBody, facets, components, capability, parent, featureId),
  fetchLLMOptions: () => fetchLLMOptions(),
  summarizeChatTitle: () => summarizeChatTitle(),
}))

beforeEach(() => {
  cleanup()
  localStorage.clear()
  payload = state([])
  ideasPayload = []
  streamChat.mockReset()
  registerChatCandidate.mockReset()
  fetchLLMOptions.mockReset()
  fetchLLMOptions.mockResolvedValue({ mode: 'byo-key', models: [], defaultModel: '', providers: [], preferredProvider: '' })
  summarizeChatTitle.mockReset()
  summarizeChatTitle.mockResolvedValue({ generated: false })
})

describe('Backlog (F-0202)', () => {
  it('painel de ideias separado das candidatas (F-0312)', async () => {
    payload = state([feature('F-0001'), feature('F-0002')])
    const at = '2026-11-06T10:00:00Z'
    ideasPayload = [
      { id: 'I-0002', text: 'ideia dois', state: 'active', created_at: at, updated_at: at },
      { id: 'I-0001', text: 'ideia um', state: 'active', created_at: at, updated_at: at },
    ]
    const { container } = render(<Backlog />)
    expect(await screen.findByText('ideia um')).toBeInTheDocument()
    expect(screen.getByText('2 candidata(s)')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Ideias' })).toBeInTheDocument()
    const cards = container.querySelectorAll('.card-candidate')
    expect(cards).toHaveLength(2)
    cards.forEach((c) => expect(c.textContent).not.toMatch(/I-|ideia/))
    // C10: the capture is reachable without scrolling past the candidates.
    const capture = screen.getByRole('textbox', { name: 'Nova ideia' })
    expect(capture.compareDocumentPosition(cards[0]) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
  })

  it('mostra o badge de esclarecimentos pendentes na candidata (F-0195)', async () => {
    payload = { ...state([feature('F-0556'), feature('F-0557')]), open_clarifications: { 'F-0557': 2 } }
    render(<Backlog />)
    const badge = await screen.findByTitle('2 esclarecimento(s) aguardando sua resposta')
    expect(badge).toHaveTextContent('2')
    // Sem pendência não vira badge fantasma.
    expect(screen.queryByTitle(/1 esclarecimento/)).toBeNull()
  })

  it('renderiza os cards sem o campo open_clarifications', async () => {
    payload = state([feature('F-0001')])
    render(<Backlog />)
    expect(await screen.findByText('Feature F-0001')).toBeInTheDocument()
  })

  it('lista só features candidate, com link para a feature', async () => {
    payload = state([
      feature('F-0001'),
      feature('F-0002', { state: 'in_development' }),
    ])
    render(<Backlog />)

    await waitFor(() => expect(screen.getByText('F-0001')).toBeInTheDocument())
    expect(screen.queryByText('F-0002')).not.toBeInTheDocument()
    const link = screen.getByText('F-0001').closest('a')
    expect(link).toHaveAttribute('href', '#/f/F-0001')
  })

  it('mostra estado vazio orientando cerne feature new', async () => {
    render(<Backlog />)
    await waitFor(() =>
      expect(screen.getByText(/cerne feature new/)).toBeInTheDocument(),
    )
  })

  it('monta o chat com o agente ao lado da lista (F-0210)', async () => {
    render(<Backlog />)
    await waitFor(() =>
      expect(screen.getByText(/converse com o agente/i)).toBeInTheDocument(),
    )
  })

  // F-0204: o Backlog ordena por prioridade por default (p0 primeiro, sem
  // prioridade por último), e mostra chip de prioridade + handle do owner.
  it('ordena por prioridade e mostra chip/handle', async () => {
    payload = state([
      feature('F-0001', { priority: 'p2' }),
      feature('F-0002', { priority: 'p0', owner: 'ana' }),
      feature('F-0003'),
    ])
    render(<Backlog />)

    await waitFor(() => expect(screen.getByText('F-0002')).toBeInTheDocument())
    const ids = Array.from(document.querySelectorAll('.card-id')).map((el) => el.textContent)
    expect(ids).toEqual(['F-0002', 'F-0001', 'F-0003'])

    const card = screen.getByText('F-0002').closest('a')
    expect(card?.querySelector('.badge-priority')).toHaveTextContent('p0')
    expect(card).toHaveTextContent('@ana')
  })

  // F-0219 critério 1: a seção "Conversas" aparece na coluna esquerda.
  it('F-0219: mostra a seção de conversas abaixo da lista de candidatas', async () => {
    render(<Backlog />)
    await waitFor(() => expect(screen.getByText(/conversas/i)).toBeInTheDocument())
  })

  // F-0219 critério 3: registrar a candidata sufixa o id gerado no título da
  // conversa ativa — o card na lista e a conversa que o originou ficam ligados.
  it('F-0219: registrar candidata sufixa o id gerado no título da conversa ativa', async () => {
    const proposalReply =
      'Aqui está:\n\n```cerne-proposta\n# Exportar relatórios em CSV\n\n## Problema\nFalta exportação.\n```\n'
    streamChat.mockResolvedValue({ answered: true, reply: proposalReply })
    registerChatCandidate.mockResolvedValue({
      ok: true,
      feature: {
        id: 'F-0221',
        title: 'Exportar relatórios em CSV',
        module: '',
        state: 'candidate',
        created_at: '2026-08-07T00:00:00Z',
        updated_at: '2026-08-07T00:00:00Z',
      },
    })
    render(<Backlog />)

    fireEvent.change(await screen.findByPlaceholderText(/descreva/i), {
      target: { value: 'Preciso exportar relatórios em CSV' },
    })
    fireEvent.click(screen.getByRole('button', { name: /enviar/i }))

    const registerButton = await screen.findByRole('button', { name: /registrar candidata/i })
    fireEvent.click(registerButton)

    await waitFor(() =>
      expect(screen.getByText(/Preciso exportar relatórios em CSV.*→ F-0221/)).toBeInTheDocument(),
    )
  })

  // F-0310 critério 17: o nome dado pelo humano é terminal — registrar a
  // candidata vincula a conversa, mas não sufixa o título.
  it('F-0310 critério 17: renomeada pela lista, a conversa não ganha o sufixo ao registrar a candidata', async () => {
    const proposalReply =
      'Aqui está:\n\n```cerne-proposta\n# Exportar relatórios em CSV\n\n## Problema\nFalta exportação.\n```\n'
    streamChat.mockResolvedValue({ answered: true, reply: proposalReply })
    registerChatCandidate.mockResolvedValue({
      ok: true,
      feature: {
        id: 'F-0221',
        title: 'Exportar relatórios em CSV',
        module: '',
        state: 'candidate',
        created_at: '2026-08-07T00:00:00Z',
        updated_at: '2026-08-07T00:00:00Z',
      },
    })
    render(<Backlog />)

    fireEvent.change(await screen.findByPlaceholderText(/descreva/i), {
      target: { value: 'Preciso exportar relatórios em CSV' },
    })
    fireEvent.click(screen.getByRole('button', { name: /enviar/i }))
    const registerButton = await screen.findByRole('button', { name: /registrar candidata/i })

    fireEvent.click(screen.getByRole('button', { name: /renomear "Preciso exportar relatórios em CSV"/i }))
    const input = screen.getByRole('textbox', { name: /novo nome/i })
    fireEvent.change(input, { target: { value: 'Meu nome' } })
    fireEvent.keyDown(input, { key: 'Enter' })

    fireEvent.click(registerButton)
    // O vínculo F-0256 chegou (appendRegisteredId rodou), e o título ficou.
    await waitFor(() => expect(readChats().chats.find((c) => c.featureId === 'F-0221')).toBeDefined())
    expect(readChats().chats.find((c) => c.featureId === 'F-0221')).toMatchObject({ title: 'Meu nome', titleSource: 'human' })
    expect(screen.getByRole('button', { name: /renomear "Meu nome"/i })).toBeInTheDocument()
    expect(screen.queryByText(/→ F-0221/)).not.toBeInTheDocument()
  })
})

// F-0310: o título da conversa nasce truncado com a primeira mensagem e é
// trocado pelo resumo quando ele chega, sem mexer no id da conversa (F-0323).
describe('Backlog — título da conversa (F-0310)', () => {
  // C3: uma conversa gravada sem updatedAt (corrupção, edição à mão) não
  // derruba a tela: a linha aparece com a idade neutra. Ela fica inativa — a
  // ativa é recarimbada pelo updateActive da montagem.
  it('C3: conversa corrompida no storage não derruba o Backlog', async () => {
    writeChats(
      [
        { id: 'ok1', title: 'conversa boa', updatedAt: Date.now(), messages: [], model: '', effort: '' },
        { id: 'corrupt1', title: 'sem carimbo', messages: [], model: '', effort: '' } as unknown as Conversation,
      ],
      'ok1',
    )
    render(<Backlog />)
    await waitFor(() => expect(within(list()).getByText('sem carimbo')).toBeInTheDocument())
    expect(within(list()).getByText('—')).toBeInTheDocument()
  })

  const list = () => document.querySelector('.conversation-list') as HTMLElement

  async function send(text: string) {
    fireEvent.change(await screen.findByPlaceholderText(/descreva/i), { target: { value: text } })
    fireEvent.click(screen.getByRole('button', { name: /enviar/i }))
  }

  it('F-0310 critério 14: a lista mostra o título truncado durante o primeiro turno', async () => {
    streamChat.mockReturnValue(new Promise(() => {}))
    render(<Backlog />)
    await send('Preciso de X')
    await waitFor(() => expect(within(list()).getByText('Preciso de X')).toBeInTheDocument())
    expect(within(list()).queryByText('(sem título)')).not.toBeInTheDocument()
  })

  it('F-0310 critério 9: o resumo substitui o título truncado quando chega', async () => {
    streamChat.mockResolvedValue({ answered: true, reply: 'Em qual formato?' })
    summarizeChatTitle.mockResolvedValue({ generated: true, title: 'Exportar PDF' })
    render(<Backlog />)
    await send('Preciso exportar relatórios')
    await waitFor(() => expect(within(list()).getByText('Exportar PDF')).toBeInTheDocument())
  })

  it('F-0310 / F-0323: o resumo não muda o conversation_id do turno seguinte', async () => {
    streamChat.mockResolvedValue({ answered: true, reply: 'Em qual formato?' })
    summarizeChatTitle.mockResolvedValue({ generated: true, title: 'Exportar PDF' })
    render(<Backlog />)
    await send('Preciso exportar relatórios')
    await waitFor(() => expect(within(list()).getByText('Exportar PDF')).toBeInTheDocument())
    const firstId = (streamChat.mock.calls[0][1] as ChatOverride).conversationId
    expect(firstId).toBeTruthy()

    await send('Em PDF')
    await waitFor(() => expect(streamChat).toHaveBeenCalledTimes(2))
    expect(streamChat.mock.calls[1][1]).toEqual(expect.objectContaining({ conversationId: firstId }))
  })
})

// F-0256 critérios 1/5: o card de uma candidata abre uma conversa de refino;
// o refino não existe para features fora de candidate (o Backlog só lista
// candidatas, então o gate visual é a própria lista + o gate do servidor).
describe('Backlog — refinar candidata no chat (F-0256)', () => {
  it('o card oferece refinar no chat e a conversa nasce ligada ao id', async () => {
    payload = state([feature('F-0001')])
    render(<Backlog />)

    const refine = await screen.findByRole('button', { name: /refinar no chat/i })
    fireEvent.click(refine)

    // A marca aparece no chat (a conversa ativa refina F-0001) e na lista de
    // conversas — a mesma conversa, vista dos dois lados.
    await waitFor(() =>
      expect(document.querySelector('.backlog-chat-refining')).toHaveTextContent('Refinando F-0001'),
    )
    expect(screen.getAllByText(/refinando F-0001/i)).toHaveLength(2)
  })
})
