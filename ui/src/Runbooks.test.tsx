import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import Runbooks, { RunbookDetail } from './Runbooks'
import type { Runbook, RunbookDetail as RunbookDetailData, StateResponse } from './api'
import { fetchRunbook, fetchState } from './api'

const runbook: Runbook = {
  id: 'RB-0001',
  title: 'Runbook vivo',
  state: 'active',
  created_at: '2026-07-25T00:00:00Z',
  updated_at: '2026-07-25T00:00:00Z',
}

const state: StateResponse = {
  project: { name: 'Projeto Demo', modules: [] },
  features: [],
  decisions: [],
  references: [],
  runbooks: [runbook],
  notes: [],
}

const detail: RunbookDetailData = { runbook, doc: 'Corpo do runbook.', errata: '' }

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  fetchState: vi.fn(async () => state),
  fetchRunbook: vi.fn(async () => detail),
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
describe('Runbooks — falha de carga (F-0300)', () => {
  it('mantém a lista e mostra o aviso quando a re-busca ao vivo falha', async () => {
    const { rerender } = render(<Runbooks />)
    expect(await screen.findByText('Runbook vivo')).toBeInTheDocument()

    vi.mocked(fetchState).mockRejectedValueOnce(new Error('HTTP 502'))
    live.version = 1
    rerender(<Runbooks />)
    const notice = await screen.findByRole('status')
    expect(notice).toHaveTextContent('Não foi possível atualizar: HTTP 502')
    expect(screen.getByText('Runbook vivo')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Tentar de novo' }))
    await waitFor(() => expect(screen.queryByRole('status')).toBeNull())
    expect(screen.getByText('Runbook vivo')).toBeInTheDocument()
  })

  it('oferece "Tentar de novo" na página de erro da primeira carga da lista', async () => {
    vi.mocked(fetchState).mockRejectedValueOnce(new Error('offline'))
    render(<Runbooks />)
    expect(await screen.findByText('Erro ao carregar runbooks: offline')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Tentar de novo' }))
    expect(await screen.findByText('Runbook vivo')).toBeInTheDocument()
  })
})

describe('RunbookDetail — falha de carga (F-0300)', () => {
  it('mantém o runbook e mostra o aviso quando a re-busca ao vivo falha', async () => {
    const { rerender } = render(<RunbookDetail id="RB-0001" />)
    expect(await screen.findByText('Corpo do runbook.')).toBeInTheDocument()

    vi.mocked(fetchRunbook).mockRejectedValueOnce(new Error('HTTP 502'))
    live.version = 1
    rerender(<RunbookDetail id="RB-0001" />)
    const notice = await screen.findByRole('status')
    expect(notice).toHaveTextContent('Não foi possível atualizar: HTTP 502')
    expect(screen.getByText('Corpo do runbook.')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Tentar de novo' }))
    await waitFor(() => expect(screen.queryByRole('status')).toBeNull())
    expect(screen.getByText('Corpo do runbook.')).toBeInTheDocument()
  })

  it('oferece "Tentar de novo" na página de erro da primeira carga do runbook', async () => {
    vi.mocked(fetchRunbook).mockRejectedValueOnce(new Error('offline'))
    render(<RunbookDetail id="RB-0001" />)
    expect(await screen.findByText('Erro ao carregar o runbook: offline')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Tentar de novo' }))
    expect(await screen.findByText('Corpo do runbook.')).toBeInTheDocument()
  })
})
