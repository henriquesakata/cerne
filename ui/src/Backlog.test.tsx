import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import Backlog from './Backlog'
import type { ChatAnswer, ChatOverride, CreateOutcome, Feature, LLMOptions, StateResponse } from './api'

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
  (title: string, specBody: string, facets?: string[], components?: string[]) => Promise<CreateOutcome>
>()
const fetchLLMOptions = vi.fn<() => Promise<LLMOptions>>()

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  fetchState: vi.fn(async () => payload),
  streamChat: (messages: unknown, override: ChatOverride | undefined, onDelta: (d: string) => void) => streamChat(messages, override, onDelta),
  registerChatCandidate: (title: string, specBody: string, facets?: string[], components?: string[]) =>
    registerChatCandidate(title, specBody, facets, components),
  fetchLLMOptions: () => fetchLLMOptions(),
}))

beforeEach(() => {
  cleanup()
  localStorage.clear()
  payload = state([])
  streamChat.mockReset()
  registerChatCandidate.mockReset()
  fetchLLMOptions.mockReset()
  fetchLLMOptions.mockResolvedValue({ mode: 'byo-key', models: [], efforts: [] })
})

describe('Backlog (F-0202)', () => {
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
