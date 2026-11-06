import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import Backlog from './Backlog'
import type { ChatAnswer, ChatOverride, CreateOutcome, Feature, Idea, LLMOptions, StateResponse } from './api'

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
