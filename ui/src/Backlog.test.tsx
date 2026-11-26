import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import Backlog from './Backlog'
import { readChats, writeChats, type Conversation } from './useBacklogChats'
import type { ChatAnswer, ChatOverride, ChatTitle, CreateOutcome, Feature, LLMOptions, StateResponse } from './api'

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
  streamChat.mockReset()
  registerChatCandidate.mockReset()
  fetchLLMOptions.mockReset()
  fetchLLMOptions.mockResolvedValue({ mode: 'byo-key', models: [], defaultModel: '', providers: [], preferredProvider: '', imageProviders: [] })
  summarizeChatTitle.mockReset()
  summarizeChatTitle.mockResolvedValue({ generated: false })
})

describe('Backlog (F-0202)', () => {
  it('não mostra ideias — a página de ideias é a superfície delas', async () => {
    payload = state([feature('F-0001'), feature('F-0002')])
    const { container } = render(<Backlog />)
    expect(await screen.findByText('2 candidata(s)')).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'Ideias' })).not.toBeInTheDocument()
    expect(screen.queryByRole('textbox', { name: 'Nova ideia' })).not.toBeInTheDocument()
    expect(container.textContent ?? '').not.toMatch(/\bI-\d+/)
    expect(container.querySelectorAll('.card-candidate')).toHaveLength(2)
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
    fireEvent.click(screen.getByRole('button', { name: /conversas \(/i }))

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

    fireEvent.click(screen.getByRole('button', { name: /conversas \(/i }))
    fireEvent.click(screen.getByRole('button', { name: /renomear "Preciso exportar relatórios em CSV"/i }))
    const input = screen.getByRole('textbox', { name: /novo nome/i })
    fireEvent.change(input, { target: { value: 'Meu nome' } })
    // C1/C17: como no navegador, um Enter não cancelado ativa o botão que
    // recebeu o foco no keydown.
    if (fireEvent.keyDown(input, { key: 'Enter' }) && document.activeElement instanceof HTMLButtonElement) {
      fireEvent.click(document.activeElement)
    }
    expect(screen.queryByRole('textbox', { name: /novo nome/i })).not.toBeInTheDocument()

    fireEvent.click(registerButton)
    // O vínculo F-0256 chegou (appendRegisteredId rodou), e o título ficou.
    await waitFor(() => expect(readChats().chats.find((c) => c.featureId === 'F-0221')).toBeDefined())
    expect(readChats().chats.find((c) => c.featureId === 'F-0221')).toMatchObject({ title: 'Meu nome', titleSource: 'human' })
    // clicar no compositor (registrar) fechou o menu; reabre para conferir a linha
    fireEvent.click(screen.getByRole('button', { name: /conversas \(/i }))
    expect(screen.getByRole('button', { name: /renomear "Meu nome"/i })).toBeInTheDocument()
    expect(screen.queryByText(/→ F-0221/)).not.toBeInTheDocument()
  })
})

// F-0310: o título da conversa nasce truncado com a primeira mensagem e é
// trocado pelo resumo quando ele chega, sem mexer no id da conversa (F-0323).
describe('Backlog — título da conversa (F-0310)', () => {
  // Montar o Backlog não é atividade: a conversa ativa mantém o carimbo da
  // última mensagem real (crit. 1-7 — sem "agora" falso a cada visita).
  it('montar o Backlog não recarimba a conversa ativa', async () => {
    const seeded = Date.now() - 20_000
    writeChats(
      [{ id: 'a1', title: 'Ativa', updatedAt: seeded, messages: [{ role: 'user', content: 'Oi' }, { role: 'assistant', content: 'Olá' }], model: '', effort: '' }],
      'a1',
    )
    render(<Backlog />)
    await screen.findByPlaceholderText(/descreva/i)
    await waitFor(() => expect(fetchLLMOptions).toHaveBeenCalled())
    await new Promise((r) => setTimeout(r, 0))
    expect(readChats().chats.find((c) => c.id === 'a1')!.updatedAt).toBe(seeded)
  })

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

  // F-0431: o histórico vive num menu da barra do chat; abre antes de ler.
  const list = () => {
    if (!document.querySelector('.conversation-list')) fireEvent.click(screen.getByRole('button', { name: /conversas \(/i }))
    return document.querySelector('.conversation-list') as HTMLElement
  }

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

    // F-0431 critério 7: a marca fica na barra do chat (a lista de conversas é
    // um menu fechado).
    await waitFor(() =>
      expect(document.querySelector('.backlog-chat-refining')).toHaveTextContent('Refinando F-0001'),
    )
    expect(screen.getAllByText(/refinando F-0001/i)).toHaveLength(1)
  })
})

// F-0431: nova conversa e histórico vivem na barra do chat, não na coluna.
const mockWidth = (wide: boolean) =>
  vi.stubGlobal('matchMedia', (q: string) => ({
    matches: wide,
    media: q,
    addEventListener: () => {},
    removeEventListener: () => {},
  }))

describe('Backlog — barra do chat com nova conversa e histórico (F-0431)', () => {
  beforeEach(() => mockWidth(false))
  const toolbar = () => document.querySelector('.backlog-chat-toolbar') as HTMLElement
  const trigger = () => within(toolbar()).getByRole('button', { name: /conversas \(/i })

  it('critérios 1 e 5: com 50 candidatas, os controles ficam na barra e a coluna só tem cabeçalho e candidatas', async () => {
    payload = state(Array.from({ length: 50 }, (_, i) => feature(`F-${String(i + 1).padStart(4, '0')}`)))
    render(<Backlog />)
    await screen.findByText('50 candidata(s)')
    expect(within(toolbar()).getByRole('button', { name: /nova conversa/i })).toBeInTheDocument()
    expect(trigger()).toBeInTheDocument()
    const col = document.querySelector('.backlog-list-col') as HTMLElement
    expect(col.querySelector('.conversation-list')).toBeNull()
    expect(within(col).queryByRole('button', { name: /nova conversa/i })).toBeNull()
    expect(within(col).queryByRole('button', { name: /conversas \(/i })).toBeNull()
  })

  it('critérios 2 e 3: o gatilho abre a lista; escolher uma conversa troca a ativa e fecha o menu', async () => {
    writeChats(
      [
        { id: 'a', title: 'Conversa A', updatedAt: Date.now() - 1000, messages: [{ role: 'user', content: 'msg A' }], model: '', effort: '' },
        { id: 'b', title: 'Conversa B', updatedAt: Date.now(), messages: [{ role: 'user', content: 'msg B' }], model: '', effort: '' },
      ],
      'b',
    )
    render(<Backlog />)
    expect(await screen.findByText('msg B')).toBeInTheDocument()
    expect(trigger()).toHaveAttribute('aria-expanded', 'false')
    fireEvent.click(trigger())
    expect(trigger()).toHaveAttribute('aria-expanded', 'true')
    const list = document.querySelector('.conversation-list') as HTMLElement
    expect(within(list).getAllByRole('button', { name: /renomear/i })).toHaveLength(2)
    expect(within(list).getAllByRole('button', { name: /excluir/i })).toHaveLength(2)
    fireEvent.click(within(list).getByText('Conversa A'))
    expect(await screen.findByText('msg A')).toBeInTheDocument()
    expect(document.querySelector('.conversation-list')).toBeNull()
  })

  it('critério 3: "Nova conversa" troca para uma conversa vazia e fecha o menu', async () => {
    writeChats(
      [{ id: 'a', title: 'Conversa A', updatedAt: Date.now(), messages: [{ role: 'user', content: 'msg A' }], model: '', effort: '' }],
      'a',
    )
    render(<Backlog />)
    expect(await screen.findByText('msg A')).toBeInTheDocument()
    fireEvent.click(trigger())
    fireEvent.click(within(toolbar()).getByRole('button', { name: /nova conversa/i }))
    await waitFor(() => expect(screen.queryByText('msg A')).not.toBeInTheDocument())
    expect(document.querySelector('.conversation-list')).toBeNull()
    expect(readChats().chats).toHaveLength(2)
  })

  it('critério 4: Esc fecha o menu e devolve o foco ao gatilho; clique fora também fecha', async () => {
    render(<Backlog />)
    await screen.findByPlaceholderText(/descreva/i)
    fireEvent.click(trigger())
    expect(document.querySelector('.conversation-list')).not.toBeNull()
    trigger().focus()
    fireEvent.keyDown(trigger(), { key: 'Escape' })
    expect(document.querySelector('.conversation-list')).toBeNull()
    expect(trigger()).toHaveFocus()

    fireEvent.click(trigger())
    fireEvent.pointerDown(screen.getByPlaceholderText(/descreva/i))
    expect(document.querySelector('.conversation-list')).not.toBeNull() // só o click fecha (renomear salva antes)
    fireEvent.click(screen.getByPlaceholderText(/descreva/i))
    expect(document.querySelector('.conversation-list')).toBeNull()
  })

  it('critério 4: Esc dentro do editor de nome cancela só o nome, o menu segue aberto', async () => {
    writeChats([{ id: 'a', title: 'Conversa A', updatedAt: Date.now(), messages: [], model: '', effort: '' }], 'a')
    render(<Backlog />)
    await screen.findByPlaceholderText(/descreva/i)
    fireEvent.click(trigger())
    fireEvent.click(screen.getByRole('button', { name: /renomear/i }))
    fireEvent.keyDown(screen.getByRole('textbox', { name: /nome/i }), { key: 'Escape' })
    expect(document.querySelector('.conversation-list')).not.toBeNull()
  })

  it('Esc no compositor com o menu aberto não fecha o menu nem rouba o foco', async () => {
    render(<Backlog />)
    const composer = await screen.findByPlaceholderText(/descreva/i)
    fireEvent.click(trigger())
    composer.focus()
    fireEvent.keyDown(composer, { key: 'Escape' })
    expect(document.querySelector('.conversation-list')).not.toBeNull()
    expect(composer).toHaveFocus()
  })

  it('Esc num item do menu fecha e foca o gatilho', async () => {
    writeChats([{ id: 'a', title: 'Conversa A', updatedAt: Date.now(), messages: [], model: '', effort: '' }], 'a')
    render(<Backlog />)
    await screen.findByPlaceholderText(/descreva/i)
    fireEvent.click(trigger())
    const item = screen.getByText('Conversa A')
    item.focus()
    fireEvent.keyDown(item, { key: 'Escape' })
    expect(document.querySelector('.conversation-list')).toBeNull()
    expect(trigger()).toHaveFocus()
  })

  it('o foco saindo do menu para fora fecha o menu', async () => {
    render(<Backlog />)
    const composer = await screen.findByPlaceholderText(/descreva/i)
    fireEvent.click(trigger())
    trigger().focus()
    fireEvent.focusOut(trigger(), { relatedTarget: composer })
    expect(document.querySelector('.conversation-list')).toBeNull()
  })

  it('o gatilho aponta para o menu (aria-controls), sem aria-haspopup; o menu tem nome', async () => {
    render(<Backlog />)
    await screen.findByPlaceholderText(/descreva/i)
    fireEvent.click(trigger())
    expect(trigger()).not.toHaveAttribute('aria-haspopup')
    const id = trigger().getAttribute('aria-controls')
    expect(id).toBeTruthy()
    const menu = document.getElementById(id!)!
    expect(menu.querySelector('.conversation-list')).not.toBeNull()
    expect(menu).toHaveAccessibleName(/conversas/i)
  })

  it('a conversa ativa leva aria-current; selecionar foca o compositor da nova conversa', async () => {
    writeChats(
      [
        { id: 'a', title: 'Conversa A', updatedAt: Date.now() - 1000, messages: [], model: '', effort: '' },
        { id: 'b', title: 'Conversa B', updatedAt: Date.now(), messages: [], model: '', effort: '' },
      ],
      'b',
    )
    render(<Backlog />)
    await screen.findByPlaceholderText(/descreva/i)
    fireEvent.click(trigger())
    expect(screen.getByText('Conversa B').closest('button')).toHaveAttribute('aria-current', 'true')
    expect(screen.getByText('Conversa A').closest('button')).not.toHaveAttribute('aria-current')
    fireEvent.click(screen.getByText('Conversa A'))
    await waitFor(() => expect(screen.getByPlaceholderText(/descreva/i)).toHaveFocus())
  })

  it('a ordem do DOM põe o painel do chat antes das candidatas, com o título primeiro', async () => {
    payload = state([feature('F-0001')])
    render(<Backlog />)
    await screen.findByText('F-0001')
    const nodes = ['.backlog-head', '.backlog-chat-shell', '.backlog-list-col'].map((q) => document.querySelector(q)!)
    expect(nodes[0].compareDocumentPosition(nodes[1]) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(nodes[1].compareDocumentPosition(nodes[2]) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
  })
})

describe('Backlog — menu estreito fora do key do chat (F-0431)', () => {
  beforeEach(() => mockWidth(false))
  const trigger = () => screen.getByRole('button', { name: /conversas \(/i })
  const three = () =>
    writeChats(
      [
        { id: 'a', title: 'Conversa A', updatedAt: Date.now() - 2000, messages: [], model: '', effort: '' },
        { id: 'b', title: 'Conversa B', updatedAt: Date.now() - 1000, messages: [], model: '', effort: '' },
        { id: 'c', title: 'Conversa C', updatedAt: Date.now(), messages: [], model: '', effort: '' },
      ],
      'c',
    )

  it('item 2: excluir a conversa ativa mantém o menu aberto e foca a linha seguinte', async () => {
    three()
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    render(<Backlog />)
    await screen.findByPlaceholderText(/descreva/i)
    fireEvent.click(trigger())
    const rows = screen.getAllByRole('listitem') // C, B, A
    fireEvent.click(within(rows[0]).getByRole('button', { name: /excluir/i }))
    await waitFor(() => expect(screen.queryByText('Conversa C')).not.toBeInTheDocument())
    expect(document.querySelector('.conversation-list')).not.toBeNull()
    expect(screen.getByText('Conversa B').closest('button')).toHaveFocus()
    expect(document.body).not.toHaveFocus()
  })

  it('item 1: excluir uma linha (alvo removido durante o clique) não conta como clique fora', async () => {
    three()
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    render(<Backlog />)
    await screen.findByPlaceholderText(/descreva/i)
    fireEvent.click(trigger())
    const rows = screen.getAllByRole('listitem')
    fireEvent.click(within(rows[2]).getByRole('button', { name: /excluir/i })) // A, inativa
    await waitFor(() => expect(screen.queryByText('Conversa A')).not.toBeInTheDocument())
    expect(document.querySelector('.conversation-list')).not.toBeNull()
  })
})

describe('Backlog — largura reativa e foco (F-0431)', () => {
  afterEach(() => vi.unstubAllGlobals())
  it('item 3: o hook reage ao change do matchMedia e solta o listener ao desmontar', async () => {
    let handler: (() => void) | undefined
    const mq = {
      matches: false,
      media: '',
      addEventListener: (_: string, h: () => void) => (handler = h),
      removeEventListener: vi.fn(),
    }
    vi.stubGlobal('matchMedia', () => mq)
    const { unmount } = render(<Backlog />)
    await screen.findByPlaceholderText(/descreva/i)
    expect(document.querySelector('.backlog-chat-rail')).toBeNull()
    expect(screen.getByRole('button', { name: /conversas \(/i })).toBeInTheDocument()
    mq.matches = true
    act(() => handler!())
    expect(document.querySelector('.backlog-chat-rail')).not.toBeNull()
    expect(screen.queryByRole('button', { name: /conversas \(/i })).toBeNull()
    unmount()
    expect(mq.removeEventListener).toHaveBeenCalled()
  })

  it('item 4: no largo, selecionar e criar focam o compositor; a primeira carga não move o foco', async () => {
    mockWidth(true)
    writeChats(
      [
        { id: 'a', title: 'Conversa A', updatedAt: Date.now() - 1000, messages: [], model: '', effort: '' },
        { id: 'b', title: 'Conversa B', updatedAt: Date.now(), messages: [], model: '', effort: '' },
      ],
      'b',
    )
    render(<Backlog />)
    const composer = await screen.findByPlaceholderText(/descreva/i)
    expect(composer).not.toHaveFocus()
    const rail = document.querySelector('.backlog-chat-rail') as HTMLElement
    fireEvent.click(within(rail).getByText('Conversa A'))
    await waitFor(() => expect(screen.getByPlaceholderText(/descreva/i)).toHaveFocus())
    ;(screen.getByRole('button', { name: /nova conversa/i }) as HTMLElement).focus()
    fireEvent.click(screen.getByRole('button', { name: /nova conversa/i }))
    await waitFor(() => expect(screen.getByPlaceholderText(/descreva/i)).toHaveFocus())
  })

  it('item 5: a coluna tem nome acessível', async () => {
    mockWidth(true)
    render(<Backlog />)
    await screen.findByPlaceholderText(/descreva/i)
    expect(screen.getByRole('region', { name: 'Conversas' })).toBe(document.querySelector('.backlog-chat-rail'))
  })
})

// F-0431 rodada 2: a partir de 48rem o histórico é uma coluna sempre à vista.
describe('Backlog — coluna de histórico no painel do chat (F-0431)', () => {
  beforeEach(() => mockWidth(true))
  const rail = () => document.querySelector('.backlog-chat-rail') as HTMLElement

  it('critérios 1 e 5: largo, com 50 candidatas, a coluna fica no painel do chat e só lá', async () => {
    payload = state(Array.from({ length: 50 }, (_, i) => feature(`F-${String(i + 1).padStart(4, '0')}`)))
    render(<Backlog />)
    await screen.findByText('50 candidata(s)')
    expect(rail().closest('.backlog-chat-shell')).not.toBeNull()
    expect(rail().closest('.backlog-list-col')).toBeNull()
    expect(within(rail()).getByRole('button', { name: /nova conversa/i })).toBeInTheDocument()
    expect(rail().querySelector('.conversation-list')).not.toBeNull()
    const toolbar = document.querySelector('.backlog-chat-toolbar') as HTMLElement
    expect(within(toolbar).queryByRole('button', { name: /nova conversa/i })).toBeNull()
    expect(within(toolbar).queryByRole('button', { name: /conversas \(/i })).toBeNull()
    const col = document.querySelector('.backlog-list-col') as HTMLElement
    expect(col.querySelector('.conversation-list')).toBeNull()
    expect(document.querySelectorAll('.conversation-list')).toHaveLength(1)
  })

  it('critérios 1 a 3: um clique numa conversa da coluna troca a ativa; Nova conversa cria outra', async () => {
    writeChats(
      [
        { id: 'a', title: 'Conversa A', updatedAt: Date.now() - 1000, messages: [{ role: 'user', content: 'msg A' }], model: '', effort: '' },
        { id: 'b', title: 'Conversa B', updatedAt: Date.now(), messages: [{ role: 'user', content: 'msg B' }], model: '', effort: '' },
      ],
      'b',
    )
    render(<Backlog />)
    expect(await screen.findByText('msg B')).toBeInTheDocument()
    fireEvent.click(within(rail()).getByText('Conversa A'))
    expect(await screen.findByText('msg A')).toBeInTheDocument()
    expect(within(rail()).getAllByRole('button', { name: /excluir/i })).toHaveLength(2)
    fireEvent.click(within(rail()).getByRole('button', { name: /nova conversa/i }))
    await waitFor(() => expect(screen.queryByText('msg A')).not.toBeInTheDocument())
    expect(readChats().chats).toHaveLength(3)
  })

  it('critério 7: largo, "Refinar no chat" mostra o aviso na barra', async () => {
    payload = state([feature('F-0001')])
    render(<Backlog />)
    fireEvent.click(await screen.findByRole('button', { name: /refinar no chat/i }))
    await waitFor(() =>
      expect(document.querySelector('.backlog-chat-toolbar .backlog-chat-refining')).toHaveTextContent('Refinando F-0001'),
    )
  })

  it('a coluna fica fora do key: trocar de conversa não a remonta; excluir foca a linha seguinte', async () => {
    writeChats(
      [
        { id: 'a', title: 'Conversa A', updatedAt: Date.now() - 2000, messages: [], model: '', effort: '' },
        { id: 'b', title: 'Conversa B', updatedAt: Date.now() - 1000, messages: [], model: '', effort: '' },
        { id: 'c', title: 'Conversa C', updatedAt: Date.now(), messages: [], model: '', effort: '' },
      ],
      'c',
    )
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    render(<Backlog />)
    await screen.findByPlaceholderText(/descreva/i)
    const before = rail()
    expect(within(rail()).getByText('Conversa C').closest('button')).toHaveAttribute('aria-current', 'true')
    fireEvent.click(within(rail()).getByText('Conversa B'))
    await waitFor(() => expect(within(rail()).getByText('Conversa B').closest('button')).toHaveAttribute('aria-current', 'true'))
    expect(rail()).toBe(before)
    // lista: C, B, A — excluir C (a primeira, não ativa) foca B
    const rows = within(rail()).getAllByRole('listitem')
    fireEvent.click(within(rows[0]).getByRole('button', { name: /excluir/i }))
    await waitFor(() => expect(within(rail()).queryByText('Conversa C')).not.toBeInTheDocument())
    expect(within(rail()).getByText('Conversa B').closest('button')).toHaveFocus()
  })
})

// B-0048: the candidates column collapses into a narrow strip, like the
// kanban's Backlog column (F-0342), so the chat takes the width.
describe('Backlog — coluna de candidatas recolhível (B-0048)', () => {
  const toggle = () => screen.getByRole('button', { name: /^backlog$/i })

  it('recolhe e expande a coluna; recolhida, as candidatas saem do DOM e a página marca o layout', async () => {
    payload = state([feature('F-0001'), feature('F-0002')])
    const { container } = render(<Backlog />)
    await screen.findByText('F-0001')
    expect(toggle()).toHaveAttribute('aria-expanded', 'true')

    fireEvent.click(toggle())
    expect(toggle()).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByText('F-0001')).not.toBeInTheDocument()
    // eslint-disable-next-line testing-library/no-container, testing-library/no-node-access
    expect(container.querySelector('main')).toHaveClass('backlog-layout-collapsed')
    // recolhida, a contagem vira só o número
    expect(screen.getByText('2')).toBeInTheDocument()
    // o chat continua montado
    expect(screen.getByPlaceholderText(/descreva/i)).toBeInTheDocument()

    fireEvent.click(toggle())
    expect(toggle()).toHaveAttribute('aria-expanded', 'true')
    expect(screen.getByText('F-0001')).toBeInTheDocument()
  })

  it('a escolha fica lembrada por navegador', async () => {
    payload = state([feature('F-0001')])
    render(<Backlog />)
    await screen.findByText('F-0001')
    fireEvent.click(toggle())
    cleanup()

    render(<Backlog />)
    await screen.findByPlaceholderText(/descreva/i)
    expect(toggle()).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByText('F-0001')).not.toBeInTheDocument()
  })

  it('storage inacessível cai no padrão expandido sem quebrar a página', async () => {
    payload = state([feature('F-0001')])
    // Só a chave da coluna: os outros leitores do storage têm seus próprios testes.
    const KEY = 'cerne.backlog.listOpen'
    const realGet = Storage.prototype.getItem
    const realSet = Storage.prototype.setItem
    const get = vi.spyOn(Storage.prototype, 'getItem').mockImplementation(function (this: Storage, k: string) {
      if (k === KEY) throw new Error('blocked')
      return realGet.call(this, k)
    })
    const set = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(function (this: Storage, k: string, v: string) {
      if (k === KEY) throw new Error('blocked')
      realSet.call(this, k, v)
    })
    try {
      render(<Backlog />)
      await screen.findByText('F-0001')
      expect(toggle()).toHaveAttribute('aria-expanded', 'true')
      fireEvent.click(toggle())
      expect(toggle()).toHaveAttribute('aria-expanded', 'false')
    } finally {
      get.mockRestore()
      set.mockRestore()
    }
  })
})

// B-0048 (user follow-up): the conversations rail collapses too. Collapsed, the
// wide layout falls back to the toolbar menu the narrow layout already has, so
// "New conversation" and the history stay one click away.
describe('Backlog — coluna de conversas recolhível (B-0048)', () => {
  beforeEach(() => mockWidth(true))
  const rail = () => document.querySelector('.backlog-chat-rail')

  it('recolher a coluna de conversas leva o histórico para o menu da barra, e volta', async () => {
    render(<Backlog />)
    await screen.findByPlaceholderText(/descreva/i)
    expect(rail()).not.toBeNull()

    fireEvent.click(screen.getByRole('button', { name: /recolher conversas/i }))
    expect(rail()).toBeNull()
    const toolbar = document.querySelector('.backlog-chat-toolbar') as HTMLElement
    expect(within(toolbar).getByRole('button', { name: /nova conversa/i })).toBeInTheDocument()
    expect(within(toolbar).getByRole('button', { name: /conversas \(/i })).toBeInTheDocument()
    expect(localStorage.getItem('cerne.backlog.railOpen')).toBe('false')

    fireEvent.click(within(toolbar).getByRole('button', { name: /mostrar conversas/i }))
    expect(rail()).not.toBeNull()
    expect(localStorage.getItem('cerne.backlog.railOpen')).toBe('true')
  })

  it('a escolha fica lembrada por navegador', async () => {
    localStorage.setItem('cerne.backlog.railOpen', 'false')
    render(<Backlog />)
    await screen.findByPlaceholderText(/descreva/i)
    expect(rail()).toBeNull()
    expect(screen.getByRole('button', { name: /mostrar conversas/i })).toHaveAttribute('aria-expanded', 'false')
  })
})

describe('Backlog — card ligado a uma issue (B-0063)', () => {
  // B-0063
  it('a candidata com mirror mostra a etiqueta da issue, sem link dentro do card', async () => {
    payload = state([
      feature('F-0001', { mirror: { tracker: 'github', kind: 'github', id: '4', url: 'https://example.test/4' } }),
      feature('F-0002'),
    ])
    render(<Backlog />)
    const badge = await screen.findByText('issue do GitHub #4')
    expect(badge.closest('a')).toHaveAttribute('href', '#/f/F-0001')
    expect(badge).toHaveClass('badge-mirror')
    expect(screen.getByText('F-0002').closest('a')?.querySelector('.badge-mirror')).toBeNull()
  })
})
