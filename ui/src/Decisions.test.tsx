import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import Decisions, { DecisionDetail } from './Decisions'
import type { Decision, DecisionDetail as DecisionDetailData, StateResponse } from './api'
import { fetchDecision, fetchState } from './api'

const decision: Decision = {
  id: 'D-0001',
  title: 'Decisão viva',
  state: 'active',
  created_at: '2026-07-25T00:00:00Z',
  updated_at: '2026-07-25T00:00:00Z',
}

const state: StateResponse = {
  project: { name: 'Projeto Demo', modules: [] },
  features: [],
  decisions: [decision],
  references: [],
  runbooks: [],
  notes: [],
}

const detail: DecisionDetailData = {
  decision,
  doc: 'Corpo da decisão.',
  informed_by: [],
  errata: '',
}

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  fetchState: vi.fn(async () => state),
  fetchDecision: vi.fn(async () => detail),
  fetchGraph: vi.fn(async () => ({ nodes: [], edges: [] })),
}))

// Versão ao vivo controlável (F-0300): a tela re-busca quando ela muda.
const live = vi.hoisted(() => ({ version: 0 }))
vi.mock('./live', () => ({ useLiveVersion: () => live.version }))

beforeEach(() => {
  cleanup()
  live.version = 0
})

// F-0300: uma atualização que falha não apaga a tela — vira aviso com
// "Tentar de novo"; sem conteúdo ainda, a página de erro oferece o mesmo botão.
describe('Decisions — falha de carga (F-0300)', () => {
  it('mantém a lista e mostra o aviso quando a re-busca ao vivo falha', async () => {
    const { rerender } = render(<Decisions />)
    expect(await screen.findByText('Decisão viva')).toBeInTheDocument()

    vi.mocked(fetchState).mockRejectedValueOnce(new Error('HTTP 502'))
    live.version = 1
    rerender(<Decisions />)
    const notice = await screen.findByRole('status')
    expect(notice).toHaveTextContent('Não foi possível atualizar: HTTP 502')
    expect(screen.getByText('Decisão viva')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Tentar de novo' }))
    await waitFor(() => expect(screen.queryByRole('status')).toBeNull())
    expect(screen.getByText('Decisão viva')).toBeInTheDocument()
  })

  it('oferece "Tentar de novo" na página de erro da primeira carga da lista', async () => {
    vi.mocked(fetchState).mockRejectedValueOnce(new Error('offline'))
    render(<Decisions />)
    expect(await screen.findByText('Erro ao carregar decisões: offline')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Tentar de novo' }))
    expect(await screen.findByText('Decisão viva')).toBeInTheDocument()
  })
})

describe('DecisionDetail — falha de carga (F-0300)', () => {
  it('mantém a decisão e mostra o aviso quando a re-busca ao vivo falha', async () => {
    const { rerender } = render(<DecisionDetail id="D-0001" />)
    expect(await screen.findByText('Corpo da decisão.')).toBeInTheDocument()

    vi.mocked(fetchDecision).mockRejectedValueOnce(new Error('HTTP 502'))
    live.version = 1
    rerender(<DecisionDetail id="D-0001" />)
    const notice = await screen.findByRole('status')
    expect(notice).toHaveTextContent('Não foi possível atualizar: HTTP 502')
    expect(screen.getByText('Corpo da decisão.')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Tentar de novo' }))
    await waitFor(() => expect(screen.queryByRole('status')).toBeNull())
    expect(screen.getByText('Corpo da decisão.')).toBeInTheDocument()
  })

  it('oferece "Tentar de novo" na página de erro da primeira carga da decisão', async () => {
    vi.mocked(fetchDecision).mockRejectedValueOnce(new Error('offline'))
    render(<DecisionDetail id="D-0001" />)
    expect(await screen.findByText('Erro ao carregar a decisão: offline')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Tentar de novo' }))
    expect(await screen.findByText('Corpo da decisão.')).toBeInTheDocument()
  })
})

// F-0385: a decisão retirada aparece no seu próprio grupo, ao lado das
// substituídas; um estado que a tela não conhece também aparece, nunca some.
describe('Decisions — decisões retiradas (F-0385)', () => {
  it('lista a retirada no grupo Retiradas e não esconde estado desconhecido', async () => {
    vi.mocked(fetchState).mockResolvedValueOnce({
      ...state,
      decisions: [
        decision,
        { ...decision, id: 'D-0002', title: 'Decisão substituída', state: 'superseded', superseded_by: 'D-0001' },
        { ...decision, id: 'D-0003', title: 'Decisão retirada', state: 'retired', reason: 'deixou de valer' },
        { ...decision, id: 'D-0004', title: 'Decisão em estado novo', state: 'paused' },
      ],
    })
    render(<Decisions />)
    expect(await screen.findByText('Decisão retirada')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: /^Retiradas/ })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: /^Substituídas/ })).toBeInTheDocument()
    expect(screen.getByText('Decisão em estado novo')).toBeInTheDocument()
  })
})
